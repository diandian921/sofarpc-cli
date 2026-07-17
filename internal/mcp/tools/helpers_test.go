package tools

import (
	"reflect"
	"strings"
	"testing"

	"github.com/diandian921/sofarpc-mcp/internal/appconfig"
	"github.com/diandian921/sofarpc-mcp/internal/schema"
)

// TestEndpointDataTimeoutAndRedaction pins endpointData: a non-positive override
// falls back to the server's timeout, a positive one wins, profile appears only
// when set, and attachment values never survive unredacted.
func TestEndpointDataTimeoutAndRedaction(t *testing.T) {
	server := appconfig.Server{
		Address:     "127.0.0.1:9",
		Protocol:    "bolt",
		TimeoutMS:   5000,
		AppName:     "app",
		Attachments: map[string]string{"token": "SECRET-VALUE"},
	}
	data := endpointData(server, 0)
	if data["timeoutMs"] != 5000 {
		t.Errorf("zero override should use the server timeout, got %v", data["timeoutMs"])
	}
	if _, has := data["profile"]; has {
		t.Error("profile key must be omitted when empty")
	}
	if atts, _ := data["attachments"].(map[string]string); atts["token"] != redactedValue {
		t.Errorf("attachments must be redacted, got %v", data["attachments"])
	}

	server.Profile = "test"
	overridden := endpointData(server, 250)
	if overridden["timeoutMs"] != 250 || overridden["profile"] != "test" {
		t.Errorf("override/profile not applied: %v", overridden)
	}
}

// TestPublicMethodsStripImports pins the bookkeeping strip: output methods lose
// Imports while the caller's slice keeps them (no aliasing mutation).
func TestPublicMethodsStripImports(t *testing.T) {
	in := []schema.Method{{Method: "m", Imports: map[string]string{"Foo": "com.x.Foo"}}}
	out := publicMethods(in)
	if out[0].Imports != nil {
		t.Error("output should strip Imports")
	}
	if in[0].Imports == nil {
		t.Error("input slice must not be mutated")
	}
}

// TestPublicSearchCandidateShape pins the agent-ready candidate contract:
// paramTypes are normalized to RPC identity FQNs (copyable into invoke),
// parameterNames are flattened, evidence folds into one reason string, and the
// optional summary/outOfPrefix keys appear only when set.
func TestPublicSearchCandidateShape(t *testing.T) {
	method := schema.Method{
		Service:    "com.example.user.UserFacade",
		Method:     "getUser",
		ReturnType: "String",
		Parameters: []schema.Parameter{{Name: "userId", Type: "String"}},
		Score:      12,
		Evidence:   []string{"name match", "param match"},
		SourceFile: "UserFacade.java",
	}
	got := publicSearchCandidate(method)
	if types, _ := got["paramTypes"].([]string); len(types) != 1 || types[0] != "java.lang.String" {
		t.Errorf("paramTypes should be normalized FQNs, got %v", got["paramTypes"])
	}
	if names, _ := got["parameterNames"].([]string); len(names) != 1 || names[0] != "userId" {
		t.Errorf("parameterNames should be flattened, got %v", got["parameterNames"])
	}
	if got["reason"] != "name match; param match" {
		t.Errorf("reason should join evidence, got %v", got["reason"])
	}
	if _, has := got["summary"]; has {
		t.Error("empty summary must be omitted")
	}
	if _, has := got["outOfPrefix"]; has {
		t.Error("false outOfPrefix must be omitted")
	}

	method.Summary = "Fetch a user"
	method.OutOfPrefix = true
	enriched := publicSearchCandidate(method)
	if enriched["summary"] != "Fetch a user" || enriched["outOfPrefix"] != true {
		t.Errorf("summary/outOfPrefix should appear when set: %v", enriched)
	}
}

