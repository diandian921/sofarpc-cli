package tools

import (
	"reflect"
	"strings"
	"testing"

	"github.com/diandian921/sofarpc-mcp/internal/appconfig"
	"github.com/diandian921/sofarpc-mcp/internal/schema"
)

// helperConfig builds an in-memory config with two projects and three servers,
// covering the plain, profile-bound, and cross-project shapes the resolvers see.
func helperConfig() appconfig.Config {
	return appconfig.Config{
		Projects: map[string]appconfig.Project{
			"user":  {WorkspaceRoot: "/ws/user"},
			"other": {WorkspaceRoot: "/ws/other"},
		},
		Servers: map[string]appconfig.Server{
			"user-dev":  {Address: "127.0.0.1:1", Project: "user", Profile: "dev"},
			"user-test": {Address: "127.0.0.1:2", Project: "user", Profile: "test"},
			"solo":      {Address: "127.0.0.1:3", Project: "other"},
		},
	}
}

// TestResolveProjectSelection pins every branch of the project resolver:
// explicit names, server-derived inference, the single-project fallback, and the
// error paths for mismatches and ambiguity.
func TestResolveProjectSelection(t *testing.T) {
	cfg := helperConfig()
	cases := []struct {
		name     string
		explicit string
		server   string
		want     string
		wantErr  string
	}{
		{name: "explicit found", explicit: "user", want: "user"},
		{name: "explicit missing", explicit: "ghost", wantErr: `project "ghost" not found`},
		{name: "explicit with matching server", explicit: "user", server: "user-dev", want: "user"},
		{name: "explicit with mismatched server", explicit: "other", server: "user-dev", wantErr: "is bound to project"},
		{name: "explicit with missing server", explicit: "user", server: "ghost", wantErr: `server "ghost" not found`},
		{name: "server-derived", server: "solo", want: "other"},
		{name: "server missing", server: "ghost", wantErr: `server "ghost" not found`},
		{name: "ambiguous without hints", wantErr: "project is required"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			name, _, err := resolveProject(cfg, tc.explicit, tc.server)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("want error containing %q, got %v", tc.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if name != tc.want {
				t.Errorf("resolved project = %q, want %q", name, tc.want)
			}
		})
	}
}

// TestResolveProjectDanglingServerReference pins the corrupted-config path: a
// server bound to a project that no longer exists is reported, not resolved.
func TestResolveProjectDanglingServerReference(t *testing.T) {
	cfg := appconfig.Config{
		Projects: map[string]appconfig.Project{},
		Servers:  map[string]appconfig.Server{"orphan": {Address: "127.0.0.1:1", Project: "gone"}},
	}
	_, _, err := resolveProject(cfg, "", "orphan")
	if err == nil || !strings.Contains(err.Error(), "missing project") {
		t.Fatalf("dangling reference should be reported, got %v", err)
	}
}

// TestResolveProjectSingleFallback pins the convenience path: with exactly one
// configured project and no hints, that project wins.
func TestResolveProjectSingleFallback(t *testing.T) {
	cfg := appconfig.Config{Projects: map[string]appconfig.Project{"only": {WorkspaceRoot: "/ws"}}}
	name, project, err := resolveProject(cfg, "", "")
	if err != nil || name != "only" || project.WorkspaceRoot != "/ws" {
		t.Fatalf("single project should be inferred, got %q %v %v", name, project, err)
	}
}

// TestResolveServerSelection pins every branch of the server resolver: explicit
// lookup with project/profile guards, profile-derived names, the activeProfile
// fallback, single-match inference, and the required/optional ambiguity split.
func TestResolveServerSelection(t *testing.T) {
	cfg := helperConfig()
	cases := []struct {
		name     string
		project  string
		profile  string
		explicit string
		required bool
		want     string
		wantHas  bool
		wantErr  string
	}{
		{name: "explicit found", explicit: "solo", want: "solo", wantHas: true},
		{name: "explicit missing", explicit: "ghost", wantErr: `server "ghost" not found`},
		{name: "explicit project mismatch", explicit: "solo", project: "user", wantErr: "is bound to project"},
		{name: "explicit profile mismatch", explicit: "user-dev", profile: "test", wantErr: "is bound to profile"},
		{name: "profile requires project", profile: "dev", wantErr: "project is required when profile is specified"},
		{name: "profile with project", project: "user", profile: "dev", want: "user-dev", wantHas: true},
		{name: "profile not found", project: "user", profile: "prod", wantErr: `profile "prod" for project "user" not found`},
		{name: "single match via project filter", project: "other", want: "solo", wantHas: true},
		{name: "ambiguous optional", project: "user", required: false, wantHas: false},
		{name: "ambiguous required with project", project: "user", required: true, wantErr: `project "user" has 2 configured servers`},
		{name: "ambiguous required without project", required: true, wantErr: "3 servers are configured"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			name, _, has, err := resolveServer(cfg, tc.project, tc.profile, tc.explicit, tc.required)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("want error containing %q, got %v", tc.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if has != tc.wantHas || name != tc.want {
				t.Errorf("resolved (%q, has=%v), want (%q, has=%v)", name, has, tc.want, tc.wantHas)
			}
		})
	}
}

// TestResolveServerActiveProfileFallback pins the activeProfile convenience: a
// project with an active profile resolves to that profile's server without an
// explicit name, even when several servers exist.
func TestResolveServerActiveProfileFallback(t *testing.T) {
	cfg := helperConfig()
	project := cfg.Projects["user"]
	project.ActiveProfile = "test"
	cfg.Projects["user"] = project
	name, server, has, err := resolveServer(cfg, "user", "", "", true)
	if err != nil || !has {
		t.Fatalf("activeProfile fallback failed: has=%v err=%v", has, err)
	}
	if name != appconfig.ServerNameForProfile("user", "test") || server.Address != "127.0.0.1:2" {
		t.Errorf("resolved %q (%s), want the active-profile server", name, server.Address)
	}
}

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
