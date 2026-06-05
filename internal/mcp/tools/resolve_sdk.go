package tools

import (
	"context"
	"io"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/diandian921/sofarpc-mcp/internal/app"
)

// AddResolve registers sofarpc_resolve on the SDK server (read-only, no network).
// SDK-native replacement for ResolveTool; the handler body mirrors ResolveTool.Run.
func AddResolve(srv *mcpsdk.Server, appSvc *app.Service, stderr io.Writer) {
	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:         "sofarpc_resolve",
		Title:        "SofaRPC Resolve",
		Description:  "Resolve the configured project, server, and invocation endpoint without touching the network.",
		Annotations:  &mcpsdk.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true},
		InputSchema:  resolveInputSchema,
		OutputSchema: resultOutputSchema,
	}, adaptTool(stderr, func(ctx context.Context, _ *mcpsdk.CallToolRequest, a ResolveArgs) (app.Result, string) {
		resolved, err := appSvc.Resolve(ctx, app.ResolveInput{
			Project:   a.Project,
			Server:    a.Server,
			TimeoutMS: a.TimeoutMS,
		})
		if err != nil {
			return app.RenderFailure(app.CodeBadRequest, err.Error(), app.DomainErrorDetails(err)), ""
		}
		if resolved.Endpoint != nil {
			return okResult(map[string]interface{}{
				"project":     resolved.Project.Name,
				"projectInfo": resolved.Project.Info,
				"server":      resolved.Server,
				"endpoint":    publicEndpoint(*resolved.Endpoint),
				"network":     resolved.Network,
				"diagnostics": resolved.Diagnostics,
			}), "Endpoint resolved."
		}
		return okResult(map[string]interface{}{
			"project":     resolved.Project.Name,
			"projectInfo": resolved.Project.Info,
			"servers":     publicServers(resolved.Servers),
			"network":     resolved.Network,
			"diagnostics": resolved.Diagnostics,
		}), "Project resolved; no single endpoint was selected."
	}))
}
