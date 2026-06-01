package tools

import (
	"context"
	"testing"

	"github.com/diandian921/sofarpc-mcp/internal/app"
)

// TestInvokePlanningFailureCarriesNextTool guards the regression where invoke
// planning errors lost their stable code and nextTool — exactly the path agents
// most need to recover from.
func TestInvokePlanningFailureCarriesNextTool(t *testing.T) {
	seedConfig(t)
	out := InvokeTool(app.New(nil)).Run(context.Background(), nopRuntime{}, InvokeArgs{
		Service: "x.Y",
		Method:  "m",
		Server:  "no-such-server",
	})
	if !out.IsError {
		t.Fatal("planning failure must set IsError")
	}
	r := asResult(t, out.Structured)
	if r.OK || r.Code != app.CodeBadRequest {
		t.Fatalf("rendered code = %q, want BAD_REQUEST: %+v", r.Code, r)
	}
	if r.Error == nil || r.Error.NextTool == "" {
		t.Fatalf("planning failure must carry error.nextTool: %+v", r.Error)
	}
}

func TestInvokePlanToolReturnsPlanWithoutNetwork(t *testing.T) {
	seedConfig(t)
	out := InvokePlanTool(app.New(nil)).Run(context.Background(), nopRuntime{}, InvokeArgs{
		Service:    "com.example.UserService",
		Method:     "getUser",
		Server:     "user-test",
		ParamTypes: []string{"java.lang.String"},
		Args:       []interface{}{"u001"},
	})
	r := asResult(t, out.Structured)
	if !r.OK {
		t.Fatalf("invoke_plan must succeed without network: %+v", r)
	}
	if string(r.Data) == "" {
		t.Fatal("invoke_plan must include the resolved plan in data")
	}
}
