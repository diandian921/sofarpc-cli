package tools

import (
	"fmt"
	"strings"

	"github.com/diandian921/sofarpc-mcp/internal/app"
	"github.com/diandian921/sofarpc-mcp/internal/appconfig"
	"github.com/diandian921/sofarpc-mcp/internal/schema"
)

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

// resolveProject picks the project to describe given an explicit project name
// and/or a bound server, mirroring the inference the facade used to do inline.
func resolveProject(cfg appconfig.Config, explicit, serverName string) (string, appconfig.Project, error) {
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

// resolveServer picks a single server given an explicit name and/or project
// filter. When required is false an ambiguous match returns hasServer=false.
func resolveServer(cfg appconfig.Config, project, explicit string, required bool) (string, appconfig.Server, bool, error) {
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
		"attachments": redactAttachments(server.Attachments),
	}
}

// publicMethods strips internal import bookkeeping from search/describe output.
func publicMethods(methods []schema.Method) []schema.Method {
	out := make([]schema.Method, len(methods))
	copy(out, methods)
	for i := range out {
		out[i].Imports = nil
	}
	return out
}

// publicSearchCandidate flattens a scored search hit into an agent-ready candidate:
// paramTypes are the normalized RPC identity types (app.RPCParamTypes — the same wire
// argTypes the planner uses, so they are copyable straight into an invoke), parameterNames
// are lifted out of the parameters array, the matched-token evidence becomes a single
// reason string, and internal bookkeeping (imports, package, sourceHash) is dropped.
func publicSearchCandidate(m schema.Method) map[string]interface{} {
	paramTypes := app.RPCParamTypes(m)
	paramNames := make([]string, len(m.Parameters))
	for i, p := range m.Parameters {
		paramNames[i] = p.Name
	}
	candidate := map[string]interface{}{
		"service":        m.Service,
		"method":         m.Method,
		"returnType":     m.ReturnType,
		"paramTypes":     paramTypes,
		"parameterNames": paramNames,
		"score":          m.Score,
		"reason":         strings.Join(m.Evidence, "; "),
		"sourceFile":     m.SourceFile,
	}
	if m.Summary != "" {
		candidate["summary"] = m.Summary
	}
	if m.OutOfPrefix {
		candidate["outOfPrefix"] = true
	}
	return candidate
}

// publicSearchCandidates maps a ranked search result list into agent-ready candidates,
// preserving the score order schema.Search already established.
func publicSearchCandidates(methods []schema.Method) []map[string]interface{} {
	out := make([]map[string]interface{}, len(methods))
	for i, m := range methods {
		out[i] = publicSearchCandidate(m)
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
