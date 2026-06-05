package tools

import (
	"context"
	"io"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/diandian921/sofarpc-mcp/internal/app"
)

// AddInvoke registers sofarpc_invoke. SDK-native replacement for InvokeTool. The
// shared adaptTool decodes arguments with UseNumber + DisallowUnknownFields, so Java
// long values keep full precision and missing service/method plus the types/args
// aliases are handled by the handler with friendly recovery hints — mirroring the
// legacy framework rather than the SDK's generic schema validation.
func AddInvoke(srv *mcpsdk.Server, appSvc *app.Service, stderr io.Writer) {
	srv.AddTool(&mcpsdk.Tool{
		Name:         "sofarpc_invoke",
		Title:        "SofaRPC Invoke",
		Description:  "Invoke a SofaRPC method over direct BOLT/Hessian2. Use sofarpc_invoke_plan first to validate arguments without sending a request.",
		Annotations:  &mcpsdk.ToolAnnotations{DestructiveHint: boolPtr(true), OpenWorldHint: boolPtr(true)},
		InputSchema:  invokeInputSchema,
		OutputSchema: resultOutputSchema,
	}, adaptTool(stderr, func(ctx context.Context, req *mcpsdk.CallToolRequest, a InvokeArgs) (app.Result, string) {
		notifyProgress(ctx, req, "resolving plan", 0)
		plan, err := appSvc.PlanInvocation(ctx, a.toInput())
		if err != nil {
			return app.RenderFailure(app.CodeBadRequest, err.Error(), app.DomainErrorDetails(err)), ""
		}
		notifyProgress(ctx, req, "invoking remote method", 0.5)
		result := app.RenderExecution(appSvc.ExecuteInvocation(ctx, plan))
		result.RequestID = app.NewRequestID("invoke")
		return result, "Invoke completed."
	}))
}

// AddInvokePlan registers sofarpc_invoke_plan: resolve and validate an invocation
// without sending a request. Read-only and idempotent. SDK-native replacement for
// InvokePlanTool.
func AddInvokePlan(srv *mcpsdk.Server, appSvc *app.Service, stderr io.Writer) {
	srv.AddTool(&mcpsdk.Tool{
		Name:         "sofarpc_invoke_plan",
		Title:        "SofaRPC Invoke Plan",
		Description:  "Resolve and validate a SofaRPC invocation (endpoint, argument types) without sending a request.",
		Annotations:  &mcpsdk.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true, DestructiveHint: boolPtr(false), OpenWorldHint: boolPtr(false)},
		InputSchema:  invokePlanInputSchema,
		OutputSchema: resultOutputSchema,
	}, adaptTool(stderr, func(ctx context.Context, _ *mcpsdk.CallToolRequest, a InvokeArgs) (app.Result, string) {
		plan, err := appSvc.PlanInvocation(ctx, a.toInput())
		if err != nil {
			return app.RenderFailure(app.CodeBadRequest, err.Error(), app.DomainErrorDetails(err)), ""
		}
		planData := publicPlanDisplay(plan)
		planData["requestId"] = app.NewRequestID("invoke")
		return okResult(map[string]interface{}{"dryRun": true, "plan": planData}), "Invoke plan resolved."
	}))
}
