package tools

import (
	"context"
	"fmt"
	"strings"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

const invokeWorkflowPromptName = "sofarpc.invoke_workflow"

// AddInvokeWorkflowPrompt registers the sofarpc.invoke_workflow prompt: a user-selected
// workflow template (offered by the host as a slash command / template, never auto-run)
// that walks an agent through the recommended resolve -> describe -> invoke_plan ->
// invoke path, including the machine-readable failure-recovery contract. Registering it
// also turns on the server's prompts capability.
func AddInvokeWorkflowPrompt(srv *mcpsdk.Server) {
	srv.AddPrompt(&mcpsdk.Prompt{
		Name:        invokeWorkflowPromptName,
		Title:       "SofaRPC Invoke Workflow",
		Description: "Guided workflow for one SOFARPC generic invocation: resolve, describe, plan, invoke, recover.",
		Arguments: []*mcpsdk.PromptArgument{
			{Name: "intent", Description: "What the user wants to call or verify.", Required: true},
			{Name: "server", Description: "Optional configured server name."},
			{Name: "project", Description: "Optional configured project name."},
			{Name: "serviceQuery", Description: "Optional natural-language hint to search for the service."},
			{Name: "service", Description: "Optional service interface FQN, if already known."},
			{Name: "method", Description: "Optional method name, if already known."},
		},
	}, func(_ context.Context, req *mcpsdk.GetPromptRequest) (*mcpsdk.GetPromptResult, error) {
		args := map[string]string{}
		if req.Params != nil {
			args = req.Params.Arguments
		}
		return &mcpsdk.GetPromptResult{
			Description: "Recommended SOFARPC invocation workflow.",
			Messages: []*mcpsdk.PromptMessage{
				{Role: "user", Content: &mcpsdk.TextContent{Text: invokeWorkflowText(args)}},
			},
		}, nil
	})
}

// invokeWorkflowText renders the workflow message, folding any supplied known context
// (server/project/service/method) and the search hint into the recommended steps.
func invokeWorkflowText(args map[string]string) string {
	var b strings.Builder
	b.WriteString("You are helping the user make one SOFARPC generic invocation.\n\n")

	intent := args["intent"]
	if intent == "" {
		intent = "(not provided — ask the user what they want to call or verify)"
	}
	fmt.Fprintf(&b, "User intent: %s\n", intent)
	for _, kv := range []struct{ key, label string }{
		{"server", "Target server"},
		{"project", "Project"},
		{"service", "Service FQN"},
		{"method", "Method"},
	} {
		if v := args[kv.key]; v != "" {
			fmt.Fprintf(&b, "%s: %s\n", kv.label, v)
		}
	}

	serviceQuery := args["serviceQuery"]
	if serviceQuery == "" {
		serviceQuery = "the intent above"
	}

	b.WriteString("\nSteps:\n")
	b.WriteString("1. Call sofarpc_resolve to confirm project/server/endpoint. With multiple servers, pass an explicit `server` (error.details.candidates lists them).\n")
	fmt.Fprintf(&b, "2. If the service FQN is unknown, call sofarpc_describe with query=%q to find candidates, then copy a candidate's service/method/paramTypes/parameterNames.\n", serviceQuery)
	b.WriteString("3. If the method, paramTypes, or DTO fields are unclear, call sofarpc_describe with service=<FQN> (and method=<method>) for the exact signature.\n")
	b.WriteString("4. Call sofarpc_invoke_plan to validate service/method/paramTypes/arguments without sending a request.\n")
	b.WriteString("5. Only after the plan succeeds and the user wants a real call, call sofarpc_invoke.\n")
	b.WriteString("6. On any failure, read structuredContent.error.nextTool and error.recovery, then follow that tool.\n")
	b.WriteString("7. A successful sofarpc_probe only proves TCP reachability, not that the service or method exists.\n")
	return b.String()
}
