package appconfig

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func TestLoadMissingReturnsDefaults(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "config.json"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Version != CurrentConfigVersion {
		t.Fatalf("version = %d, want %d", cfg.Version, CurrentConfigVersion)
	}
	if len(cfg.Projects) != 0 || len(cfg.Servers) != 0 {
		t.Fatalf("expected empty maps: %+v", cfg)
	}
}

func TestLoadIgnoresDeprecatedEngineConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"projects":{},"servers":{},"engine":{"mode":"java","port":37651}}`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Projects) != 0 || len(cfg.Servers) != 0 {
		t.Fatalf("expected empty maps: %+v", cfg)
	}
	if cfg.Version != LegacyConfigVersion {
		t.Fatalf("version = %d, want legacy version %d", cfg.Version, LegacyConfigVersion)
	}
}

func TestLoadRejectsUnsupportedFutureVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"version":999,"projects":{},"servers":{}}`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, err := Load(path)
	var cfgErr *ConfigError
	if !errors.As(err, &cfgErr) {
		t.Fatalf("expected ConfigError, got %T: %v", err, err)
	}
	if cfgErr.Code != CodeConfigUnsupported {
		t.Fatalf("code = %q, want %q", cfgErr.Code, CodeConfigUnsupported)
	}
}

func TestLoadFutureVersionWinsOverUnknownFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"version":999,"futureField":{"x":1}}`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, err := Load(path)
	var cfgErr *ConfigError
	if !errors.As(err, &cfgErr) || cfgErr.Code != CodeConfigUnsupported {
		t.Fatalf("err = %v, want unsupported-version before strict field validation", err)
	}
}

func TestLoadInvalidJSONReturnsConfigInvalid(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte("{bad json"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	_, err := Load(path)
	var cfgErr *ConfigError
	if !errors.As(err, &cfgErr) {
		t.Fatalf("expected ConfigError, got %T: %v", err, err)
	}
	if cfgErr.Code != CodeConfigInvalid {
		t.Fatalf("code = %q, want %q", cfgErr.Code, CodeConfigInvalid)
	}
}

func TestAddProjectCanonicalizesWorkspaceAndPrefixes(t *testing.T) {
	root := t.TempDir()
	var cfg = DefaultConfig()

	project, err := cfg.AddProject("user", root, []string{"com.example.user", "com.example.user.", ""}, false)
	if err != nil {
		t.Fatalf("AddProject: %v", err)
	}
	wantRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	if project.WorkspaceRoot != wantRoot {
		t.Fatalf("workspaceRoot = %q, want %q", project.WorkspaceRoot, wantRoot)
	}
	if got := project.ServicePrefixes; len(got) != 1 || got[0] != "com.example.user." {
		t.Fatalf("prefixes = %#v", got)
	}
}

func TestAddServerAppliesDefaultsAndRequiresProject(t *testing.T) {
	root := t.TempDir()
	var cfg = DefaultConfig()
	if _, err := cfg.AddProject("user", root, nil, false); err != nil {
		t.Fatalf("AddProject: %v", err)
	}

	server, err := cfg.AddServer("user_test", Server{Address: "127.0.0.1:12200", Project: "user"}, false)
	if err != nil {
		t.Fatalf("AddServer: %v", err)
	}
	if server.Protocol != DefaultServerProtocol || server.TimeoutMS != DefaultServerTimeoutMS || server.AppName != DefaultServerAppName {
		t.Fatalf("defaults not applied: %+v", server)
	}
	if server.Attachments == nil {
		t.Fatalf("attachments should be initialized")
	}
}

func TestLoadV2ProfilesDerivesCompatibilityServers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	body := `{
  "version": 2,
  "defaults": {
    "protocol": "bolt",
    "timeoutMs": 5000,
    "appName": "sofarpc-agent",
    "attachments": {}
  },
  "projects": {
    "salesfundmp": {
      "activeProfile": "test",
      "workspaceRoot": "/tmp/salesfundmp",
      "servicePrefixes": ["com.thfund.salesfundmp.facade."],
      "profiles": {
        "local": {"address": "127.0.0.1:12300", "timeoutMs": 1000},
        "test": {"address": "10.74.194.40:12200"}
      }
    }
  }
}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Version != CurrentConfigVersion {
		t.Fatalf("version = %d, want %d", cfg.Version, CurrentConfigVersion)
	}
	project := cfg.Projects["salesfundmp"]
	if project.ActiveProfile != "test" {
		t.Fatalf("activeProfile = %q", project.ActiveProfile)
	}
	local := cfg.Servers["salesfundmp-local"]
	if local.Project != "salesfundmp" || local.Profile != "local" || local.TimeoutMS != 1000 {
		t.Fatalf("local server = %+v", local)
	}
	test := cfg.Servers["salesfundmp-test"]
	if test.Project != "salesfundmp" || test.Profile != "test" || test.TimeoutMS != DefaultServerTimeoutMS || test.AppName != DefaultServerAppName {
		t.Fatalf("test server = %+v", test)
	}
}

