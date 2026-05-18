package mcp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/sofarpc/cli/internal/appconfig"
	"github.com/sofarpc/cli/internal/invoker"
	"github.com/sofarpc/cli/internal/protocol"
	"github.com/sofarpc/cli/internal/schema"
)

type Server struct {
	BuildVersion       string
	Stdin              io.Reader
	Stdout             io.Writer
	Stderr             io.Writer
	DisableConfigWrite bool
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
		{Name: "sofarpc_resolve", Description: "Resolve the configured project, server, and invocation endpoint without touching the network.", InputSchema: objectSchema(map[string]interface{}{
			"project":   stringSchema("Optional configured project name."),
			"server":    stringSchema("Optional configured server name."),
			"timeoutMs": numberSchema("Optional timeout override to show on the resolved endpoint."),
		})},
		{Name: "sofarpc_probe", Description: "Probe TCP reachability for a configured server or explicit address; this does not prove an interface or method exists.", InputSchema: objectSchema(map[string]interface{}{
			"server":    stringSchema("Optional configured server name."),
			"address":   stringSchema("Optional explicit host:port. Used when server is omitted."),
			"service":   stringSchema("Optional service FQN for labeling diagnostics."),
			"timeoutMs": numberSchema("Optional total timeout in milliseconds."),
		})},
		{Name: "sofarpc_describe", Description: "Search local Java source or describe methods and DTO fields for a service FQN.", InputSchema: objectSchema(map[string]interface{}{
			"project":            stringSchema("Optional project name. Required when multiple projects are configured and server is omitted."),
			"server":             stringSchema("Optional server name used to infer the bound project."),
			"query":              stringSchema("Natural language or identifier query for search mode."),
			"service":            stringSchema("Service interface FQN for describe mode."),
			"method":             stringSchema("Optional method filter for describe mode."),
			"limit":              numberSchema("Max search candidates; default 5, max 20."),
			"includeOutOfPrefix": boolSchema("Include services outside configured servicePrefixes."),
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
		}, "service", "method")},
		{Name: "sofarpc_doctor", Description: "Run structured diagnostics for config, project source schema, and invocation prerequisites.", InputSchema: objectSchema(map[string]interface{}{
			"project": stringSchema("Optional project name."),
			"server":  stringSchema("Optional server name."),
			"service": stringSchema("Optional service interface FQN."),
			"method":  stringSchema("Optional method filter."),
		})},
	}
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
	case "sofarpc_config":
		return s.config(params.Arguments)
	case "sofarpc_resolve":
		return s.resolve(params.Arguments)
	case "sofarpc_probe":
		return s.probe(params.Arguments)
	case "sofarpc_describe":
		return s.describe(params.Arguments)
	case "sofarpc_invoke":
		return s.invoke(params.Arguments)
	case "sofarpc_doctor":
		return s.doctor(params.Arguments)
	default:
		return toolErr("unknown tool", fmt.Errorf("%s", params.Name))
	}
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

func (s *Server) resolve(args map[string]interface{}) toolResult {
	cfg, err := loadConfig()
	if err != nil {
		return toolErr("config read failed", err)
	}
	serverName, server, hasServer, err := s.resolveServer(cfg, args, false)
	if err != nil {
		return toolErr("server resolution failed", err)
	}
	if hasServer {
		project, ok := cfg.Projects[server.Project]
		if !ok {
			return toolErr("project resolution failed", fmt.Errorf("server %q references missing project %q", serverName, server.Project))
		}
		timeoutMS := intArgDefault(args, "timeoutMs", server.TimeoutMS)
		return toolOK("Endpoint resolved.", map[string]interface{}{
			"project":     server.Project,
			"projectInfo": project,
			"server":      serverName,
			"endpoint":    endpointData(server, timeoutMS),
			"network":     "not_probed",
		})
	}
	projectName, project, err := s.resolveProject(cfg, args, "")
	if err != nil {
		return toolErr("project resolution failed", err)
	}
	servers := boundServers(cfg, projectName)
	return toolOK("Project resolved; no single endpoint was selected.", map[string]interface{}{
		"project":     projectName,
		"projectInfo": project,
		"servers":     servers,
		"network":     "not_probed",
	})
}

