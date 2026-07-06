package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestInvokeWorkflowPromptRoundtrip pins the prompt registration from within the
// tools package: prompts/list advertises the template with a required intent, and
// prompts/get folds the supplied context into a user-role workflow message.
func TestInvokeWorkflowPromptRoundtrip(t *testing.T) {
	cs := newToolClient(t, func(srv *mcpsdk.Server) {
		AddInvokeWorkflowPrompt(srv)
	})
	ctx := context.Background()

	listed, err := cs.ListPrompts(ctx, nil)
	if err != nil {
		t.Fatalf("list prompts: %v", err)
	}
	if len(listed.Prompts) != 1 || listed.Prompts[0].Name != invokeWorkflowPromptName {
		t.Fatalf("expected only %s, got %+v", invokeWorkflowPromptName, listed.Prompts)
	}

	res, err := cs.GetPrompt(ctx, &mcpsdk.GetPromptParams{
		Name: invokeWorkflowPromptName,
		Arguments: map[string]string{
			"intent": "check user u001", "server": "user-test", "project": "user",
			"service": "com.example.user.UserFacade", "method": "getUser",
			"serviceQuery": "find user",
		},
	})
	if err != nil {
		t.Fatalf("get prompt: %v", err)
	}
	if len(res.Messages) != 1 || res.Messages[0].Role != "user" {
		t.Fatalf("expected one user-role message, got %+v", res.Messages)
	}
	text := res.Messages[0].Content.(*mcpsdk.TextContent).Text
	for _, want := range []string{
		"check user u001", "Target server: user-test", "Project: user",
		"Service FQN: com.example.user.UserFacade", "Method: getUser",
		`query="find user"`, "sofarpc_invoke_plan", "nextTool",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("workflow message missing %q; got:\n%s", want, text)
		}
	}
}

// TestInvokeWorkflowTextDefaults pins the template fallbacks: without an intent
// the message tells the agent to ask, and without a serviceQuery the describe
// step falls back to "the intent above".
func TestInvokeWorkflowTextDefaults(t *testing.T) {
	text := invokeWorkflowText(map[string]string{})
	if !strings.Contains(text, "(not provided") {
		t.Errorf("missing intent should be called out: %s", text)
	}
	if !strings.Contains(text, `query="the intent above"`) {
		t.Errorf("missing serviceQuery should fall back to the intent: %s", text)
	}
	for _, label := range []string{"Target server:", "Project:", "Service FQN:", "Method:"} {
		if strings.Contains(text, label) {
			t.Errorf("unset context %q must be omitted: %s", label, text)
		}
	}
}

// TestCompatibilityResourceRoundtrip pins the resource registration from within
// the tools package: the fixed URI serves valid JSON with the documented feature
// entries and no config-derived content.
func TestCompatibilityResourceRoundtrip(t *testing.T) {
	cs := newToolClient(t, func(srv *mcpsdk.Server) {
		AddCompatibilityResource(srv)
	})
	res, err := cs.ReadResource(context.Background(), &mcpsdk.ReadResourceParams{URI: compatibilityResourceURI})
	if err != nil {
		t.Fatalf("read resource: %v", err)
	}
	if len(res.Contents) != 1 {
		t.Fatalf("expected one content block, got %d", len(res.Contents))
	}
	c := res.Contents[0]
	if c.URI != compatibilityResourceURI || c.MIMEType != "application/json" {
		t.Errorf("content uri/mime = %q/%q", c.URI, c.MIMEType)
	}
	if !json.Valid([]byte(c.Text)) {
		t.Fatalf("compatibility content is not valid JSON: %s", c.Text)
	}
	if !strings.Contains(c.Text, "BigDecimal") {
		t.Errorf("compatibility summary missing documented entries: %s", c.Text)
	}
}
