package mcp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/sofarpc/cli/internal/appconfig"
	"github.com/sofarpc/cli/internal/engine"
	"github.com/sofarpc/cli/internal/invoker"
	"github.com/sofarpc/cli/internal/launcher"
	"github.com/sofarpc/cli/internal/protocol"
	"github.com/sofarpc/cli/internal/schema"
)

type Server struct {
	BuildVersion       string
	Stdin              io.Reader
	Stdout             io.Writer
	Stderr             io.Writer
	DisableConfigWrite bool

	currentProject string
}

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  interface{}     `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
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
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var req request
		if err := decodeJSON(line, &req); err != nil {
			_ = write(out, response{JSONRPC: "2.0", Error: &rpcError{Code: -32700, Message: err.Error()}})
			continue
		}
		resp, shouldReply := s.handle(req)
		if shouldReply {
			_ = write(out, resp)
		}
	}
	if err := scanner.Err(); err != nil {
		fmt.Fprintln(stderr, "mcp:", err)
		return 1
	}
	return 0
}

func (s *Server) handle(req request) (response, bool) {
	base := response{JSONRPC: "2.0", ID: req.ID}
	switch req.Method {
	case "initialize":
		base.Result = map[string]interface{}{
			"protocolVersion": "2024-11-05",
			"capabilities": map[string]interface{}{
				"tools": map[string]interface{}{},
			},
			"serverInfo": map[string]interface{}{
				"name":    "sofarpc-mcp",
				"version": s.BuildVersion,
			},
		}
		return base, true
	case "notifications/initialized":
		return response{}, false
	case "tools/list":
		base.Result = map[string]interface{}{"tools": s.tools()}
		return base, true
	case "tools/call":
		result := s.handleToolCall(req.Params)
		base.Result = result
		return base, true
	default:
		base.Error = &rpcError{Code: -32601, Message: "method not found: " + req.Method}
		return base, true
	}
}

func (s *Server) tools() []tool {
	tools := []tool{
		{Name: "engine_status", Description: "Inspect Engine status without starting it.", InputSchema: objectSchema(nil)},
		{Name: "list_projects", Description: "List configured local source projects.", InputSchema: objectSchema(nil)},
		{Name: "set_current_project", Description: "Set the session-only current project.", InputSchema: objectSchema(map[string]interface{}{"project": stringSchema("Project name.")}, "project")},
		{Name: "list_servers", Description: "List configured SofaRPC servers.", InputSchema: objectSchema(map[string]interface{}{"project": stringSchema("Optional project filter.")})},
		{Name: "ping_service", Description: "Check a configured server/service transport path; this does not prove the remote method exists.", InputSchema: objectSchema(map[string]interface{}{
			"server":    stringSchema("Configured server name."),
			"service":   stringSchema("Service interface FQN."),
			"timeoutMs": numberSchema("Optional total timeout in milliseconds."),
		}, "server", "service")},
		{Name: "search_interface", Description: "Search local Java source for SofaRPC service/method candidates.", InputSchema: objectSchema(map[string]interface{}{
			"project":            stringSchema("Optional project name; defaults to current project when set."),
			"query":              stringSchema("Natural language or identifier query."),
			"limit":              numberSchema("Max candidates; default 5, max 20."),
			"includeOutOfPrefix": boolSchema("Include services outside configured servicePrefixes."),
		}, "query")},
		{Name: "describe_interface", Description: "Describe methods and visible DTO fields for a service FQN from local Java source.", InputSchema: objectSchema(map[string]interface{}{
			"project": stringSchema("Optional project name; defaults to current project when set."),
			"service": stringSchema("Service interface FQN."),
			"method":  stringSchema("Optional method filter."),
		}, "service")},
		{Name: "invoke_method", Description: "Invoke a SofaRPC method with explicit parameter types and ordered arguments, or named arguments when source schema can resolve the method.", InputSchema: objectSchema(map[string]interface{}{
			"server":           stringSchema("Configured server name."),
			"service":          stringSchema("Service interface FQN."),
			"method":           stringSchema("Method name."),
			"engine":           stringSchema("Optional invoke engine: java, go, or auto. Defaults to config engine.mode."),
			"paramTypes":       arraySchema("Optional Java parameter type FQNs for overload disambiguation."),
			"orderedArguments": arraySchema("Arguments in method parameter order."),
			"arguments":        objectSchema(nil),
			"timeoutMs":        numberSchema("Optional total timeout in milliseconds."),
		}, "server", "service", "method")},
	}
	if !s.DisableConfigWrite {
		tools = append(tools,
			tool{Name: "add_project", Description: "Add or replace a project in ~/.sofarpc/config.json.", InputSchema: objectSchema(map[string]interface{}{
				"name":            stringSchema("Project name."),
				"workspaceRoot":   stringSchema("Absolute or ~-relative local source root."),
				"servicePrefixes": arraySchema("Optional Java service package prefixes."),
				"overwrite":       boolSchema("Allow replacing an existing project."),
			}, "name", "workspaceRoot")},
			tool{Name: "remove_project", Description: "Remove a project from ~/.sofarpc/config.json.", InputSchema: objectSchema(map[string]interface{}{
				"name":    stringSchema("Project name."),
				"confirm": boolSchema("Must be true."),
				"cascade": boolSchema("Also remove servers bound to the project."),
			}, "name", "confirm")},
			tool{Name: "add_server", Description: "Add or replace a SofaRPC server in ~/.sofarpc/config.json.", InputSchema: objectSchema(map[string]interface{}{
				"name":        stringSchema("Server name."),
				"address":     stringSchema("host:port."),
				"project":     stringSchema("Bound project name."),
				"protocol":    stringSchema("Protocol; default bolt."),
				"timeoutMs":   numberSchema("Default total timeout in milliseconds."),
				"appName":     stringSchema("SofaRPC consumer app name."),
				"attachments": objectSchema(nil),
				"overwrite":   boolSchema("Allow replacing an existing server."),
			}, "name", "address", "project")},
			tool{Name: "remove_server", Description: "Remove a server from ~/.sofarpc/config.json.", InputSchema: objectSchema(map[string]interface{}{
				"name":    stringSchema("Server name."),
				"confirm": boolSchema("Must be true."),
			}, "name", "confirm")},
		)
	}
	return tools
}

func (s *Server) handleToolCall(raw json.RawMessage) toolResult {
	var params struct {
		Name      string                 `json:"name"`
		Arguments map[string]interface{} `json:"arguments"`
	}
	if err := decodeJSON(raw, &params); err != nil {
		return toolErr("invalid tools/call params", err)
	}
	if params.Arguments == nil {
		params.Arguments = map[string]interface{}{}
	}
	switch params.Name {
	case "engine_status":
		return s.engineStatus()
	case "list_projects":
		return s.listProjects()
	case "set_current_project":
		return s.setCurrentProject(params.Arguments)
	case "list_servers":
		return s.listServers(params.Arguments)
	case "add_project":
		return s.addProject(params.Arguments)
	case "remove_project":
		return s.removeProject(params.Arguments)
	case "add_server":
		return s.addServer(params.Arguments)
	case "remove_server":
		return s.removeServer(params.Arguments)
	case "ping_service":
		return s.pingService(params.Arguments)
	case "search_interface":
		return s.searchInterface(params.Arguments)
	case "describe_interface":
		return s.describeInterface(params.Arguments)
	case "invoke_method":
		return s.invokeMethod(params.Arguments)
	default:
		return toolErr("unknown tool", fmt.Errorf("%s", params.Name))
	}
}

func (s *Server) listProjects() toolResult {
	cfg, err := loadConfig()
	if err != nil {
		return toolErr("config read failed", err)
	}
	projects := make([]map[string]interface{}, 0, len(cfg.Projects))
	for _, name := range cfg.ProjectNames() {
		projects = append(projects, map[string]interface{}{"name": name, "project": cfg.Projects[name]})
	}
	return toolOK(fmt.Sprintf("%d project(s) configured.", len(projects)), map[string]interface{}{"projects": projects, "currentProject": s.currentProject})
}

func (s *Server) listServers(args map[string]interface{}) toolResult {
	cfg, err := loadConfig()
	if err != nil {
		return toolErr("config read failed", err)
	}
	project, _ := stringArg(args, "project", false)
	servers := make([]map[string]interface{}, 0, len(cfg.Servers))
	for _, name := range cfg.ServerNames() {
		server := cfg.Servers[name]
		if project != "" && server.Project != project {
			continue
		}
		servers = append(servers, map[string]interface{}{"name": name, "server": server})
	}
	return toolOK(fmt.Sprintf("%d server(s) configured.", len(servers)), map[string]interface{}{"servers": servers})
}

func (s *Server) setCurrentProject(args map[string]interface{}) toolResult {
	project, err := stringArg(args, "project", true)
	if err != nil {
		return toolErr("bad arguments", err)
	}
	cfg, err := loadConfig()
	if err != nil {
		return toolErr("config read failed", err)
	}
	if _, ok := cfg.Projects[project]; !ok {
		return toolErr("project not found", fmt.Errorf("%q", project))
	}
	s.currentProject = project
	return toolOK("Current project set for this MCP session.", map[string]interface{}{"currentProject": project})
}

func (s *Server) addProject(args map[string]interface{}) toolResult {
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
		return toolErr("add_project failed", err)
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
	if s.currentProject == name {
		s.currentProject = ""
	}
	return toolOK("Project removed from config.json.", map[string]interface{}{"removed": name})
}

func (s *Server) addServer(args map[string]interface{}) toolResult {
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
		return toolErr("add_server failed", err)
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

func (s *Server) engineStatus() toolResult {
	cfg, cfgErr := loadConfig()
	if cfgErr != nil {
		return toolErr("config read failed", cfgErr)
	}
	paths, err := launcher.DefaultPaths()
	if err != nil {
		return toolErr("launcher path failed", err)
	}
	state, stateErr := launcher.ReadState(paths.StateFile)
	running := stateErr == nil && state != nil && launcher.IsPIDAlive(state.PID)
	data := map[string]interface{}{
		"running":         running,
		"state":           state,
		"stateError":      errorString(stateErr),
		"desired":         cfg.Engine,
		"logFile":         paths.LogFile,
		"restartRequired": false,
	}
	if state != nil && cfg.Engine.Port != 0 && state.Port != cfg.Engine.Port {
		data["restartRequired"] = true
	}
	return toolOK(statusSummary(running), data)
}

func (s *Server) pingService(args map[string]interface{}) toolResult {
	serverName, err := stringArg(args, "server", true)
	if err != nil {
		return toolErr("bad arguments", err)
	}
	service, err := stringArg(args, "service", true)
	if err != nil {
		return toolErr("bad arguments", err)
	}
	cfg, server, err := serverConfig(serverName)
	if err != nil {
		return toolErr("server resolution failed", err)
	}
	timeoutMS := intArgDefault(args, "timeoutMs", server.TimeoutMS)
	var resp protocol.Response
	if err := callEngine("sofarpc.ping", map[string]interface{}{
		"address":      server.Address,
		"service":      service,
		"rpcTimeoutMs": timeoutMS,
	}, &resp, s.BuildVersion); err != nil {
		return toolErr("ping_service failed", err)
	}
	data := map[string]interface{}{"server": serverName, "project": server.Project, "service": service, "engineResponse": resp, "configEngine": cfg.Engine}
	return toolOK("Ping completed. Success only means the local transport path was usable; it does not prove the remote interface or method exists.", data)
}

func (s *Server) searchInterface(args map[string]interface{}) toolResult {
	query, err := stringArg(args, "query", true)
	if err != nil {
		return toolErr("bad arguments", err)
	}
	cfg, err := loadConfig()
	if err != nil {
		return toolErr("config read failed", err)
	}
	projectName, project, err := s.resolveProject(cfg, args, "")
	if err != nil {
		return toolErr("project resolution failed", err)
	}
	idx, err := schema.LoadOrBuildIndex(schema.Project{
		Name:            projectName,
		WorkspaceRoot:   project.WorkspaceRoot,
		ServicePrefixes: project.ServicePrefixes,
	})
	if err != nil {
		return toolErr("source index failed", err)
	}
	limit := intArgDefault(args, "limit", 5)
	results := schema.Search(idx, query, limit, boolArg(args, "includeOutOfPrefix"))
	return toolOK(fmt.Sprintf("%d candidate(s) found.", len(results)), map[string]interface{}{
		"project":    projectName,
		"query":      query,
		"candidates": publicMethods(results),
	})
}

func (s *Server) describeInterface(args map[string]interface{}) toolResult {
	service, err := stringArg(args, "service", true)
	if err != nil {
		return toolErr("bad arguments", err)
	}
	method, err := stringArg(args, "method", false)
	if err != nil {
		return toolErr("bad arguments", err)
	}
	cfg, err := loadConfig()
	if err != nil {
		return toolErr("config read failed", err)
	}
	projectName, project, err := s.resolveProject(cfg, args, "")
	if err != nil {
		return toolErr("project resolution failed", err)
	}
	idx, err := schema.LoadOrBuildIndex(schema.Project{
		Name:            projectName,
		WorkspaceRoot:   project.WorkspaceRoot,
		ServicePrefixes: project.ServicePrefixes,
	})
	if err != nil {
		return toolErr("source index failed", err)
	}
	desc, err := schema.Describe(idx, service, method)
	if err != nil {
		return toolErr("describe_interface failed", err)
	}
	return toolOK(fmt.Sprintf("%d method(s) described.", len(desc.Methods)), map[string]interface{}{
		"project":     projectName,
		"description": publicDescription(desc),
	})
}

func (s *Server) invokeMethod(args map[string]interface{}) toolResult {
	serverName, err := stringArg(args, "server", true)
	if err != nil {
		return toolErr("bad arguments", err)
	}
	service, err := stringArg(args, "service", true)
	if err != nil {
		return toolErr("bad arguments", err)
	}
	method, err := stringArg(args, "method", true)
	if err != nil {
		return toolErr("bad arguments", err)
	}
	paramTypes, err := stringSliceArg(args, "paramTypes")
	if err != nil {
		return toolErr("bad arguments", err)
	}
	cfg, server, err := serverConfig(serverName)
	if err != nil {
		return toolErr("server resolution failed", err)
	}
	ordered, paramTypes, err := s.resolveInvokeArguments(cfg, serverName, service, method, args, paramTypes)
	if err != nil {
		return toolErr("argument resolution failed", err)
	}
	timeoutMS := intArgDefault(args, "timeoutMs", server.TimeoutMS)
	engineMode, err := stringArg(args, "engine", false)
	if err != nil {
		return toolErr("bad arguments", err)
	}
	payload := protocol.InvokePayload{
		Address:      server.Address,
		Service:      service,
		Method:       method,
		ArgTypes:     paramTypes,
		Args:         ordered,
		RPCTimeoutMS: timeoutMS,
	}
	var resp protocol.Response
	mode := normalizeMCPInvokeEngineMode(engineMode)
	if strings.TrimSpace(engineMode) == "" {
		mode = normalizeMCPInvokeEngineMode(cfg.Engine.Mode)
	}
	if mode == appconfig.EngineModeGo || mode == appconfig.EngineModeAuto {
		req, err := protocol.NewRequest(protocol.OpInvoke, payload)
		if err != nil {
			return toolErr("invoke_method failed", err)
		}
		directResp, err := invoker.DirectRequest(req)
		if err != nil {
			return toolErr("invoke_method failed", err)
		}
		resp = *directResp
	} else {
		if err := callEngine("sofarpc.invoke", map[string]interface{}{
			"address":      payload.Address,
			"service":      payload.Service,
			"method":       payload.Method,
			"argTypes":     payload.ArgTypes,
			"args":         payload.Args,
			"rpcTimeoutMs": payload.RPCTimeoutMS,
		}, &resp, s.BuildVersion); err != nil {
			return toolErr("invoke_method failed", err)
		}
	}
	return toolOK("Invoke completed.", map[string]interface{}{"server": serverName, "service": service, "method": method, "engineResponse": resp})
}

func (s *Server) resolveInvokeArguments(cfg appconfig.Config, serverName, service, method string, args map[string]interface{}, paramTypes []string) ([]interface{}, []string, error) {
	if raw, ok := args["orderedArguments"]; ok && raw != nil {
		ordered, ok := raw.([]interface{})
		if !ok {
			return nil, nil, fmt.Errorf("orderedArguments must be an array")
		}
		if len(paramTypes) == 0 {
			resolvedTypes, err := s.resolveParamTypes(cfg, serverName, service, method, len(ordered), nil)
			if err != nil {
				return nil, nil, err
			}
			paramTypes = resolvedTypes
		}
		if len(paramTypes) != len(ordered) {
			return nil, nil, fmt.Errorf("paramTypes length (%d) does not match orderedArguments length (%d)", len(paramTypes), len(ordered))
		}
		return ordered, paramTypes, nil
	}
	named, ok := args["arguments"].(map[string]interface{})
	if !ok {
		return nil, nil, fmt.Errorf("either orderedArguments or arguments is required")
	}
	methodSchema, err := s.resolveMethodSchema(cfg, serverName, service, method, paramTypes)
	if err != nil {
		return nil, nil, err
	}
	if len(methodSchema.Parameters) == 1 {
		param := methodSchema.Parameters[0]
		if _, ok := named[param.Name]; !ok {
			return []interface{}{named}, []string{rpcParamTypeForMethod(param.Type, methodSchema)}, nil
		}
	}
	ordered := make([]interface{}, 0, len(methodSchema.Parameters))
	resolvedTypes := make([]string, 0, len(methodSchema.Parameters))
	for _, param := range methodSchema.Parameters {
		value, ok := named[param.Name]
		if !ok {
			return nil, nil, fmt.Errorf("missing argument %q", param.Name)
		}
		ordered = append(ordered, value)
		resolvedTypes = append(resolvedTypes, rpcParamTypeForMethod(param.Type, methodSchema))
	}
	return ordered, resolvedTypes, nil
}

func (s *Server) resolveParamTypes(cfg appconfig.Config, serverName, service, method string, count int, paramTypes []string) ([]string, error) {
	methodSchema, err := s.resolveMethodSchema(cfg, serverName, service, method, paramTypes)
	if err != nil {
		return nil, err
	}
	if len(methodSchema.Parameters) != count {
		return nil, fmt.Errorf("resolved method has %d parameters, got %d arguments", len(methodSchema.Parameters), count)
	}
	out := make([]string, 0, len(methodSchema.Parameters))
	for _, param := range methodSchema.Parameters {
		out = append(out, rpcParamTypeForMethod(param.Type, methodSchema))
	}
	return out, nil
}

func (s *Server) resolveMethodSchema(cfg appconfig.Config, serverName, service, method string, paramTypes []string) (schema.Method, error) {
	projectName, project, err := s.resolveProject(cfg, nilSafeArgs(nil), serverName)
	if err != nil {
		return schema.Method{}, err
	}
	idx, err := schema.LoadOrBuildIndex(schema.Project{
		Name:            projectName,
		WorkspaceRoot:   project.WorkspaceRoot,
		ServicePrefixes: project.ServicePrefixes,
	})
	if err != nil {
		return schema.Method{}, err
	}
	desc, err := schema.Describe(idx, service, method)
	if err != nil {
		return schema.Method{}, err
	}
	var matches []schema.Method
	for _, candidate := range desc.Methods {
		if len(paramTypes) > 0 && !sameParamTypes(candidate, paramTypes) {
			continue
		}
		matches = append(matches, candidate)
	}
	if len(matches) == 0 {
		return schema.Method{}, fmt.Errorf("method %s.%s not found for supplied paramTypes", service, method)
	}
	if len(matches) > 1 {
		return schema.Method{}, fmt.Errorf("method %s.%s is overloaded; provide paramTypes", service, method)
	}
	return matches[0], nil
}

func (s *Server) resolveProject(cfg appconfig.Config, args map[string]interface{}, serverName string) (string, appconfig.Project, error) {
	explicit, err := stringArg(args, "project", false)
	if err != nil {
		return "", appconfig.Project{}, err
	}
	if explicit != "" {
		project, ok := cfg.Projects[explicit]
		if !ok {
			return "", appconfig.Project{}, fmt.Errorf("project %q not found", explicit)
		}
		return explicit, project, nil
	}
	if serverName != "" {
		server, ok := cfg.Servers[serverName]
		if !ok {
			return "", appconfig.Project{}, fmt.Errorf("server %q not found", serverName)
		}
		project, ok := cfg.Projects[server.Project]
		if !ok {
			return "", appconfig.Project{}, fmt.Errorf("server %q references missing project %q", serverName, server.Project)
		}
		return server.Project, project, nil
	}
	if s.currentProject != "" {
		project, ok := cfg.Projects[s.currentProject]
		if ok {
			return s.currentProject, project, nil
		}
	}
	return "", appconfig.Project{}, fmt.Errorf("project is required")
}

func nilSafeArgs(args map[string]interface{}) map[string]interface{} {
	if args == nil {
		return map[string]interface{}{}
	}
	return args
}

func publicMethods(methods []schema.Method) []schema.Method {
	out := make([]schema.Method, len(methods))
	copy(out, methods)
	for i := range out {
		out[i].Imports = nil
	}
	return out
}

func publicDescription(desc schema.Description) schema.Description {
	desc.Methods = publicMethods(desc.Methods)
	if len(desc.Types) > 0 {
		types := make(map[string]schema.TypeSchema, len(desc.Types))
		for name, typ := range desc.Types {
			typ.Imports = nil
			types[name] = typ
		}
		desc.Types = types
	}
	return desc
}

func sameParamTypes(method schema.Method, types []string) bool {
	if len(method.Parameters) != len(types) {
		return false
	}
	for i := range method.Parameters {
		if rpcParamTypeForMethod(method.Parameters[i].Type, method) != rpcParamTypeForMethod(types[i], method) {
			return false
		}
	}
	return true
}

func rpcParamTypeForMethod(typ string, method schema.Method) string {
	base := eraseRPCGeneric(typ)
	if base == "" {
		return typ
	}
	mapped := rpcParamType(base)
	if mapped != base || strings.Contains(mapped, ".") || isPrimitiveRPCType(mapped) {
		return mapped
	}
	if imported, ok := method.Imports[base]; ok {
		return imported
	}
	if method.Package != "" {
		return method.Package + "." + base
	}
	return base
}

func rpcParamType(typ string) string {
	switch typ {
	case "String":
		return "java.lang.String"
	case "Integer":
		return "java.lang.Integer"
	case "Long":
		return "java.lang.Long"
	case "Boolean":
		return "java.lang.Boolean"
	case "Double":
		return "java.lang.Double"
	case "Float":
		return "java.lang.Float"
	case "Short":
		return "java.lang.Short"
	case "Byte":
		return "java.lang.Byte"
	case "Character":
		return "java.lang.Character"
	case "BigDecimal":
		return "java.math.BigDecimal"
	case "Date":
		return "java.util.Date"
	case "List":
		return "java.util.List"
	case "Map":
		return "java.util.Map"
	case "Set":
		return "java.util.Set"
	default:
		return typ
	}
}

func normalizeMCPInvokeEngineMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case appconfig.EngineModeGo:
		return appconfig.EngineModeGo
	case appconfig.EngineModeAuto:
		return appconfig.EngineModeAuto
	default:
		return appconfig.EngineModeJava
	}
}

func eraseRPCGeneric(typ string) string {
	base := strings.TrimSpace(typ)
	base = strings.TrimPrefix(base, "final ")
	if idx := strings.Index(base, "<"); idx >= 0 {
		base = strings.TrimSpace(base[:idx])
	}
	return strings.TrimSuffix(base, "[]")
}

func isPrimitiveRPCType(typ string) bool {
	switch typ {
	case "boolean", "byte", "char", "short", "int", "long", "float", "double", "void":
		return true
	default:
		return false
	}
}

func callEngine(method string, params interface{}, out interface{}, buildVersion string) error {
	cfg, err := launcher.DefaultConfig(buildVersion)
	if err != nil {
		return err
	}
	appCfg, err := loadConfig()
	if err != nil {
		return err
	}
	if appCfg.Engine.Port > 0 {
		cfg.Port = appCfg.Engine.Port
	}
	if appCfg.Engine.StartTimeoutMS > 0 {
		cfg.SpawnBudget = time.Duration(appCfg.Engine.StartTimeoutMS) * time.Millisecond
	}
	if idle, err := time.ParseDuration(appCfg.Engine.IdleTTL); err == nil && idle > 0 {
		cfg.IdleTTLMS = idle.Milliseconds()
	}
	if appCfg.Engine.JavaHome != nil && *appCfg.Engine.JavaHome != "" {
		cfg.JavaBin = filepath.Join(*appCfg.Engine.JavaHome, "bin", "java")
	}
	conn, err := launcher.Connect(cfg)
	if err != nil {
		return err
	}
	token, err := launcher.ReadToken(cfg.Paths.TokenFile)
	if err != nil {
		return err
	}
	client := engine.Client{
		Addr:           conn.Client.Addr,
		DialTimeout:    cfg.DialTimeout,
		RequestTimeout: cfg.RequestTimeout,
		Token:          token,
	}
	return client.CallAuthenticated(method, params, out)
}

func serverConfig(name string) (appconfig.Config, appconfig.Server, error) {
	cfg, err := loadConfig()
	if err != nil {
		return cfg, appconfig.Server{}, err
	}
	server, ok := cfg.Servers[name]
	if !ok {
		return cfg, appconfig.Server{}, fmt.Errorf("server %q not found", name)
	}
	return cfg, server, nil
}

func loadConfig() (appconfig.Config, error) {
	path, err := appconfig.DefaultPath()
	if err != nil {
		return appconfig.Config{}, err
	}
	return appconfig.Load(path)
}

func configPaths() (string, string, error) {
	path, err := appconfig.DefaultPath()
	if err != nil {
		return "", "", err
	}
	lock, err := appconfig.DefaultLockPath()
	if err != nil {
		return "", "", err
	}
	return path, lock, nil
}

func mutateOnly(path, lock string, mutate func(*appconfig.Config) error) error {
	_, err := appconfig.Update(path, lock, mutate)
	return err
}

func toolOK(summary string, data interface{}) toolResult {
	return toolResult{Content: []content{{Type: "text", Text: summary}}, StructuredContent: data}
}

func toolErr(summary string, err error) toolResult {
	data := map[string]interface{}{"ok": false, "message": summary}
	if err != nil {
		data["error"] = err.Error()
		if diag, ok := launcher.AsDiagnostic(err); ok {
			data["reason"] = diag.Reason
			for k, v := range diag.Details {
				data[k] = v
			}
		}
		var cfgErr *appconfig.ConfigError
		if errors.As(err, &cfgErr) {
			data["code"] = cfgErr.Code
			data["configPath"] = cfgErr.Path
		}
	}
	return toolResult{Content: []content{{Type: "text", Text: summary}}, StructuredContent: data, IsError: true}
}

func write(w io.Writer, resp response) error {
	body, err := json.Marshal(resp)
	if err != nil {
		return err
	}
	body = append(body, '\n')
	_, err = w.Write(body)
	return err
}

func decodeJSON(raw []byte, out interface{}) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	return dec.Decode(out)
}

func stringArg(args map[string]interface{}, key string, required bool) (string, error) {
	v, ok := args[key]
	if !ok || v == nil {
		if required {
			return "", fmt.Errorf("%s is required", key)
		}
		return "", nil
	}
	s, ok := v.(string)
	if !ok || s == "" {
		return "", fmt.Errorf("%s must be a non-empty string", key)
	}
	return s, nil
}

func stringArgDefault(args map[string]interface{}, key, def string) string {
	s, err := stringArg(args, key, false)
	if err != nil || s == "" {
		return def
	}
	return s
}

func stringSliceArg(args map[string]interface{}, key string) ([]string, error) {
	v, ok := args[key]
	if !ok || v == nil {
		return nil, nil
	}
	arr, ok := v.([]interface{})
	if !ok {
		return nil, fmt.Errorf("%s must be an array of strings", key)
	}
	out := make([]string, 0, len(arr))
	for i, item := range arr {
		s, ok := item.(string)
		if !ok {
			return nil, fmt.Errorf("%s[%d] must be a string", key, i)
		}
		out = append(out, s)
	}
	return out, nil
}

func stringMapArg(args map[string]interface{}, key string) (map[string]string, error) {
	v, ok := args[key]
	if !ok || v == nil {
		return map[string]string{}, nil
	}
	raw, ok := v.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("%s must be an object", key)
	}
	out := map[string]string{}
	for k, val := range raw {
		s, ok := val.(string)
		if !ok {
			return nil, fmt.Errorf("%s.%s must be a string", key, k)
		}
		out[k] = s
	}
	return out, nil
}

func boolArg(args map[string]interface{}, key string) bool {
	v, ok := args[key]
	if !ok {
		return false
	}
	b, _ := v.(bool)
	return b
}

func intArgDefault(args map[string]interface{}, key string, def int) int {
	v, ok := args[key]
	if !ok || v == nil {
		return def
	}
	switch n := v.(type) {
	case float64:
		if n <= 0 {
			return def
		}
		return int(n)
	case json.Number:
		i, err := strconv.Atoi(n.String())
		if err != nil || i <= 0 {
			return def
		}
		return i
	default:
		return def
	}
}

func statusSummary(running bool) string {
	if running {
		return "Engine appears to be running."
	}
	return "Engine is not running."
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func objectSchema(properties map[string]interface{}, required ...string) map[string]interface{} {
	if properties == nil {
		properties = map[string]interface{}{}
	}
	schema := map[string]interface{}{
		"type":                 "object",
		"properties":           properties,
		"additionalProperties": false,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func stringSchema(description string) map[string]interface{} {
	return map[string]interface{}{"type": "string", "description": description}
}

func numberSchema(description string) map[string]interface{} {
	return map[string]interface{}{"type": "integer", "description": description}
}

func boolSchema(description string) map[string]interface{} {
	return map[string]interface{}{"type": "boolean", "description": description}
}

func arraySchema(description string) map[string]interface{} {
	return map[string]interface{}{"type": "array", "description": description}
}