func (s *Server) probe(args map[string]interface{}) toolResult {
	address, err := stringArg(args, "address", false)
	if err != nil {
		return toolErr("bad arguments", err)
	}
	service, err := stringArg(args, "service", false)
	if err != nil {
		return toolErr("bad arguments", err)
	}
	serverName := ""
	projectName := ""
	timeoutMS := intArgDefault(args, "timeoutMs", appconfig.DefaultServerTimeoutMS)
	if address == "" {
		cfg, err := loadConfig()
		if err != nil {
			return toolErr("config read failed", err)
		}
		var server appconfig.Server
		var hasServer bool
		serverName, server, hasServer, err = s.resolveServer(cfg, args, true)
		if err != nil {
			return toolErr("server resolution failed", err)
		}
		if !hasServer {
			return toolErr("server resolution failed", fmt.Errorf("server or address is required"))
		}
		address = server.Address
		projectName = server.Project
		timeoutMS = intArgDefault(args, "timeoutMs", server.TimeoutMS)
	}
	payload := protocol.PingPayload{
		Address:      address,
		Service:      service,
		RPCTimeoutMS: timeoutMS,
	}
	req, err := protocol.NewRequest(protocol.OpPing, payload)
	if err != nil {
		return toolErr("sofarpc_probe failed", err)
	}
	directResp, err := invoker.DirectRequest(req)
	if err != nil {
		return toolErr("sofarpc_probe failed", err)
	}
	resp := *directResp
	return toolOK("Probe completed. Success only means the TCP transport path was reachable; it does not prove the remote interface or method exists.", map[string]interface{}{
		"server":    serverName,
		"project":   projectName,
		"address":   address,
		"service":   service,
		"timeoutMs": timeoutMS,
		"response":  resp,
	})
}

func (s *Server) describe(args map[string]interface{}) toolResult {
	query, err := stringArg(args, "query", false)
	if err != nil {
		return toolErr("bad arguments", err)
	}
	service, err := stringArg(args, "service", false)
	if err != nil {
		return toolErr("bad arguments", err)
	}
	if query == "" && service == "" {
		return toolErr("bad arguments", fmt.Errorf("query or service is required"))
	}
	serverName, err := stringArg(args, "server", false)
	if err != nil {
		return toolErr("bad arguments", err)
	}
	cfg, err := loadConfig()
	if err != nil {
		return toolErr("config read failed", err)
	}
	projectName, project, err := s.resolveProject(cfg, args, serverName)
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
	data := map[string]interface{}{"project": projectName}
	var summary []string
	if query != "" {
		limit := intArgDefault(args, "limit", 5)
		results := schema.Search(idx, query, limit, boolArg(args, "includeOutOfPrefix"))
		data["query"] = query
		data["candidates"] = publicMethods(results)
		summary = append(summary, fmt.Sprintf("%d candidate(s) found", len(results)))
	}
	if service != "" {
		method, err := stringArg(args, "method", false)
		if err != nil {
			return toolErr("bad arguments", err)
		}
		desc, err := schema.Describe(idx, service, method)
		if err != nil {
			return toolErr("sofarpc_describe failed", err)
		}
		data["description"] = publicDescription(desc)
		summary = append(summary, fmt.Sprintf("%d method(s) described", len(desc.Methods)))
	}
	return toolOK(strings.Join(summary, "; ")+".", data)
}

