package cli

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeSource struct {
	dir     string
	sofarpc string
}

func newFakeSource(t *testing.T) fakeSource {
	t.Helper()
	dir := t.TempDir()
	s := fakeSource{
		dir:     dir,
		sofarpc: filepath.Join(dir, "sofarpc"),
	}
	if err := os.WriteFile(s.sofarpc, []byte("#sofarpc-binary"), 0o755); err != nil {
		t.Fatalf("write fake sofarpc: %v", err)
	}
	return s
}

// stubVersions makes binVersion return values keyed by binary base name
// (paths get symlink-resolved on macOS, so match on the stable basename).
func stubVersions(t *testing.T, byName map[string]string) {
	t.Helper()
	prev := binVersion
	binVersion = func(path, _ string) (string, error) {
		if v, ok := byName[filepath.Base(path)]; ok {
			return v, nil
		}
		return "unknown", nil
	}
	t.Cleanup(func() { binVersion = prev })
}

func stubExecutable(t *testing.T, path string) {
	t.Helper()
	prev := executablePath
	executablePath = func() (string, error) { return path, nil }
	t.Cleanup(func() { executablePath = prev })
}

func runSI(t *testing.T, root string, buildVersion string, args ...string) (int, string, string) {
	t.Helper()
	t.Setenv("SOFARPC_HOME", root)
	var out, errBuf bytes.Buffer
	code := runSelfInstall(args, Env{BuildVersion: buildVersion, Stdout: &out, Stderr: &errBuf})
	return code, out.String(), errBuf.String()
}

func TestSelfInstallFreshInstall(t *testing.T) {
	src := newFakeSource(t)
	root := t.TempDir()
	stubExecutable(t, src.sofarpc)
	stubVersions(t, nil)

	code, out, errOut := runSI(t, root, "v1.0.0")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, errOut)
	}
	for _, p := range []string{
		filepath.Join(root, "bin", "sofarpc"),
		filepath.Join(root, "config.json"),
		filepath.Join(root, "cache", "schema"),
	} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("expected %s to exist: %v", p, err)
		}
	}
	if !strings.Contains(out, "Installed:") {
		t.Fatalf("missing Installed banner: %s", out)
	}
	// Single-binary layout: sofarpc-mcp must NOT be installed as a separate file.
	if _, err := os.Stat(filepath.Join(root, "bin", "sofarpc-mcp")); err == nil {
		t.Fatal("sofarpc-mcp must not be installed under the single-binary layout")
	}
}

func TestSelfInstallRemovesLegacyBinaries(t *testing.T) {
	src := newFakeSource(t)
	root := t.TempDir()
	stubExecutable(t, src.sofarpc)
	stubVersions(t, nil)
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	legacy := []string{"sofarpc-cli", "sofarpc-mcp"}
	for _, name := range legacy {
		if err := os.WriteFile(filepath.Join(binDir, name), []byte("legacy"), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	code, _, errOut := runSI(t, root, "v1.0.0")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, errOut)
	}
	for _, name := range legacy {
		if _, err := os.Stat(filepath.Join(binDir, name)); !os.IsNotExist(err) {
			t.Fatalf("legacy %s should be removed during install, stat err=%v", name, err)
		}
	}
}

func TestSelfInstallIdempotentNoop(t *testing.T) {
	src := newFakeSource(t)
	root := t.TempDir()
	stubExecutable(t, src.sofarpc)
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "sofarpc"), []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	stubVersions(t, map[string]string{"sofarpc": "v1.2.3"})

	code, out, errOut := runSI(t, root, "v1.2.3")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, errOut)
	}
	if !strings.Contains(out, "Already installed") {
		t.Fatalf("expected no-op message, got: %s", out)
	}
}

func TestSelfInstallNoopFailsWhenScaffoldFails(t *testing.T) {
	src := newFakeSource(t)
	root := t.TempDir()
	stubExecutable(t, src.sofarpc)
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "sofarpc"), []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	stubVersions(t, map[string]string{"sofarpc": "v1.2.3"})
	prevMkdirAll := mkdirAll
	mkdirAll = func(string, os.FileMode) error {
		return errors.New("disk full")
	}
	t.Cleanup(func() { mkdirAll = prevMkdirAll })

	code, out, errOut := runSI(t, root, "v1.2.3")
	if code == 0 {
		t.Fatal("no-op install must fail when scaffold cannot be verified")
	}
	if out != "" {
		t.Fatalf("must not print no-op success when scaffold fails, got stdout: %s", out)
	}
	if !strings.Contains(errOut, "disk full") {
		t.Fatalf("want scaffold error, got: %s", errOut)
	}
}

func TestSelfInstallDowngradeBlockedThenAllowed(t *testing.T) {
	src := newFakeSource(t)
	root := t.TempDir()
	stubExecutable(t, src.sofarpc)
	target := filepath.Join(root, "bin", "sofarpc")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("newer"), 0o755); err != nil {
		t.Fatal(err)
	}
	stubVersions(t, map[string]string{"sofarpc": "v2.0.0"})

	code, _, errOut := runSI(t, root, "v1.0.0")
	if code == 0 {
		t.Fatal("downgrade should be blocked without --allow-downgrade")
	}
	if !strings.Contains(errOut, "older version") {
		t.Fatalf("want downgrade-block message, got: %s", errOut)
	}

	code, out, errOut := runSI(t, root, "v1.0.0", "--allow-downgrade")
	if code != 0 {
		t.Fatalf("downgrade with --allow-downgrade should succeed: exit=%d stderr=%s", code, errOut)
	}
	if !strings.Contains(out, "Installed:") {
		t.Fatalf("expected install after --allow-downgrade, got: %s", out)
	}
}

