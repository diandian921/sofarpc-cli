package tools

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/diandian921/sofarpc-mcp/internal/app"
	"github.com/diandian921/sofarpc-mcp/internal/appconfig"
)

const (
	sentinelKey   = "_sofa_token"
	sentinelValue = "SENTINEL_ATTACHMENT_VALUE_8f3a"
)

func structuredJSON(t *testing.T, structured interface{}) string {
	t.Helper()
	b, err := json.Marshal(structured)
	if err != nil {
		t.Fatalf("marshal structured content: %v", err)
	}
	return string(b)
}

// assertRedacted checks the positive contract for a single exit: the attachment
// key survives, the [redacted] marker is present, and the secret value is gone.
func assertRedacted(t *testing.T, label, js string) {
	t.Helper()
	if strings.Contains(js, sentinelValue) {
		t.Fatalf("%s leaks attachment value: %s", label, js)
	}
	if !strings.Contains(js, sentinelKey) {
		t.Fatalf("%s dropped attachment key %q: %s", label, sentinelKey, js)
	}
	if !strings.Contains(js, redactedValue) {
		t.Fatalf("%s missing %q marker: %s", label, redactedValue, js)
	}
}

// TestInvokePlanDisplayRedactsAttachments exercises the plan exit directly:
// publicPlanDisplay is the only redaction point and must leave the source plan
// untouched.
func TestInvokePlanDisplayRedactsAttachments(t *testing.T) {
	att := map[string]string{sentinelKey: sentinelValue}
	plan := app.InvocationPlan{
		Server:   "user-test",
		Endpoint: app.Endpoint{Server: "user-test", Address: "127.0.0.1:12200", Attachments: att},
		Method:   app.MethodSignature{Name: "m"},
	}
	disp := publicPlanDisplay(plan)
	assertRedacted(t, "invoke_plan(plan)", structuredJSON(t, disp))
	if plan.Endpoint.Attachments[sentinelKey] != sentinelValue {
		t.Fatalf("publicPlanDisplay mutated the source plan attachments: %v", plan.Endpoint.Attachments)
	}
}

func TestPublicViewsRedactValuesKeepKeys(t *testing.T) {
	if got := redactAttachments(nil); got != nil {
		t.Fatalf("redactAttachments(nil) = %v, want nil", got)
	}
	in := map[string]string{sentinelKey: sentinelValue}
	got := redactAttachments(in)
	if got[sentinelKey] != redactedValue {
		t.Fatalf("redactAttachments value = %q, want %q", got[sentinelKey], redactedValue)
	}
	if in[sentinelKey] != sentinelValue {
		t.Fatalf("redactAttachments mutated input: %v", in)
	}
	srv := publicServer(appconfig.Server{Address: "a", Attachments: map[string]string{sentinelKey: sentinelValue}})
	assertRedacted(t, "publicServer", structuredJSON(t, srv))
	ep := publicEndpoint(app.Endpoint{Address: "a", Attachments: map[string]string{sentinelKey: sentinelValue}})
	assertRedacted(t, "publicEndpoint", structuredJSON(t, ep))
}