func (s *Server) invoke(args map[string]interface{}) toolResult {
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
	if len(paramTypes) == 0 {
		paramTypes, err = stringSliceArg(args, "types")
		if err != nil {
			return toolErr("bad arguments", err)
		}
	}
	cfg, err := loadConfig()
	if err != nil {
		return toolErr("config read failed", err)
	}
	serverName, server, hasServer, err := s.resolveServer(cfg, args, true)
	if err != nil {
		return toolErr("server resolution failed", err)
	}
	if !hasServer {
		return toolErr("server resolution failed", fmt.Errorf("server is required"))
	}
	ordered, paramTypes, err := s.resolveInvokeArguments(cfg, serverName, service, method, args, paramTypes)
	if err != nil {
		return toolErr("argument resolution failed", err)
	}
	timeoutMS := intArgDefault(args, "timeoutMs", server.TimeoutMS)
	payload := protocol.InvokePayload{
		Address:      server.Address,
		Service:      service,
		Method:       method,
		ArgTypes:     paramTypes,
		Args:         ordered,
		RPCTimeoutMS: timeoutMS,
	}
	req, err := protocol.NewRequest(protocol.OpInvoke, payload)
	if err != nil {
		return toolErr("sofarpc_invoke failed", err)
	}
	plan := map[string]interface{}{
		"requestId":        req.RequestID,
		"server":           serverName,
		"project":          server.Project,
		"endpoint":         endpointData(server, timeoutMS),
		"service":          service,
		"method":           method,
		"paramTypes":       paramTypes,
		"orderedArguments": ordered,
		"payload":          payload,
	}
	if boolArg(args, "dryRun") {
		return toolOK("Invoke dry run completed.", map[string]interface{}{"dryRun": true, "plan": plan})
	}
	directResp, err := invoker.DirectRequest(req)
	if err != nil {
		return toolErr("sofarpc_invoke failed", err)
	}
	resp := *directResp
	return toolOK("Invoke completed.", map[string]interface{}{"plan": plan, "response": resp})
}