func TestSelfInstallPrereleaseDowngradeBlocked(t *testing.T) {
	src := newFakeSource(t)
	root := t.TempDir()
	stubExecutable(t, src.sofarpc)
	target := filepath.Join(root, "bin", "sofarpc")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("newer-beta"), 0o755); err != nil {
		t.Fatal(err)
	}
	stubVersions(t, map[string]string{"sofarpc": "v1.0.0-beta.2"})

	code, _, errOut := runSI(t, root, "v1.0.0-beta.1")
	if code == 0 {
		t.Fatal("prerelease downgrade should be blocked without --allow-downgrade")
	}
	if !strings.Contains(errOut, "older version") {
		t.Fatalf("want downgrade-block message, got: %s", errOut)
	}
}

func TestSelfInstallUninstallKeepsConfig(t *testing.T) {
	src := newFakeSource(t)
	root := t.TempDir()
	stubExecutable(t, src.sofarpc)
	stubVersions(t, nil)
	if code, _, e := runSI(t, root, "v1.0.0"); code != 0 {
		t.Fatalf("install failed: %s", e)
	}

	code, out, errOut := runSI(t, root, "v1.0.0", "--uninstall")
	if code != 0 {
		t.Fatalf("uninstall exit=%d stderr=%s", code, errOut)
	}
	if !strings.Contains(out, "Uninstalled") {
		t.Fatalf("want uninstall message, got: %s", out)
	}
	if _, err := os.Stat(filepath.Join(root, "bin", "sofarpc")); err == nil {
		t.Fatal("binary should be removed after uninstall")
	}
	if _, err := os.Stat(filepath.Join(root, "config.json")); err != nil {
		t.Fatal("config.json must be preserved after uninstall")
	}
}

func TestSelfInstallDoesNotOverwriteConfig(t *testing.T) {
	src := newFakeSource(t)
	root := t.TempDir()
	stubExecutable(t, src.sofarpc)
	stubVersions(t, nil)

	configPath := filepath.Join(root, "config.json")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	sentinel := `{"version":1,"projects":{"keep":{"workspaceRoot":"/x","servicePrefixes":[]}},"servers":{}}`
	if err := os.WriteFile(configPath, []byte(sentinel), 0o644); err != nil {
		t.Fatal(err)
	}

	if code, _, e := runSI(t, root, "v1.0.0"); code != 0 {
		t.Fatalf("install failed: %s", e)
	}
	got, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), `"keep"`) {
		t.Fatalf("existing config.json must not be overwritten, got: %s", got)
	}
}

func TestCompareSemver(t *testing.T) {
	cases := []struct {
		a, b   string
		want   int
		usable bool
	}{
		{"v1.0.0", "v1.0.0", 0, true},
		{"v1.2.0", "v1.1.9", 1, true},
		{"v1.0.0", "v2.0.0", -1, true},
		{"v1.0.0-beta.2", "v1.0.0-beta.1", 1, true},
		{"v1.0.0-beta.10", "v1.0.0-beta.2", 1, true},
		{"v1.0.0-beta.1", "v1.0.0", -1, true},
		{"v1.0.0+build.2", "v1.0.0+build.1", 0, true},
		{"dev", "v1.0.0", 0, false},
		{"abc123", "def456", 0, false},
		// git-describe dev builds ("-<commits>-g<hash>") are not cleanly ordered against
		// release tags by semver precedence, so they must be reported as not comparable —
		// otherwise a clean release (beta.10) is wrongly ranked below a dev build of an
		// earlier tag (beta.9-18-gHASH) and `install` refuses it as a downgrade.
		{"v0.1.0-beta.10", "v0.1.0-beta.9-18-gdd32f79", 0, false},
		{"v0.1.0-beta.9-18-gdd32f79", "v0.1.0-beta.9", 0, false},
		{"v0.1.0-beta.9-18-gabc1234-dirty", "v0.1.0-beta.10", 0, false},
	}
	for _, c := range cases {
		got, usable := compareSemver(c.a, c.b)
		if usable != c.usable || (usable && got != c.want) {
			t.Fatalf("compareSemver(%q,%q)=(%d,%v), want (%d,%v)", c.a, c.b, got, usable, c.want, c.usable)
		}
	}
}

// TestDecideInstallAllowsReleaseOverDevBuild guards the install version fix: when the
// installed binary is a git-describe dev build, installing a clean release tag must
// proceed instead of being refused as a downgrade.
func TestDecideInstallAllowsReleaseOverDevBuild(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "sofarpc")
	if err := os.WriteFile(target, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	orig := binVersion
	binVersion = func(string, string) (string, error) { return "v0.1.0-beta.9-18-gdd32f79", nil }
	defer func() { binVersion = orig }()
	if d := decideInstall("v0.1.0-beta.10", target, false); d != installProceed {
		t.Errorf("installing release over a dev build = %v, want installProceed", d)
	}
}
