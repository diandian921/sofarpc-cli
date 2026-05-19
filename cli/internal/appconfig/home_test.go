package appconfig

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHomeExplicitEnvWins(t *testing.T) {
	custom := t.TempDir()
	t.Setenv(EnvHome, custom)

	got, err := Home()
	if err != nil {
		t.Fatalf("Home: %v", err)
	}
	if got != custom {
		t.Fatalf("Home with %s set = %q, want %q", EnvHome, got, custom)
	}

	gotInstall, err := InstallRoot()
	if err != nil {
		t.Fatalf("InstallRoot: %v", err)
	}
	if gotInstall != custom {
		t.Fatalf("InstallRoot with %s set = %q, want %q", EnvHome, gotInstall, custom)
	}
}

func TestHomeDefaultWhenUnset(t *testing.T) {
	t.Setenv(EnvHome, "")
	home := t.TempDir()
	t.Setenv("HOME", home)

	got, err := Home()
	if err != nil {
		t.Fatalf("Home: %v", err)
	}
	want := filepath.Join(home, DefaultDirName)
	if got != want {
		t.Fatalf("Home default = %q, want %q", got, want)
	}
}

func TestRootFromExeSelfLocate(t *testing.T) {
	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "config.json"), []byte("{}"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	exe := filepath.Join(binDir, "sofarpc")

	got, ok := rootFromExe(exe)
	if !ok {
		t.Fatalf("rootFromExe(%q) ok=false, want true", exe)
	}
	if got != root {
		t.Fatalf("rootFromExe = %q, want %q", got, root)
	}
}

func TestRootFromExeRejectsNonCanonical(t *testing.T) {
	// Not in a bin/ dir.
	if _, ok := rootFromExe(filepath.Join(t.TempDir(), "sofarpc")); ok {
		t.Fatal("rootFromExe should reject a binary not under bin/")
	}
	// In bin/ but no sibling config.json under the parent.
	noConfig := filepath.Join(t.TempDir(), "bin", "sofarpc")
	if _, ok := rootFromExe(noConfig); ok {
		t.Fatal("rootFromExe should reject when parent has no config.json")
	}
}