func (s *Server) doctor(args map[string]interface{}) toolResult {
	checks := []map[string]interface{}{}
	addCheck := func(name, status string, details map[string]interface{}) {
		if details == nil {
			details = map[string]interface{}{}
		}
		details["name"] = name
		details["status"] = status
		checks = append(checks, details)
	}
	cfg, err := loadConfig()
	if err != nil {
		addCheck("config", "failed", map[string]interface{}{"error": err.Error()})
		return toolResult{Content: []content{{Type: "text", Text: "Doctor found configuration errors."}}, StructuredContent: map[string]interface{}{"ok": false, "checks": checks}, IsError: true}
	}
	path, _ := appconfig.DefaultPath()
	addCheck("config", "ok", map[string]interface{}{"configPath": path, "projectCount": len(cfg.Projects), "serverCount": len(cfg.Servers), "writeEnabled": !s.DisableConfigWrite})

	serverName, server, hasServer, err := s.resolveServer(cfg, args, false)
	if err != nil {
		addCheck("server", "failed", map[string]interface{}{"error": err.Error()})
	} else if hasServer {
		addCheck("server", "ok", map[string]interface{}{"server": serverName, "endpoint": endpointData(server, server.TimeoutMS)})
	} else {
		addCheck("server", "skipped", map[string]interface{}{"reason": "no single server resolved"})
	}

	projectName := ""
	var project appconfig.Project
	if hasServer {
		projectName, project, err = s.resolveProject(cfg, args, serverName)
	} else {
		projectName, project, err = s.resolveProject(cfg, args, "")
	}
	if err != nil {
		addCheck("project", "failed", map[string]interface{}{"error": err.Error()})
	} else {
		addCheck("project", "ok", map[string]interface{}{"project": projectName, "workspaceRoot": project.WorkspaceRoot, "servicePrefixes": project.ServicePrefixes})
		idx, idxErr := schema.LoadOrBuildIndex(schema.Project{Name: projectName, WorkspaceRoot: project.WorkspaceRoot, ServicePrefixes: project.ServicePrefixes})
		if idxErr != nil {
			addCheck("source_schema", "failed", map[string]interface{}{"error": idxErr.Error()})
		} else {
			addCheck("source_schema", "ok", map[string]interface{}{"methodCount": len(idx.Methods), "typeCount": len(idx.Types)})
			service, _ := stringArg(args, "service", false)
			if service != "" {
				method, _ := stringArg(args, "method", false)
				desc, descErr := schema.Describe(idx, service, method)
				if descErr != nil {
					addCheck("describe", "failed", map[string]interface{}{"service": service, "method": method, "error": descErr.Error()})
				} else {
					addCheck("describe", "ok", map[string]interface{}{"service": service, "method": method, "methodCount": len(desc.Methods)})
				}
			}
		}
	}

	ok := true
	for _, check := range checks {
		if check["status"] == "failed" {
			ok = false
			break
		}
	}
	text := "Doctor completed."
	if !ok {
		text = "Doctor found issues."
	}
	return toolResult{Content: []content{{Type: "text", Text: text}}, StructuredContent: map[string]interface{}{"ok": ok, "checks": checks}, IsError: !ok}
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

func (s *Server) resolveInvokeArguments(cfg appconfig.Config, serverName, service, method string, args map[string]interface{}, paramTypes []string) ([]interface{}, []string, error) {
	raw, ok := args["orderedArguments"]
	if !ok || raw == nil {
		raw, ok = args["args"]
	}
	if ok && raw != nil {
		ordered, ok := raw.([]interface{})
		if !ok {
			return nil, nil, fmt.Errorf("orderedArguments/args must be an array")
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
		if serverName != "" {
			server, ok := cfg.Servers[serverName]
			if !ok {
				return "", appconfig.Project{}, fmt.Errorf("server %q not found", serverName)
			}
			if server.Project != explicit {
				return "", appconfig.Project{}, fmt.Errorf("server %q is bound to project %q, not %q", serverName, server.Project, explicit)
			}
		}
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
	if len(cfg.Projects) == 1 {
		for name, project := range cfg.Projects {
			return name, project, nil
		}
	}
	return "", appconfig.Project{}, fmt.Errorf("project is required")
}

func (s *Server) resolveServer(cfg appconfig.Config, args map[string]interface{}, required bool) (string, appconfig.Server, bool, error) {
	explicit, err := stringArg(args, "server", false)
	if err != nil {
		return "", appconfig.Server{}, false, err
	}
	project, err := stringArg(args, "project", false)
	if err != nil {
		return "", appconfig.Server{}, false, err
	}
	if explicit != "" {
		server, ok := cfg.Servers[explicit]
		if !ok {
			return "", appconfig.Server{}, false, fmt.Errorf("server %q not found", explicit)
		}
		if project != "" && server.Project != project {
			return "", appconfig.Server{}, false, fmt.Errorf("server %q is bound to project %q, not %q", explicit, server.Project, project)
		}
		return explicit, server, true, nil
	}

	var names []string
	for _, name := range cfg.ServerNames() {
		server := cfg.Servers[name]
		if project == "" || server.Project == project {
			names = append(names, name)
		}
	}
	if len(names) == 1 {
		name := names[0]
		return name, cfg.Servers[name], true, nil
	}
	if !required {
		return "", appconfig.Server{}, false, nil
	}
	if project != "" {
		return "", appconfig.Server{}, false, fmt.Errorf("server is required because project %q has %d configured servers", project, len(names))
	}
	return "", appconfig.Server{}, false, fmt.Errorf("server is required because %d servers are configured", len(names))
}

func endpointData(server appconfig.Server, timeoutMS int) map[string]interface{} {
	if timeoutMS <= 0 {
		timeoutMS = server.TimeoutMS
	}
	return map[string]interface{}{
		"address":     server.Address,
		"protocol":    server.Protocol,
		"timeoutMs":   timeoutMS,
		"appName":     server.AppName,
		"attachments": server.Attachments,
	}
}

func boundServers(cfg appconfig.Config, project string) []map[string]interface{} {
	servers := []map[string]interface{}{}
	for _, name := range cfg.ServerNames() {
		server := cfg.Servers[name]
		if server.Project != project {
			continue
		}
		servers = append(servers, map[string]interface{}{"name": name, "server": server})
	}
	return servers
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

func freeObjectSchema(description string) map[string]interface{} {
	return map[string]interface{}{"type": "object", "description": description, "additionalProperties": true}
}

func stringMapSchema(description string) map[string]interface{} {
	return map[string]interface{}{
		"type":        "object",
		"description": description,
		"additionalProperties": map[string]interface{}{
			"type": "string",
		},
	}
}

func stringSchema(description string) map[string]interface{} {
	return map[string]interface{}{"type": "string", "description": description}
}

func enumStringSchema(description string, values ...string) map[string]interface{} {
	enum := make([]interface{}, 0, len(values))
	for _, value := range values {
		enum = append(enum, value)
	}
	return map[string]interface{}{"type": "string", "description": description, "enum": enum}
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
