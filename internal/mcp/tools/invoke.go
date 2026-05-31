package tools

import (
	"context"
	"encoding/json"

	"github.com/diandian921/sofarpc-cli/internal/app"
	"github.com/diandian921/sofarpc-cli/internal/mcp/server"
)

// InvokeArgs are the arguments shared by sofarpc_invoke and sofarpc_invoke_plan.
// paramTypes/orderedArguments are the advertised names; types/args are accepted
// server-side aliases that the input schema does not advertise.
type InvokeArgs struct {
	Server           string                 `json:"server,omitempty"`
	Project          string                 `json:"project,omitempty"`
	Service          string                 `json:"service"`
	Method           string                 `json:"method"`
	ParamTypes       []string               `json:"paramTypes,omitempty"`
	Types            []string               `json:"types,omitempty"`
	OrderedArguments []interface{}          `json:"orderedArguments,omitempty"`
	Args             []interface{}          `json:"args,omitempty"`
	Arguments        map[string]interface{} `json:"arguments,omitempty"`
	TimeoutMS        int                    `json:"timeoutMs,omitempty"`
	RawResult        bool                   `json:"rawResult,omitempty"`
}

func (a InvokeArgs) toInput() app.InvocationInput {
	paramTypes := a.ParamTypes
	if len(paramTypes) == 0 {
		paramTypes = a.Types
	}
	input := app.InvocationInput{
		Project:    a.Project,
		Server:     a.Server,
		Service:    a.Service,
		Method:     a.Method,
		ParamTypes: paramTypes,
		TimeoutMS:  a.TimeoutMS,
		RawResult:  a.RawResult,
	}
	ordered := a.OrderedArguments
	if ordered == nil {
		ordered = a.Args
	}
	if ordered != nil {
		input.OrderedArguments = ordered
		input.HasOrderedArguments = true
		return input
	}
	if a.Arguments != nil {
		input.NamedArguments = a.Arguments
	}
	return input
}

var invokeInputSchema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["service", "method"],
  "properties": {
    "server": {"type": "string", "description": "Configured server name. Optional only when exactly one matching server can be inferred."},
    "project": {"type": "string", "description": "Optional project name used to infer a single bound server."},
    "service": {"type": "string", "description": "Service interface FQN."},
    "method": {"type": "string", "description": "Method name."},
    "paramTypes": {"type": "array", "items": {"type": "string"}, "description": "Optional Java parameter type FQNs for overload disambiguation."},
    "orderedArguments": {"type": "array", "description": "Arguments in method parameter order."},
    "arguments": {"type": "object", "additionalProperties": true, "description": "Named arguments keyed by Java parameter name, or a single DTO object when the method has one parameter."},
    "timeoutMs": {"type": "integer", "description": "Optional total timeout in milliseconds."},
    "rawResult": {"type": "boolean", "description": "When true, include the decoded Java object shape alongside the flattened result."}
  }
}`)

var invokePlanInputSchema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["service", "method"],
  "properties": {
    "server": {"type": "string", "description": "Configured server name. Optional only when exactly one matching server can be inferred."},
    "project": {"type": "string", "description": "Optional project name used to infer a single bound server."},
    "service": {"type": "string", "description": "Service interface FQN."},
    "method": {"type": "string", "description": "Method name."},
    "paramTypes": {"type": "array", "items": {"type": "string"}, "description": "Optional Java parameter type FQNs for overload disambiguation."},
    "orderedArguments": {"type": "array", "description": "Arguments in method parameter order."},
    "arguments": {"type": "object", "additionalProperties": true, "description": "Named arguments keyed by Java parameter name, or a single DTO object when the method has one parameter."},
    "timeoutMs": {"type": "integer", "description": "Optional total timeout in milliseconds."}
  }
}`)

// InvokeTool performs a real SofaRPC invocation against a configured server,
// resolved by server/project name — there is no address argument, so it cannot
// target an arbitrary host. Marked destructive and open-world because the remote
// side effect is outside our control.
func InvokeTool(appSvc *app.Service) server.Tool[InvokeArgs] {
	return server.Tool[InvokeArgs]{
		Spec: server.ToolSpec{
			Name:         "sofarpc_invoke",
			Title:        "SofaRPC Invoke",
			Description:  "Invoke a SofaRPC method over direct BOLT/Hessian2. Use sofarpc_invoke_plan first to validate arguments without sending a request.",
			Annotations:  server.Annotations{DestructiveHint: true, OpenWorldHint: true},
			InputSchema:  invokeInputSchema,
			OutputSchema: resultOutputSchema,
			Async:        true,
		},
		Run: func(ctx context.Context, rt server.Runtime, a InvokeArgs) server.Result {
			rt.Progress(ctx, "resolving plan", 0)
			plan, err := appSvc.PlanInvocation(ctx, a.toInput())
			if err != nil {
				return failure(app.CodeBadRequest, err.Error(), app.DomainErrorDetails(err))
			}
			rt.Progress(ctx, "invoking remote method", 0.5)
			result := app.RenderExecution(appSvc.ExecuteInvocation(ctx, plan))
			result.RequestID = app.NewRequestID("invoke")
			return rendered(result, "Invoke completed.")
		},
	}
}

// InvokePlanTool resolves and validates an invocation without sending a request
// (the former dryRun mode). Read-only and idempotent, so a host can auto-approve it.
func InvokePlanTool(appSvc *app.Service) server.Tool[InvokeArgs] {
	return server.Tool[InvokeArgs]{
		Spec: server.ToolSpec{
			Name:         "sofarpc_invoke_plan",
			Title:        "SofaRPC Invoke Plan",
			Description:  "Resolve and validate a SofaRPC invocation (endpoint, argument types) without sending a request.",
			Annotations:  server.Annotations{ReadOnlyHint: true, IdempotentHint: true},
			InputSchema:  invokePlanInputSchema,
			OutputSchema: resultOutputSchema,
			Async:        true,
		},
		Run: func(ctx context.Context, _ server.Runtime, a InvokeArgs) server.Result {
			plan, err := appSvc.PlanInvocation(ctx, a.toInput())
			if err != nil {
				return failure(app.CodeBadRequest, err.Error(), app.DomainErrorDetails(err))
			}
			planData := publicPlanDisplay(plan)
			planData["requestId"] = app.NewRequestID("invoke")
			return success("Invoke plan resolved.", map[string]interface{}{"dryRun": true, "plan": planData})
		},
	}
}
