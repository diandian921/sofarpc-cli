package tools

import (
	"context"
	"io"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/diandian921/sofarpc-mcp/internal/app"
)

// AddInvoke registers sofarpc_invoke. SDK-native replacement for InvokeTool. The
// shared adaptTool decodes arguments with UseNumber + DisallowUnknownFields, so Java
// long values keep full precision and a missing service/method (or any unknown
// argument) is handled by the handler with friendly recovery hints — mirroring the
// legacy framework rather than the SDK's generic schema validation.
func AddInvoke(srv *mcpsdk.Server, appSvc *app.Service, stderr io.Writer) {
	srv.AddTool(&mcpsdk.Tool{
		Name:         "sofarpc_invoke",
		Title:        "SofaRPC Invoke",
		Description:  "Invoke a SofaRPC method over direct BOLT/Hessian2. Prefer named arguments (parameter names from sofarpc_describe); use paramTypes + orderedArguments only when the source schema is unavailable or the method is overloaded — never both forms. For complex payloads run sofarpc_invoke_plan first with the same arguments. To keep large results small, pass resultPath ($.path subtree) and assertions instead of reading the whole result; arrays longer than 200 items are truncated with a $truncated marker. Note: ok=true means the RPC completed — business success lives inside data.result (e.g. success/errorMsg fields).",
		Annotations:  &mcpsdk.ToolAnnotations{DestructiveHint: boolPtr(true), OpenWorldHint: boolPtr(true)},
		InputSchema:  invokeInputSchema,
		OutputSchema: invokeOutputSchema,
	}, adaptTool(stderr, func(ctx context.Context, req *mcpsdk.CallToolRequest, a InvokeArgs) (app.Result, string) {
		if bad, ok := a.validate(); !ok {
			return bad, ""
		}
		notifyProgress(ctx, req, "resolving plan", 0)
		plan, err := appSvc.PlanInvocation(ctx, a.toInput())
		if err != nil {
			return app.RenderFailure(app.CodeBadRequest, err.Error(), app.DomainErrorDetails(err)), ""
		}
		notifyProgress(ctx, req, "plan resolved", 0.25)
		notifyProgress(ctx, req, "invoking remote method", 0.5)
		result := app.RenderExecution(appSvc.ExecuteInvocation(ctx, plan))
		result.RequestID = app.NewRequestID("invoke")
		notifyProgress(ctx, req, "response decoded", 0.8)
		notifyProgress(ctx, req, "done", 1.0)
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
		Description:  "Resolve and validate a SofaRPC invocation (endpoint, method signature, argument encoding) without sending any request — it can never reach the remote service. Takes the same arguments as sofarpc_invoke; run it first for complex or first-time payloads, then call sofarpc_invoke with the identical arguments.",
		Annotations:  &mcpsdk.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true, DestructiveHint: boolPtr(false), OpenWorldHint: boolPtr(false)},
		InputSchema:  invokePlanInputSchema,
		OutputSchema: invokePlanOutputSchema,
	}, adaptTool(stderr, func(ctx context.Context, _ *mcpsdk.CallToolRequest, a InvokeArgs) (app.Result, string) {
		if bad, ok := a.validate(); !ok {
			return bad, ""
		}
		plan, err := appSvc.PlanInvocation(ctx, a.toInput())
		if err != nil {
			return app.RenderFailure(app.CodeBadRequest, err.Error(), app.DomainErrorDetails(err)), ""
		}
		planData := publicPlanDisplay(plan)
		planData["requestId"] = app.NewRequestID("invoke")
		return okResult(map[string]interface{}{"dryRun": true, "plan": planData}), "Invoke plan resolved."
	}))
}
