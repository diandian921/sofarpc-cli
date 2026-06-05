package tools

import (
	"context"
	"encoding/json"
	"io"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/diandian921/sofarpc-mcp/internal/app"
)

// AddInvoke registers sofarpc_invoke on the SDK server. SDK-native replacement for
// InvokeTool.
//
// It uses the raw Server.AddTool (not the generic mcp.AddTool) on purpose: the
// generic path validates input via applySchema, which unmarshals arguments into a
// map[string]any with plain json.Unmarshal and re-marshals them — rounding any Java
// long beyond 2^53 through float64 before the handler ever runs. The raw path hands
// us the untouched wire bytes, which we decode with UseNumber to keep full precision.
// As a bonus, this restores the legacy "schema advertised, validated in the handler"
// behavior: missing service/method and the types/args aliases are handled by the
// handler with friendly recovery hints rather than rejected as protocol errors.
func AddInvoke(srv *mcpsdk.Server, appSvc *app.Service, stderr io.Writer) {
	srv.AddTool(&mcpsdk.Tool{
		Name:         "sofarpc_invoke",
		Title:        "SofaRPC Invoke",
		Description:  "Invoke a SofaRPC method over direct BOLT/Hessian2. Use sofarpc_invoke_plan first to validate arguments without sending a request.",
		Annotations:  &mcpsdk.ToolAnnotations{DestructiveHint: boolPtr(true), OpenWorldHint: boolPtr(true)},
		InputSchema:  invokeInputSchema,
		OutputSchema: resultOutputSchema,
	}, adaptRawTool(stderr, func(ctx context.Context, req *mcpsdk.CallToolRequest) (app.Result, string) {
		a, derr := decodeInvokeArgs(req.Params.Arguments)
		if derr != nil {
			return *derr, ""
		}
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
// InvokePlanTool. Uses the raw Server.AddTool for the same precision reason as
// AddInvoke (its argument types feed the plan's type checking).
func AddInvokePlan(srv *mcpsdk.Server, appSvc *app.Service, stderr io.Writer) {
	srv.AddTool(&mcpsdk.Tool{
		Name:         "sofarpc_invoke_plan",
		Title:        "SofaRPC Invoke Plan",
		Description:  "Resolve and validate a SofaRPC invocation (endpoint, argument types) without sending a request.",
		Annotations:  &mcpsdk.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true},
		InputSchema:  invokePlanInputSchema,
		OutputSchema: resultOutputSchema,
	}, adaptRawTool(stderr, func(ctx context.Context, req *mcpsdk.CallToolRequest) (app.Result, string) {
		a, derr := decodeInvokeArgs(req.Params.Arguments)
		if derr != nil {
			return *derr, ""
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

// decodeInvokeArgs decodes raw arguments into InvokeArgs with number precision
// preserved. A non-nil returned result is a ready failure envelope (never a Go
// error), so the adapter keeps the unified shape.
func decodeInvokeArgs(raw json.RawMessage) (InvokeArgs, *app.Result) {
	var a InvokeArgs
	if err := decodeStrict(raw, &a); err != nil {
		r := app.RenderFailure(app.CodeBadRequest, err.Error(), nil)
		return a, &r
	}
	return a, nil
}
