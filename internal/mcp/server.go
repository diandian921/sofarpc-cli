package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	session := proto.NewSession(proto.Config{
		In:           in,
		Out:          out,
		Stderr:       stderr,
		Info:         s.serverInfo(),
		Capabilities: s.serverCapabilities(),
		Instructions: serverInstructions,
		Dispatcher:   &dispatcher{server: s},
	})
	return session.Run()
}

// SelfTest brings up the server machinery — config path, app service, tool
// registry (including its invariants), version negotiation, and the tools/list
// path — and exits without serving stdio, so a broken config fails here instead
// of at first agent use.
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
	if _, verr := proto.NegotiateVersion("2025-06-18"); verr != nil {
		return fmt.Errorf("version negotiation failed: %s", verr.Message)
	}
	ctx := context.Background()
	if resp, _ := s.handle(ctx, proto.Request{JSONRPC: "2.0", Method: "tools/list"}); resp.Error != nil {
		return fmt.Errorf("tools/list failed: %s", resp.Error.Message)
	}
	return nil
}

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