func TestSaveV2ProfilesOmitsDerivedServers(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	cfg := DefaultConfig()
	if _, err := cfg.AddProject("user", dir, []string{"com.example"}, false); err != nil {
		t.Fatalf("AddProject: %v", err)
	}
	if _, err := cfg.AddProfile("user", "test", Profile{Address: "127.0.0.1:12200"}, false); err != nil {
		t.Fatalf("AddProfile: %v", err)
	}

	if err := Save(path, cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var disk map[string]interface{}
	if err := json.Unmarshal(raw, &disk); err != nil {
		t.Fatalf("unmarshal saved config: %v", err)
	}
	if _, exists := disk["servers"]; exists {
		t.Fatalf("derived servers should not be persisted in v2 config: %s", raw)
	}
}

func TestAddServerInfersProfileFromProjectPrefixedName(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig()
	if _, err := cfg.AddProject("user", root, nil, false); err != nil {
		t.Fatalf("AddProject: %v", err)
	}
	server, err := cfg.AddServer("user-test", Server{Address: "127.0.0.1:12200", Project: "user"}, false)
	if err != nil {
		t.Fatalf("AddServer: %v", err)
	}
	if server.Profile != "test" {
		t.Fatalf("server profile = %q", server.Profile)
	}
	if _, ok := cfg.Projects["user"].Profiles["test"]; !ok {
		t.Fatalf("profile not stored on project: %+v", cfg.Projects["user"])
	}
	if cfg.Projects["user"].ActiveProfile != "test" {
		t.Fatalf("activeProfile = %q", cfg.Projects["user"].ActiveProfile)
	}
}

func TestRemoveServerProfileKeepsProjectActiveProfileValid(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig()
	if _, err := cfg.AddProject("user", root, nil, false); err != nil {
		t.Fatalf("AddProject: %v", err)
	}
	if _, err := cfg.AddProfile("user", "local", Profile{Address: "127.0.0.1:12200"}, false); err != nil {
		t.Fatalf("AddProfile local: %v", err)
	}
	if _, err := cfg.AddProfile("user", "test", Profile{Address: "10.0.0.1:12200"}, false); err != nil {
		t.Fatalf("AddProfile test: %v", err)
	}
	project := cfg.Projects["user"]
	project.ActiveProfile = "local"
	cfg.Projects["user"] = project

	if err := cfg.RemoveServer("user-local", true); err != nil {
		t.Fatalf("RemoveServer: %v", err)
	}
	if _, ok := cfg.Projects["user"].Profiles["local"]; ok {
		t.Fatalf("local profile still exists: %+v", cfg.Projects["user"].Profiles)
	}
	if got := cfg.Projects["user"].ActiveProfile; got != "test" {
		t.Fatalf("activeProfile = %q, want test", got)
	}
}

func TestUpdateWritesAtomically(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	lock := filepath.Join(dir, "state", "config.lock")

	_, err := Update(path, lock, func(cfg *Config) error {
		_, err := cfg.AddProject("user", dir, []string{"com.example"}, false)
		return err
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, ok := loaded.Projects["user"]; !ok {
		t.Fatalf("project not persisted: %+v", loaded)
	}
	if loaded.Version != CurrentConfigVersion {
		t.Fatalf("version = %d, want %d", loaded.Version, CurrentConfigVersion)
	}
}

func TestUpdateMigratesLegacyServerIntoUsableProfile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	lock := filepath.Join(dir, "state", "config.lock")
	body := `{
  "projects": {"user": {"workspaceRoot":"/tmp/user","servicePrefixes":[]}},
  "servers": {"user-test": {"address":"127.0.0.1:12200","project":"user","protocol":"bolt","timeoutMs":5000,"appName":"test","attachments":{}}}
}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	updated, err := Update(path, lock, func(*Config) error { return nil })
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	project := updated.Projects["user"]
	if updated.Version != CurrentConfigVersion || project.ActiveProfile != "test" {
		t.Fatalf("migrated config = %#v", updated)
	}
	if profile, ok := project.Profiles["test"]; !ok || profile.Address != "127.0.0.1:12200" {
		t.Fatalf("migrated profile = %#v", project.Profiles)
	}
}

func TestSaveDoesNotMutateCallerMaps(t *testing.T) {
	dir := t.TempDir()
	cfg := DefaultConfig()
	cfg.Defaults.Attachments["tenant"] = "blue"
	cfg.Projects["user"] = Project{
		WorkspaceRoot: "/tmp/user", ServicePrefixes: []string{"com.example"}, ActiveProfile: "test",
		Profiles: map[string]Profile{"test": {Address: "127.0.0.1:12200", Attachments: map[string]string{"trace": "one"}}},
	}
	cfg.Servers["user-test"] = cfg.ServerFromProfile("user", "test", cfg.Projects["user"].Profiles["test"])
	before := cloneConfig(cfg)
	if err := Save(filepath.Join(dir, "config.json"), cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if !reflect.DeepEqual(cfg, before) {
		t.Fatalf("Save mutated caller:\n got  %#v\n want %#v", cfg, before)
	}
}

func TestAddProfileRejectsDerivedServerNameCollision(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Projects["a"] = Project{Profiles: map[string]Profile{"b-c": {Address: "127.0.0.1:1"}}}
	cfg.Projects["a-b"] = Project{}
	_, err := cfg.AddProfile("a-b", "c", Profile{Address: "127.0.0.1:2"}, false)
	if err == nil || !strings.Contains(err.Error(), "derived server name") {
		t.Fatalf("err = %v, want deterministic collision", err)
	}
}

func TestEnsureExistsPreservesExistingConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	lock := filepath.Join(dir, "state", "config.lock")
	want := []byte(`{"version":2,"defaults":{},"projects":{}}`)
	if err := os.WriteFile(path, want, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := EnsureExists(path, lock); err != nil {
		t.Fatalf("EnsureExists: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("existing config changed: %q", got)
	}
}

func TestConcurrentUpdatesDoNotLoseChanges(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	lock := filepath.Join(dir, "state", "config.lock")
	if err := EnsureExists(path, lock); err != nil {
		t.Fatalf("EnsureExists: %v", err)
	}

	const workers = 12
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := Update(path, lock, func(cfg *Config) error {
				name := fmt.Sprintf("project-%02d", i)
				cfg.Projects[name] = Project{WorkspaceRoot: "/tmp/" + name}
				return nil
			})
			errs <- err
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent Update: %v", err)
		}
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Projects) != workers {
		t.Fatalf("projects = %d, want %d", len(cfg.Projects), workers)
	}
}
