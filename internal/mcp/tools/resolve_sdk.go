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
	srv.AddTool(&mcpsdk.Tool{
		Name:         "sofarpc_resolve",
		Title:        "SofaRPC Resolve",
		Description:  "Resolve the configured project, server, and invocation endpoint without touching the network.",
		Annotations:  &mcpsdk.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true, DestructiveHint: boolPtr(false), OpenWorldHint: boolPtr(false)},
		InputSchema:  resolveInputSchema,
		OutputSchema: resolveOutputSchema,
	}, adaptTool(stderr, func(ctx context.Context, _ *mcpsdk.CallToolRequest, a ResolveArgs) (app.Result, string) {
		resolved, err := appSvc.Resolve(ctx, app.ResolveInput{
			Project:   a.Project,
			Profile:   a.Profile,
			Server:    a.Server,
			TimeoutMS: a.TimeoutMS,
		})
		if err != nil {
			return failureResult(err, app.CodeBadRequest), ""
		}
		// network is a constant "not_probed" and diagnostics.resolution repeats the
		// top-level fields verbatim; both are omitted from the tool payload to keep
		// the per-call token cost down.
		if resolved.Endpoint != nil {
			return okResult(map[string]interface{}{
				"project":     resolved.Project.Name,
				"profile":     resolved.Profile,
				"projectInfo": publicProject(resolved.Project.Info),
				"server":      resolved.Server,
				"endpoint":    publicEndpoint(*resolved.Endpoint),
			}), "Endpoint resolved."
		}
		return okResult(map[string]interface{}{
			"project":     resolved.Project.Name,
			"projectInfo": publicProject(resolved.Project.Info),
			"servers":     publicServers(resolved.Servers),
		}), "Project resolved; no single endpoint was selected."
	}))
}
