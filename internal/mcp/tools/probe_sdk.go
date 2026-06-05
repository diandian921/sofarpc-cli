package tools

import (
	"context"
	"io"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/diandian921/sofarpc-mcp/internal/app"
)

// AddProbe registers sofarpc_probe on an official-SDK server. It is the SDK-native
// replacement for ProbeTool; both reuse the same args, schema, and display text in
// this package during the migration. The handler body mirrors ProbeTool.Run.
func AddProbe(srv *mcpsdk.Server, appSvc *app.Service, stderr io.Writer) {
	srv.AddTool(&mcpsdk.Tool{
		Name:         "sofarpc_probe",
		Title:        probeTitle,
		Description:  probeDescription,
		Annotations:  &mcpsdk.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true, DestructiveHint: boolPtr(false), OpenWorldHint: boolPtr(true)},
		InputSchema:  probeInputSchema,
		OutputSchema: resultOutputSchema,
	}, adaptTool(stderr, func(ctx context.Context, _ *mcpsdk.CallToolRequest, a ProbeArgs) (app.Result, string) {
		probe := appSvc.ProbeEndpoint(ctx, app.ProbeInput{
			Project:   a.Project,
			Server:    a.Server,
			Address:   a.Address,
			Service:   a.Service,
			TimeoutMS: a.TimeoutMS,
		})
		result := app.RenderProbe(probe)
		result.RequestID = app.NewRequestID("ping")
		return result, probeSummary
	}))
}
