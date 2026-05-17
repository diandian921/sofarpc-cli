package appconfig

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMissingReturnsDefaults(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "config.json"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Engine.Port != DefaultPort || cfg.Engine.IdleTTL != DefaultIdleTTL || cfg.Engine.Mode != EngineModeJava {
		t.Fatalf("defaults not applied: %+v", cfg.Engine)
	}
	if len(cfg.Projects) != 0 || len(cfg.Servers) != 0 {
		t.Fatalf("expected empty maps: %+v", cfg)
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
}
