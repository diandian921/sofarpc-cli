package tools

import (
	"github.com/diandian921/sofarpc-mcp/internal/app"
	"github.com/diandian921/sofarpc-mcp/internal/appconfig"
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

// publicServers redacts the "server" entry of an app-built bound-server list
// (each element is {"name": string, "server": appconfig.Server}). Redacting the
// list app already resolved keeps the output on the same ConfigStore appSvc used,
// rather than re-reading the global config (which can diverge under an injected
// store). A "server" value that is not an appconfig.Server is dropped, never
// passed through raw, so an unexpected shape cannot leak attachment values.
func publicServers(servers []map[string]interface{}) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(servers))
	for _, entry := range servers {
		redacted := make(map[string]interface{}, len(entry))
		for k, v := range entry {
			if k == "server" {
				if srv, ok := v.(appconfig.Server); ok {
					redacted[k] = publicServer(srv)
				}
				continue
			}
			redacted[k] = v
		}
		out = append(out, redacted)
	}
	return out
}

// publicPlanDisplay is plan.Display() with the endpoint routed through
// publicEndpoint so attachment values never reach MCP output. Display() builds a
// fresh map each call, so replacing its endpoint entry mutates no shared state.
func publicPlanDisplay(plan app.InvocationPlan) map[string]interface{} {
	display := plan.Display()
	display["endpoint"] = publicEndpoint(plan.Endpoint)
	return display
}
