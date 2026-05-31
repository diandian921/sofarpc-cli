package tools

import (
	"context"
	"encoding/json"

	"github.com/diandian921/sofarpc-cli/internal/app"
	"github.com/diandian921/sofarpc-cli/internal/mcp/server"
)

// ResolveArgs are the arguments for sofarpc_resolve.
type ResolveArgs struct {
	Project   string `json:"project,omitempty"`
	Server    string `json:"server,omitempty"`
	TimeoutMS int    `json:"timeoutMs,omitempty"`
}

var resolveInputSchema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "project": {"type": "string", "description": "Optional configured project name."},
    "server": {"type": "string", "description": "Optional configured server name."},
    "timeoutMs": {"type": "integer", "description": "Optional timeout override to show on the resolved endpoint."}
  }
}`)

// ResolveTool resolves project/server/endpoint without touching the network.
func ResolveTool(appSvc *app.Service) server.Tool[ResolveArgs] {
	return server.Tool[ResolveArgs]{
		Spec: server.ToolSpec{
			Name:         "sofarpc_resolve",
			Title:        "SofaRPC Resolve",
			Description:  "Resolve the configured project, server, and invocation endpoint without touching the network.",
			Annotations:  server.Annotations{ReadOnlyHint: true, IdempotentHint: true},
			InputSchema:  resolveInputSchema,
			OutputSchema: resultOutputSchema,
		},
		Run: func(ctx context.Context, _ server.Runtime, a ResolveArgs) server.Result {
			resolved, err := appSvc.Resolve(ctx, app.ResolveInput{
				Project:   a.Project,
				Server:    a.Server,
				TimeoutMS: a.TimeoutMS,
			})
			if err != nil {
				return failure(app.CodeBadRequest, err.Error(), app.DomainErrorDetails(err))
			}
			if resolved.Endpoint != nil {
				return success("Endpoint resolved.", map[string]interface{}{
					"project":     resolved.Project.Name,
					"projectInfo": resolved.Project.Info,
					"server":      resolved.Server,
					"endpoint":    resolved.Endpoint,
					"network":     resolved.Network,
					"diagnostics": resolved.Diagnostics,
				})
			}
			return success("Project resolved; no single endpoint was selected.", map[string]interface{}{
				"project":     resolved.Project.Name,
				"projectInfo": resolved.Project.Info,
				"servers":     resolved.Servers,
				"network":     resolved.Network,
				"diagnostics": resolved.Diagnostics,
			})
		},
	}
}
