package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/diandian921/sofarpc-cli/internal/app"
	"github.com/diandian921/sofarpc-cli/internal/appconfig"
	"github.com/diandian921/sofarpc-cli/internal/mcp/proto"
	mcpserver "github.com/diandian921/sofarpc-cli/internal/mcp/server"
	"github.com/diandian921/sofarpc-cli/internal/mcp/tools"
	"github.com/diandian921/sofarpc-cli/internal/schema"
)

// Server is the stdio MCP server facade. It wires the proto session, the typed
// tool registry, and the app service together; its public surface (fields, Run,
// SelfTest) is unchanged so cli/mcp.go does not churn.
type Server struct {
	BuildVersion       string
	Stdin              io.Reader
	Stdout             io.Writer
	Stderr             io.Writer
	DisableConfigWrite bool
	App                *app.Service

	registry *mcpserver.Registry
}

func (s *Server) Run() int {
	_ = schema.CleanupUnused(7 * 24 * time.Hour)
	in := s.Stdin
	if in == nil {
		in = bytes.NewReader(nil)
	}
	out := s.Stdout
	if out == nil {
		out = io.Discard
	}
	stderr := s.Stderr
	if stderr == nil {
		stderr = io.Discard
	}
	return s.newSession(in, out, stderr).Run()
}

// newSession wires a proto.Session over the given streams with this server's
// identity, capabilities, and dispatcher. Shared by Run and SelfTest so both go
// through the real protocol engine.
func (s *Server) newSession(in io.Reader, out, stderr io.Writer) *proto.Session {
	return proto.NewSession(proto.Config{
		In:           in,
		Out:          out,
		Stderr:       stderr,
		Info:         s.serverInfo(),
		Capabilities: s.serverCapabilities(),
		Instructions: serverInstructions,
		Dispatcher:   &dispatcher{server: s, stderr: stderr},
	})
}

// SelfTest brings up the server machinery — config path, app service, tool
// registry (including its invariants) — then drives a real proto.Session through
// the full lifecycle (initialize → notifications/initialized → tools/list →
// tools/call) over in-memory streams, so a broken config or handshake fails here
// instead of at first agent use.
func (s *Server) SelfTest() error {
	if _, err := appconfig.DefaultPath(); err != nil {
		return fmt.Errorf("config path: %w", err)
	}
	if s.application() == nil {
		return errors.New("app service is nil")
	}
	if len(s.toolDefs()) == 0 {
		return errors.New("no tools registered")
	}
	if err := s.toolRegistry().Validate(); err != nil {
		return fmt.Errorf("tool registry invalid: %w", err)
	}
	frames := strings.Join([]string{
		`{"jsonrpc":"2.0","id":0,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"sofarpc_config_list","arguments":{}}}`,
		"",
	}, "\n")
	out := &bytes.Buffer{}
	if code := s.newSession(strings.NewReader(frames), out, io.Discard).Run(); code != 0 {
		return fmt.Errorf("self-test session exited with code %d", code)
	}
	return verifySelfTest(out.String())
}

// verifySelfTest checks that each self-test request got a healthy response.
func verifySelfTest(out string) error {
	byID := map[string]map[string]interface{}{}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var resp map[string]interface{}
		if err := json.Unmarshal([]byte(line), &resp); err != nil {
			continue
		}
		if resp["id"] != nil {
			byID[fmt.Sprint(resp["id"])] = resp
		}
	}
	initResp := byID["0"]
	if initResp == nil || initResp["error"] != nil {
		return fmt.Errorf("initialize failed: %s", out)
	}
	if r, _ := initResp["result"].(map[string]interface{}); r == nil || r["protocolVersion"] == nil {
		return errors.New("initialize response missing protocolVersion")
	}
	listResp := byID["1"]
	if listResp == nil || listResp["error"] != nil {
		return errors.New("tools/list failed")
	}
	if r, _ := listResp["result"].(map[string]interface{}); r == nil || r["tools"] == nil {
		return errors.New("tools/list missing tools")
	}
	callResp := byID["2"]
	if callResp == nil || callResp["error"] != nil {
		return fmt.Errorf("tools/call config_list failed: %v", callResp["error"])
	}
	r, _ := callResp["result"].(map[string]interface{})
	if r == nil {
		return errors.New("tools/call config_list missing result")
	}
	if isErr, _ := r["isError"].(bool); isErr {
		return errors.New("config_list returned an error result")
	}
	return nil
}

// handle answers tools/list and tools/call for the dispatcher. It is the
// dispatcher's entry only — never call it from tests or self-test, which must go
// through a real proto.Session so lifecycle / ping / cancel / transport behavior
// is exercised.
func (s *Server) handle(ctx context.Context, req proto.Request) (proto.Response, bool) {
	base := proto.Response{JSONRPC: "2.0", ID: req.ID}
	switch req.Method {
	case "tools/list":
		base.Result = map[string]interface{}{"tools": s.toolDefs()}
		return base, true
	case "tools/call":
		result, perr := s.dispatchTool(ctx, req.Params)
		if perr != nil {
			base.Error = perr
		} else {
			base.Result = result
		}
		return base, true
	default:
		base.Error = &proto.Error{Code: proto.CodeMethodNotFound, Message: "method not found: " + req.Method}
		return base, true
	}
}

// toolRegistry lazily builds the full registry. The four config-write tools are
// omitted when DisableConfigWrite is set, so they vanish from tools/list rather
// than failing on call. First invoked from the single-threaded read loop, so the
// lazy init is race-free.
func (s *Server) toolRegistry() *mcpserver.Registry {
	if s.registry == nil {
		r := mcpserver.NewRegistry()
		appSvc := s.application()
		writeEnabled := !s.DisableConfigWrite
		mcpserver.Register(r, tools.ResolveTool(appSvc))
		mcpserver.Register(r, tools.ProbeTool(appSvc))
		mcpserver.Register(r, tools.DescribeTool(appSvc))
		mcpserver.Register(r, tools.DoctorTool(appSvc, writeEnabled))
		mcpserver.Register(r, tools.ConfigListTool(writeEnabled))
		mcpserver.Register(r, tools.InvokePlanTool(appSvc))
		mcpserver.Register(r, tools.InvokeTool(appSvc))
		if writeEnabled {
			mcpserver.Register(r, tools.ConfigSaveProjectTool())
			mcpserver.Register(r, tools.ConfigSaveServerTool())
			mcpserver.Register(r, tools.ConfigRemoveProjectTool())
			mcpserver.Register(r, tools.ConfigRemoveServerTool())
		}
		s.registry = r
	}
	return s.registry
}

func (s *Server) toolDefs() []map[string]interface{} {
	return s.toolRegistry().ToolList()
}

// dispatchTool routes a tools/call to the typed registry. A malformed params
// envelope or a strict-decode failure surfaces as a JSON-RPC error.
func (s *Server) dispatchTool(ctx context.Context, rawParams json.RawMessage) (interface{}, *proto.Error) {
	var call struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if len(rawParams) > 0 {
		if err := decodeJSON(rawParams, &call); err != nil {
			return nil, &proto.Error{Code: proto.CodeInvalidParams, Message: "invalid tools/call params"}
		}
	}
	return s.toolRegistry().Call(ctx, mcpserver.SessionRuntime{}, call.Name, call.Arguments)
}

func (s *Server) application() *app.Service {
	if s.App != nil {
		return s.App
	}
	return app.New(nil)
}
