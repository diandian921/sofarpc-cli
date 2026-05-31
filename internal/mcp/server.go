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

type Server struct {
	BuildVersion       string
	Stdin              io.Reader
	Stdout             io.Writer
	Stderr             io.Writer
	DisableConfigWrite bool
	App                *app.Service

	registry *mcpserver.Registry
}

type content struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type toolResult struct {
	Content           []content   `json:"content"`
	StructuredContent interface{} `json:"structuredContent,omitempty"`
	IsError           bool        `json:"isError,omitempty"`
}

type tool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"inputSchema"`
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

// SelfTest brings up the server machinery — config path resolution, app
// service, tool list, and the initialize/tools/list request path — and exits
// without serving stdio. A config that points at a broken binary fails here
// instead of at first agent use.
func (s *Server) SelfTest() error {
	if _, err := appconfig.DefaultPath(); err != nil {
		return fmt.Errorf("config path: %w", err)
	}
	if s.application() == nil {
		return errors.New("app service is nil")
	}
	if len(s.allToolDefs()) == 0 {
		return errors.New("no tools registered")
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
		base.Result = map[string]interface{}{"tools": s.allToolDefs()}
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

// toolRegistry lazily builds the registry of migrated typed tools. It is first
// invoked from the single-threaded read loop, so the lazy init is race-free.
func (s *Server) toolRegistry() *mcpserver.Registry {
	if s.registry == nil {
		r := mcpserver.NewRegistry()
		appSvc := s.application()
		mcpserver.Register(r, tools.ResolveTool(appSvc))
		mcpserver.Register(r, tools.ProbeTool(appSvc))
		mcpserver.Register(r, tools.DescribeTool(appSvc))
		mcpserver.Register(r, tools.DoctorTool(appSvc, !s.DisableConfigWrite))
		s.registry = r
	}
	return s.registry
}

// allToolDefs merges the migrated registry tools with the legacy config/invoke
// definitions still served by the facade.
func (s *Server) allToolDefs() []interface{} {
	defs := []interface{}{}
	for _, def := range s.toolRegistry().ToolList() {
		defs = append(defs, def)
	}
	for _, def := range s.legacyTools() {
		defs = append(defs, def)
	}
	return defs
}

// legacyTools are the tool definitions not yet migrated to the typed registry.
func (s *Server) legacyTools() []tool {
	return []tool{
		{Name: "sofarpc_config", Description: "List or update ~/.sofarpc/config.json. Mutating actions fail when config writes are disabled.", InputSchema: objectSchema(map[string]interface{}{
			"action":          enumStringSchema("Action: list, save_project, remove_project, save_server, or remove_server.", "list", "save_project", "remove_project", "save_server", "remove_server"),
			"name":            stringSchema("Project or server name for save/remove actions."),
			"project":         stringSchema("Project filter for list, or bound project for save_server."),
			"workspaceRoot":   stringSchema("Absolute or ~-relative local source root for save_project."),
			"servicePrefixes": arraySchema("Optional Java service package prefixes for save_project."),
			"address":         stringSchema("host:port for save_server."),
			"protocol":        stringSchema("Protocol for save_server; default bolt."),
			"timeoutMs":       numberSchema("Default total timeout in milliseconds."),
			"appName":         stringSchema("SofaRPC consumer app name."),
			"attachments":     stringMapSchema("Optional static SofaRPC attachments for save_server."),
			"overwrite":       boolSchema("Allow replacing an existing project or server."),
			"confirm":         boolSchema("Must be true for remove actions."),
			"cascade":         boolSchema("When removing a project, also remove servers bound to it."),
		})},
		{Name: "sofarpc_invoke", Description: "Invoke a SofaRPC method, or return the planned request when dryRun=true.", InputSchema: objectSchema(map[string]interface{}{
			"server":           stringSchema("Configured server name. Optional only when exactly one matching server can be inferred."),
			"project":          stringSchema("Optional project name used to infer a single bound server."),
			"service":          stringSchema("Service interface FQN."),
			"method":           stringSchema("Method name."),
			"paramTypes":       arraySchema("Optional Java parameter type FQNs for overload disambiguation."),
			"types":            arraySchema("Alias for paramTypes."),
			"orderedArguments": arraySchema("Arguments in method parameter order."),
			"args":             arraySchema("Alias for orderedArguments."),
			"arguments":        freeObjectSchema("Named arguments keyed by Java parameter name, or a single DTO object when the method has one parameter."),
			"timeoutMs":        numberSchema("Optional total timeout in milliseconds."),
			"dryRun":           boolSchema("When true, return the resolved plan without sending a SofaRPC request."),
			"rawResult":        boolSchema("When true, include the decoded Java object shape alongside the flattened result."),
		}, "service", "method")},
	}
}

// dispatchTool routes a tools/call to the typed registry or the legacy switch.
// A strict-decode failure on the typed path surfaces as a JSON-RPC error.
func (s *Server) dispatchTool(ctx context.Context, rawParams json.RawMessage) (interface{}, *proto.Error) {
	var call struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if len(rawParams) > 0 {
		if err := decodeJSON(rawParams, &call); err != nil {
			return toolErr("invalid tools/call params", err), nil
		}
	}
	if s.toolRegistry().Has(call.Name) {
		return s.toolRegistry().Call(ctx, mcpserver.SessionRuntime{}, call.Name, call.Arguments)
	}
	args := map[string]interface{}{}
	if len(call.Arguments) > 0 {
		if err := decodeJSON(call.Arguments, &args); err != nil {
			return toolErr("invalid arguments", err), nil
		}
	}
	return s.legacyToolCall(ctx, call.Name, args), nil
}

func (s *Server) legacyToolCall(ctx context.Context, name string, args map[string]interface{}) toolResult {
	switch name {
	case "sofarpc_config":
		return s.config(args)
	case "sofarpc_invoke":
		return s.invoke(ctx, args)
	default:
		return toolErr("unknown tool", fmt.Errorf("%s", name))
	}
}

func (s *Server) application() *app.Service {
	if s.App != nil {
		return s.App
	}
	return app.New(nil)
}

func (s *Server) config(args map[string]interface{}) toolResult {
	action := stringArgDefault(args, "action", "list")
	switch action {
	case "list":
		return s.listConfig(args)
	case "save_project":
		return s.saveProject(args)
	case "remove_project":
		return s.removeProject(args)
	case "save_server":
		return s.saveServer(args)
	case "remove_server":
		return s.removeServer(args)
	default:
		return toolErr("bad arguments", fmt.Errorf("unknown config action %q", action))
	}
}

func (s *Server) listConfig(args map[string]interface{}) toolResult {
	cfg, err := loadConfig()
	if err != nil {
		return toolErr("config read failed", err)
	}
	path, pathErr := appconfig.DefaultPath()
	if pathErr != nil {
		return toolErr("config path failed", pathErr)
	}
	projectFilter, err := stringArg(args, "project", false)
	if err != nil {
		return toolErr("bad arguments", err)
	}
	projects := make([]map[string]interface{}, 0, len(cfg.Projects))
	for _, name := range cfg.ProjectNames() {
		if projectFilter != "" && name != projectFilter {
			continue
		}
		projects = append(projects, map[string]interface{}{"name": name, "project": cfg.Projects[name]})
	}
	servers := make([]map[string]interface{}, 0, len(cfg.Servers))
	for _, name := range cfg.ServerNames() {
		server := cfg.Servers[name]
		if projectFilter != "" && server.Project != projectFilter {
			continue
		}
		servers = append(servers, map[string]interface{}{"name": name, "server": server})
	}
	return toolOK("Config loaded.", map[string]interface{}{
		"configPath":    path,
		"writeEnabled":  !s.DisableConfigWrite,
		"projects":      projects,
		"servers":       servers,
		"projectFilter": projectFilter,
	})
}

func (s *Server) invoke(ctx context.Context, args map[string]interface{}) toolResult {
	dryRun, err := strictBoolArg(args, "dryRun")
	if err != nil {
		return toolErrRendered(app.RenderFailure(app.CodeBadRequest, err.Error(), nil))
	}
	input, err := invocationInput(args)
	if err != nil {
		return toolErr("bad arguments", err)
	}
	plan, err := s.application().PlanInvocation(ctx, input)
	if err != nil {
		// Render through the app contract so the agent receives stable code,
		// DomainError kind, and a nextTool recovery hint (the kinds raised
		// here — SERVICE_NOT_FOUND, METHOD_AMBIGUOUS, ARGUMENT_TYPE_MISMATCH —
		// are exactly the ones an agent needs to recover from).
		return toolErrRendered(app.RenderFailure(app.CodeBadRequest, err.Error(), app.DomainErrorDetails(err)))
	}
	requestID := app.NewRequestID("invoke")
	planData := plan.Display()
	planData["requestId"] = requestID
	if dryRun {
		return toolOK("Invoke dry run completed.", map[string]interface{}{"dryRun": true, "plan": planData})
	}
	resp := app.RenderExecution(s.application().ExecuteInvocation(ctx, plan))
	resp.RequestID = requestID
	return toolOK("Invoke completed.", map[string]interface{}{"plan": planData, "response": resp})
}

func invocationInput(args map[string]interface{}) (app.InvocationInput, error) {
	service, err := stringArg(args, "service", true)
	if err != nil {
		return app.InvocationInput{}, err
	}
	method, err := stringArg(args, "method", true)
	if err != nil {
		return app.InvocationInput{}, err
	}
	server, err := stringArg(args, "server", false)
	if err != nil {
		return app.InvocationInput{}, err
	}
	project, err := stringArg(args, "project", false)
	if err != nil {
		return app.InvocationInput{}, err
	}
	paramTypes, err := stringSliceArg(args, "paramTypes")
	if err != nil {
		return app.InvocationInput{}, err
	}
	if len(paramTypes) == 0 {
		paramTypes, err = stringSliceArg(args, "types")
		if err != nil {
			return app.InvocationInput{}, err
		}
	}
	input := app.InvocationInput{
		Project:    project,
		Server:     server,
		Service:    service,
		Method:     method,
		ParamTypes: paramTypes,
		TimeoutMS:  intArgDefault(args, "timeoutMs", 0),
		RawResult:  boolArg(args, "rawResult"),
	}
	raw, ok := args["orderedArguments"]
	if !ok || raw == nil {
		raw, ok = args["args"]
	}
	if ok && raw != nil {
		ordered, ok := raw.([]interface{})
		if !ok {
			return app.InvocationInput{}, fmt.Errorf("orderedArguments/args must be an array")
		}
		input.OrderedArguments = ordered
		input.HasOrderedArguments = true
		return input, nil
	}
	if named, ok := args["arguments"].(map[string]interface{}); ok {
		input.NamedArguments = named
	}
	return input, nil
}

func (s *Server) saveProject(args map[string]interface{}) toolResult {
	if s.DisableConfigWrite {
		return toolErr("config write tools are disabled", nil)
	}
	name, err := stringArg(args, "name", true)
	if err != nil {
		return toolErr("bad arguments", err)
	}
	root, err := stringArg(args, "workspaceRoot", true)
	if err != nil {
		return toolErr("bad arguments", err)
	}
	prefixes, err := stringSliceArg(args, "servicePrefixes")
	if err != nil {
		return toolErr("bad arguments", err)
	}
	overwrite := boolArg(args, "overwrite")
	path, lock, err := configPaths()
	if err != nil {
		return toolErr("config path failed", err)
	}
	var project appconfig.Project
	_, err = appconfig.Update(path, lock, func(cfg *appconfig.Config) error {
		var addErr error
		project, addErr = cfg.AddProject(name, root, prefixes, overwrite)
		return addErr
	})
	if err != nil {
		return toolErr("save_project failed", err)
	}
	return toolOK("Project saved to config.json.", map[string]interface{}{"name": name, "project": project})
}

func (s *Server) removeProject(args map[string]interface{}) toolResult {
	if s.DisableConfigWrite {
		return toolErr("config write tools are disabled", nil)
	}
	name, err := stringArg(args, "name", true)
	if err != nil {
		return toolErr("bad arguments", err)
	}
	path, lock, err := configPaths()
	if err != nil {
		return toolErr("config path failed", err)
	}
	err = mutateOnly(path, lock, func(cfg *appconfig.Config) error {
		return cfg.RemoveProject(name, boolArg(args, "confirm"), boolArg(args, "cascade"))
	})
	if err != nil {
		return toolErr("remove_project failed", err)
	}
	return toolOK("Project removed from config.json.", map[string]interface{}{"removed": name})
}

func (s *Server) saveServer(args map[string]interface{}) toolResult {
	if s.DisableConfigWrite {
		return toolErr("config write tools are disabled", nil)
	}
	name, err := stringArg(args, "name", true)
	if err != nil {
		return toolErr("bad arguments", err)
	}
	address, err := stringArg(args, "address", true)
	if err != nil {
		return toolErr("bad arguments", err)
	}
	project, err := stringArg(args, "project", true)
	if err != nil {
		return toolErr("bad arguments", err)
	}
	attachments, err := stringMapArg(args, "attachments")
	if err != nil {
		return toolErr("bad arguments", err)
	}
	server := appconfig.Server{
		Address:     address,
		Project:     project,
		Protocol:    stringArgDefault(args, "protocol", appconfig.DefaultServerProtocol),
		TimeoutMS:   intArgDefault(args, "timeoutMs", appconfig.DefaultServerTimeoutMS),
		AppName:     stringArgDefault(args, "appName", appconfig.DefaultServerAppName),
		Attachments: attachments,
	}
	path, lock, err := configPaths()
	if err != nil {
		return toolErr("config path failed", err)
	}
	var saved appconfig.Server
	_, err = appconfig.Update(path, lock, func(cfg *appconfig.Config) error {
		var addErr error
		saved, addErr = cfg.AddServer(name, server, boolArg(args, "overwrite"))
		return addErr
	})
	if err != nil {
		return toolErr("save_server failed", err)
	}
	return toolOK("Server saved to config.json.", map[string]interface{}{"name": name, "server": saved})
}

func (s *Server) removeServer(args map[string]interface{}) toolResult {
	if s.DisableConfigWrite {
		return toolErr("config write tools are disabled", nil)
	}
	name, err := stringArg(args, "name", true)
	if err != nil {
		return toolErr("bad arguments", err)
	}
	path, lock, err := configPaths()
	if err != nil {
		return toolErr("config path failed", err)
	}
	err = mutateOnly(path, lock, func(cfg *appconfig.Config) error {
		return cfg.RemoveServer(name, boolArg(args, "confirm"))
	})
	if err != nil {
		return toolErr("remove_server failed", err)
	}
	return toolOK("Server removed from config.json.", map[string]interface{}{"removed": name})
}