// TestPublicSearchCandidatesPreserveOrder pins that the ranked order produced by
// schema.Search is kept as-is in the flattened candidate list.
func TestPublicSearchCandidatesPreserveOrder(t *testing.T) {
	out := publicSearchCandidates([]schema.Method{{Method: "first"}, {Method: "second"}})
	if len(out) != 2 || out[0]["method"] != "first" || out[1]["method"] != "second" {
		t.Errorf("candidate order changed: %v", out)
	}
}

// TestPublicDescriptionStripsImports pins that both the described methods and the
// referenced type schemas lose their import bookkeeping before leaving the tool.
func TestPublicDescriptionStripsImports(t *testing.T) {
	desc := schema.Description{
		Service: "com.example.S",
		Methods: []schema.Method{{Method: "m", Imports: map[string]string{"A": "com.x.A"}}},
		Types: map[string]schema.TypeSchema{
			"com.x.A": {Type: "com.x.A", Imports: map[string]string{"B": "com.x.B"}},
		},
	}
	got := publicDescription(desc)
	if got.Methods[0].Imports != nil {
		t.Error("method imports should be stripped")
	}
	if got.Types["com.x.A"].Imports != nil {
		t.Error("type imports should be stripped")
	}
}

// TestValueOrIntOr pins the default-filling helpers used by config_save_server:
// empty/zero (and negative) inputs fall back, real values pass through.
func TestValueOrIntOr(t *testing.T) {
	if got := valueOr("", "def"); got != "def" {
		t.Errorf("valueOr empty = %q", got)
	}
	if got := valueOr("v", "def"); got != "v" {
		t.Errorf("valueOr set = %q", got)
	}
	if got := intOr(0, 42); got != 42 {
		t.Errorf("intOr zero = %d", got)
	}
	if got := intOr(-1, 42); got != 42 {
		t.Errorf("intOr negative = %d", got)
	}
	if got := intOr(7, 42); got != 7 {
		t.Errorf("intOr set = %d", got)
	}
}

// TestPublicServersRedactsAndDropsUnexpected pins the bound-server list exit:
// a real appconfig.Server is redacted through publicServer, non-server keys pass
// through untouched, and an unexpected "server" value type is dropped rather than
// leaked raw.
func TestPublicServersRedactsAndDropsUnexpected(t *testing.T) {
	in := []map[string]interface{}{
		{"name": "a", "server": appconfig.Server{Address: "127.0.0.1:1", Attachments: map[string]string{"token": "SECRET-VALUE"}}},
		{"name": "b", "server": "not-a-server-struct"},
	}
	out := publicServers(in)
	if len(out) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(out))
	}
	first, _ := out[0]["server"].(map[string]interface{})
	if first == nil {
		t.Fatalf("server entry should be a redacted map: %v", out[0])
	}
	if atts, _ := first["attachments"].(map[string]string); atts["token"] != redactedValue {
		t.Errorf("attachments must be redacted: %v", first["attachments"])
	}
	if _, has := out[1]["server"]; has {
		t.Error("unexpected server value type must be dropped, not passed through")
	}
	if out[1]["name"] != "b" {
		t.Errorf("non-server keys should pass through: %v", out[1])
	}
	if !reflect.DeepEqual(in[0]["server"], appconfig.Server{Address: "127.0.0.1:1", Attachments: map[string]string{"token": "SECRET-VALUE"}}) {
		t.Error("input entries must not be mutated")
	}
}

// TestLoadConfigAndConfigPaths pins the SOFARPC_HOME wiring of the two config
// path helpers: both derive from the isolated home and loadConfig succeeds on a
// fresh (absent) config file.
func TestLoadConfigAndConfigPaths(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SOFARPC_HOME", home)
	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig on a fresh home should succeed: %v", err)
	}
	if len(cfg.Projects) != 0 || len(cfg.Servers) != 0 {
		t.Errorf("fresh config should be empty: %+v", cfg)
	}
	path, lock, err := configPaths()
	if err != nil {
		t.Fatalf("configPaths: %v", err)
	}
	if !strings.HasPrefix(path, home) || !strings.HasPrefix(lock, home) {
		t.Errorf("paths should live under SOFARPC_HOME: %q / %q", path, lock)
	}
}
