package tools

import (
	"context"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/diandian921/sofarpc-mcp/internal/app"
	"github.com/diandian921/sofarpc-mcp/internal/appconfig"
)

// seedUnreachableServer configures one project and one server whose address
// nothing listens on, so plans resolve locally but executions fail at dial time.
func seedUnreachableServer(t *testing.T) {
	t.Helper()
	seedConfig(t, func(cfg *appconfig.Config) error {
		if _, err := cfg.AddProject("user", t.TempDir(), nil, false); err != nil {
			return err
		}
		_, err := cfg.AddServer("user-test", appconfig.Server{
			Address: "127.0.0.1:1", Project: "user", TimeoutMS: 500,
		}, false)
		return err
	})
}

func registerInvokeTools(srv *mcpsdk.Server) {
	appSvc := app.New(nil)
	AddInvoke(srv, appSvc, io.Discard)
	AddInvokePlan(srv, appSvc, io.Discard)
}

// TestInvokePlanSuccessShape pins the plan-only contract: a resolvable invocation
// returns dryRun:true plus a plan whose endpoint attachments are redacted and
// whose requestId is invoke-prefixed, without touching the network.
func TestInvokePlanSuccessShape(t *testing.T) {
	seedConfig(t, func(cfg *appconfig.Config) error {
		if _, err := cfg.AddProject("user", t.TempDir(), nil, false); err != nil {
			return err
		}
		_, err := cfg.AddServer("user-test", appconfig.Server{
			Address: "127.0.0.1:1", Project: "user",
			Attachments: map[string]string{"token": "SECRET-VALUE"},
		}, false)
		return err
	})
	cs := newToolClient(t, registerInvokeTools)
	env := decodeEnvelope(t, callTool(t, cs, "sofarpc_invoke_plan", map[string]any{
		"server": "user-test", "service": "com.example.S", "method": "m",
		"paramTypes": []any{"java.lang.String"}, "orderedArguments": []any{"x"},
	}))
	if !env.OK {
		t.Fatalf("invoke_plan failed: %+v", env)
	}
	if env.Data["dryRun"] != true {
		t.Errorf("data.dryRun must be true: %+v", env.Data)
	}
	plan, _ := env.Data["plan"].(map[string]any)
	if plan == nil {
		t.Fatalf("data.plan missing: %+v", env.Data)
	}
	if rid, _ := plan["requestId"].(string); !strings.HasPrefix(rid, "invoke-") {
		t.Errorf("plan.requestId should be invoke-prefixed, got %q", rid)
	}
	endpoint, _ := plan["endpoint"].(map[string]any)
	if atts, _ := endpoint["attachments"].(map[string]any); atts["token"] != redactedValue {
		t.Errorf("plan endpoint attachments must be redacted: %v", endpoint)
	}
}

// TestInvokeToolsRejectBothArgumentForms pins the mutual-exclusion gate on both
// tools: passing arguments and orderedArguments together is BAD_REQUEST with the
// pinned recovery, before any resolution happens.
func TestInvokeToolsRejectBothArgumentForms(t *testing.T) {
	seedUnreachableServer(t)
	cs := newToolClient(t, registerInvokeTools)
	for _, tool := range []string{"sofarpc_invoke", "sofarpc_invoke_plan"} {
		res := callTool(t, cs, tool, map[string]any{
			"server": "user-test", "service": "com.example.S", "method": "m",
			"arguments":        map[string]any{"a": 1},
			"orderedArguments": []any{1},
			"paramTypes":       []any{"java.lang.Integer"},
		})
		env := decodeEnvelope(t, res)
		if !res.IsError || env.Code != app.CodeBadRequest {
			t.Errorf("%s: both argument forms must be BAD_REQUEST, got %+v", tool, env)
		}
		if env.Error == nil || !strings.Contains(env.Error.Message, "not both") {
			t.Errorf("%s: error should state the exclusivity, got %+v", tool, env.Error)
		}
	}
}

// TestInvokePlanUnknownServer pins the planning failure envelope: an unknown
// server is BAD_REQUEST carrying the domain-error kind in details.
func TestInvokePlanUnknownServer(t *testing.T) {
	seedConfig(t, nil)
	cs := newToolClient(t, registerInvokeTools)
	res := callTool(t, cs, "sofarpc_invoke_plan", map[string]any{
		"server": "ghost", "service": "com.example.S", "method": "m",
		"paramTypes": []any{"java.lang.String"}, "orderedArguments": []any{"x"},
	})
	env := decodeEnvelope(t, res)
	if !res.IsError || env.Code != app.CodeBadRequest {
		t.Fatalf("unknown server must be BAD_REQUEST, got %+v", env)
	}
	if env.Error == nil || env.Error.Details["kind"] != "SERVER_NOT_FOUND" {
		t.Errorf("error.details.kind should be SERVER_NOT_FOUND: %+v", env.Error)
	}
}

// TestInvokeConnectFailureAndProgress drives the full sofarpc_invoke handler
// against a dead endpoint: the execution fails with a recoverable envelope, the
// requestId is invoke-prefixed, and — with a progress token — the staged progress
// notifications (resolve 0.25 through done 1.0) are emitted even on failure.
func TestInvokeConnectFailureAndProgress(t *testing.T) {
	seedUnreachableServer(t)

	ctx := context.Background()
	serverT, clientT := mcpsdk.NewInMemoryTransports()
	srv := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "tools-test", Version: "0"}, nil)
	registerInvokeTools(srv)
	if _, err := srv.Connect(ctx, serverT, nil); err != nil {
		t.Fatalf("server connect: %v", err)
	}
	var mu sync.Mutex
	var progresses []float64
	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "t", Version: "0"}, &mcpsdk.ClientOptions{
		ProgressNotificationHandler: func(_ context.Context, req *mcpsdk.ProgressNotificationClientRequest) {
			mu.Lock()
			progresses = append(progresses, req.Params.Progress)
			mu.Unlock()
		},
	})
	cs, err := client.Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer cs.Close()

	res, err := cs.CallTool(ctx, &mcpsdk.CallToolParams{
		Name: "sofarpc_invoke",
		Arguments: map[string]any{
			"server": "user-test", "service": "com.example.S", "method": "m",
			"paramTypes": []any{"java.lang.String"}, "orderedArguments": []any{"x"},
			"timeoutMs": 300,
		},
		Meta: mcpsdk.Meta{"progressToken": "p1"},
	})
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	env := decodeEnvelope(t, res)
	if !res.IsError {
		t.Fatalf("invoke against a dead endpoint must be isError: %+v", env)
	}
	if !strings.HasPrefix(env.RequestID, "invoke-") {
		t.Errorf("requestId should be invoke-prefixed, got %q", env.RequestID)
	}
	if env.Error == nil || env.Error.NextTool == "" {
		t.Errorf("failure must carry a recovery nextTool: %+v", env.Error)
	}

	has := func(vals []float64, want float64) bool {
		for _, v := range vals {
			if v == want {
				return true
			}
		}
		return false
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		mu.Lock()
		got := append([]float64(nil), progresses...)
		mu.Unlock()
		if has(got, 1.0) || time.Now().After(deadline) {
			if !has(got, 0.25) || !has(got, 1.0) {
				t.Errorf("invoke should emit staged progress including 0.25 and 1.0, got %v", got)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}
