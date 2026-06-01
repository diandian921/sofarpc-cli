package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/diandian921/sofarpc-mcp/internal/app"
	"github.com/diandian921/sofarpc-mcp/internal/appconfig"
	"github.com/diandian921/sofarpc-mcp/internal/mcp/server"
)

const (
	sentinelKey   = "_sofa_token"
	sentinelValue = "SENTINEL_ATTACHMENT_VALUE_8f3a"
)

// seedConfigAttach seeds a project with two servers, both carrying a sentinel
// attachment value, so every server/endpoint exit can be checked for leaks.
func seedConfigAttach(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("SOFARPC_HOME", home)
	ws := filepath.Join(home, "ws")
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatalf("mkdir ws: %v", err)
	}
	path, err := appconfig.DefaultPath()
	if err != nil {
		t.Fatalf("default path: %v", err)
	}
	lock, err := appconfig.DefaultLockPath()
	if err != nil {
		t.Fatalf("lock path: %v", err)
	}
	att := func() map[string]string { return map[string]string{sentinelKey: sentinelValue} }
	if _, err := appconfig.Update(path, lock, func(c *appconfig.Config) error {
		if _, err := c.AddProject("user", ws, nil, false); err != nil {
			return err
		}
		if _, err := c.AddServer("user-test", appconfig.Server{Address: "127.0.0.1:12200", Project: "user", Attachments: att()}, false); err != nil {
			return err
		}
		_, err := c.AddServer("user-test2", appconfig.Server{Address: "127.0.0.1:12201", Project: "user", Attachments: att()}, false)
		return err
	}); err != nil {
		t.Fatalf("seed config: %v", err)
	}
	return ws
}

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

type fixedStore struct{ cfg appconfig.Config }

func (s fixedStore) Load() (appconfig.Config, error) { return s.cfg, nil }

// TestResolveMultiServerUsesInjectedStore pins that the multi-server branch reads
// the servers app resolved from appSvc's ConfigStore, not the global config path.
// SOFARPC_HOME points at an empty dir, so a re-read of the global config would
// return no servers; only redacting resolved.Servers keeps the output correct.
func TestResolveMultiServerUsesInjectedStore(t *testing.T) {
	t.Setenv("SOFARPC_HOME", t.TempDir())
	cfg := appconfig.Config{
		Projects: map[string]appconfig.Project{"user": {WorkspaceRoot: t.TempDir()}},
		Servers: map[string]appconfig.Server{
			"s1": {Address: "127.0.0.1:1", Project: "user", Attachments: map[string]string{sentinelKey: sentinelValue}},
			"s2": {Address: "127.0.0.1:2", Project: "user", Attachments: map[string]string{sentinelKey: sentinelValue}},
		},
	}
	out := ResolveTool(app.New(fixedStore{cfg: cfg})).Run(context.Background(), nopRuntime{}, ResolveArgs{})
	r := asResult(t, out.Structured)
	if !r.OK {
		t.Fatalf("resolve must succeed off the injected store: %+v", r)
	}
	js := structuredJSON(t, out.Structured)
	if !strings.Contains(js, "127.0.0.1:1") || !strings.Contains(js, "127.0.0.1:2") {
		t.Fatalf("resolve must return the injected store's servers, not the global config: %s", js)
	}
	assertRedacted(t, "resolve(injected servers)", js)
}

func TestResolveSingleEndpointRedactsAttachments(t *testing.T) {
	seedConfigAttach(t)
	out := ResolveTool(app.New(nil)).Run(context.Background(), nopRuntime{}, ResolveArgs{Server: "user-test"})
	assertRedacted(t, "resolve(endpoint)", structuredJSON(t, out.Structured))
}

func TestResolveMultiServerRedactsAttachments(t *testing.T) {
	seedConfigAttach(t)
	out := ResolveTool(app.New(nil)).Run(context.Background(), nopRuntime{}, ResolveArgs{Project: "user"})
	assertRedacted(t, "resolve(servers)", structuredJSON(t, out.Structured))
}

func TestConfigListRedactsAttachments(t *testing.T) {
	seedConfigAttach(t)
	out := ConfigListTool(true).Run(context.Background(), nopRuntime{}, ConfigListArgs{})
	assertRedacted(t, "config_list(servers)", structuredJSON(t, out.Structured))
}

func TestConfigSaveServerRedactsAttachments(t *testing.T) {
	seedConfigAttach(t)
	out := ConfigSaveServerTool().Run(context.Background(), nopRuntime{}, ConfigSaveServerArgs{
		Name:        "user-test",
		Address:     "127.0.0.1:12200",
		Project:     "user",
		Attachments: map[string]string{sentinelKey: sentinelValue},
		Overwrite:   true,
	})
	assertRedacted(t, "config_save_server(saved)", structuredJSON(t, out.Structured))
}

func TestDoctorRedactsAttachments(t *testing.T) {
	seedConfigAttach(t)
	out := DoctorTool(app.New(nil), true).Run(context.Background(), nopRuntime{}, DoctorArgs{Server: "user-test"})
	assertRedacted(t, "doctor(endpoint)", structuredJSON(t, out.Structured))
}

// TestInvokePlanDisplayRedactsAttachments exercises the plan exit directly: the
// real invoke_plan path needs a built source-schema index, but publicPlanDisplay
// is the only redaction point and must leave the source plan untouched.
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

// TestNoToolLeaksAttachmentValue is the catch-all net: drive every tool that can
// emit a configured server/endpoint and assert none ever surfaces the sentinel
// value. This guards against a future tool wiring app.Endpoint / appconfig.Server
// straight into structured content, which the per-exit tests above cannot see.
func TestNoToolLeaksAttachmentValue(t *testing.T) {
	seedConfigAttach(t)
	svc := app.New(nil)
	ctx := context.Background()
	cases := []struct {
		name string
		run  func() server.Result
	}{
		{"resolve_single", func() server.Result {
			return ResolveTool(svc).Run(ctx, nopRuntime{}, ResolveArgs{Server: "user-test"})
		}},
		{"resolve_multi", func() server.Result {
			return ResolveTool(svc).Run(ctx, nopRuntime{}, ResolveArgs{Project: "user"})
		}},
		{"config_list", func() server.Result {
			return ConfigListTool(true).Run(ctx, nopRuntime{}, ConfigListArgs{})
		}},
		{"config_save_server", func() server.Result {
			return ConfigSaveServerTool().Run(ctx, nopRuntime{}, ConfigSaveServerArgs{
				Name: "user-test", Address: "127.0.0.1:12200", Project: "user",
				Attachments: map[string]string{sentinelKey: sentinelValue}, Overwrite: true,
			})
		}},
		{"doctor", func() server.Result {
			return DoctorTool(svc, true).Run(ctx, nopRuntime{}, DoctorArgs{Server: "user-test"})
		}},
	}
	for _, c := range cases {
		js := structuredJSON(t, c.run().Structured)
		if strings.Contains(js, sentinelValue) {
			t.Fatalf("tool %s leaked attachment value into structured content: %s", c.name, js)
		}
	}
}
