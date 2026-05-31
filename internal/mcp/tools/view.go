package tools

import (
	"github.com/diandian921/sofarpc-cli/internal/app"
	"github.com/diandian921/sofarpc-cli/internal/appconfig"
)

// redactedValue replaces every attachment value in MCP-facing output. Attachment
// values carry credentials (token / SSO / divisionId / tenant / trace); keys are
// kept so an agent sees what is configured, but the secret values never leave.
const redactedValue = "[redacted]"

// redactAttachments returns a new map with every value replaced by redactedValue,
// preserving keys. It never mutates the input and returns nil for a nil input so
// the "no attachments" shape is preserved.
func redactAttachments(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for k := range in {
		out[k] = redactedValue
	}
	return out
}

// publicServer renders an appconfig.Server for MCP output with attachment values
// redacted. Sole sanctioned exit for a server in structured content — never hand
// a raw appconfig.Server to success()/structuredContent.
func publicServer(s appconfig.Server) map[string]interface{} {
	return map[string]interface{}{
		"address":     s.Address,
		"project":     s.Project,
		"protocol":    s.Protocol,
		"timeoutMs":   s.TimeoutMS,
		"appName":     s.AppName,
		"attachments": redactAttachments(s.Attachments),
	}
}

// publicEndpoint renders an app.Endpoint for MCP output with attachment values
// redacted. Sole sanctioned exit for an endpoint in structured content. server /
// project are omitted when empty to mirror the struct's omitempty tags.
func publicEndpoint(e app.Endpoint) map[string]interface{} {
	out := map[string]interface{}{
		"address":     e.Address,
		"protocol":    e.Protocol,
		"timeoutMs":   e.TimeoutMS,
		"appName":     e.AppName,
		"attachments": redactAttachments(e.Attachments),
	}
	if e.Server != "" {
		out["server"] = e.Server
	}
	if e.Project != "" {
		out["project"] = e.Project
	}
	return out
}

// publicBoundServers lists the servers bound to project with attachment values
// redacted, mirroring app.boundServers but routed through publicServer. Re-listed
// from config in the tools layer so no caller hands a raw appconfig.Server out.
func publicBoundServers(cfg appconfig.Config, project string) []map[string]interface{} {
	servers := []map[string]interface{}{}
	for _, name := range cfg.ServerNames() {
		srv := cfg.Servers[name]
		if srv.Project != project {
			continue
		}
		servers = append(servers, map[string]interface{}{"name": name, "server": publicServer(srv)})
	}
	return servers
}

// publicPlanDisplay is plan.Display() with the endpoint routed through
// publicEndpoint so attachment values never reach MCP output. Display() builds a
// fresh map each call, so replacing its endpoint entry mutates no shared state.
func publicPlanDisplay(plan app.InvocationPlan) map[string]interface{} {
	display := plan.Display()
	display["endpoint"] = publicEndpoint(plan.Endpoint)
	return display
}
