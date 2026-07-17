package tools

import (
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

func endpointData(server appconfig.Server, timeoutMS int) map[string]interface{} {
	if timeoutMS <= 0 {
		timeoutMS = server.TimeoutMS
	}
	out := map[string]interface{}{
		"address":     server.Address,
		"protocol":    server.Protocol,
		"timeoutMs":   timeoutMS,
		"appName":     server.AppName,
		"attachments": redactAttachments(server.Attachments),
	}
	if server.Profile != "" {
		out["profile"] = server.Profile
	}
	return out
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
