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
	mcp     string
}

func newFakeSource(t *testing.T) fakeSource {
	t.Helper()
	dir := t.TempDir()
	s := fakeSource{
		dir:     dir,
		sofarpc: filepath.Join(dir, "sofarpc"),
		mcp:     filepath.Join(dir, "sofarpc-mcp"),
	}
	if err := os.WriteFile(s.sofarpc, []byte("#sofarpc-binary"), 0o755); err != nil {
		t.Fatalf("write fake sofarpc: %v", err)
	}
	if err := os.WriteFile(s.mcp, []byte("#sofarpc-mcp-binary"), 0o755); err != nil {
		t.Fatalf("write fake sofarpc-mcp: %v", err)
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
	stubVersions(t, map[string]string{"sofarpc-mcp": "v1.0.0"})

	code, out, errOut := runSI(t, root, "v1.0.0")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, errOut)
	}
	for _, p := range []string{
		filepath.Join(root, "bin", "sofarpc"),
		filepath.Join(root, "bin", "sofarpc-mcp"),
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
}

func TestSelfInstallVersionMismatchRefused(t *testing.T) {
	src := newFakeSource(t)
	root := t.TempDir()
	stubExecutable(t, src.sofarpc)
	stubVersions(t, map[string]string{"sofarpc-mcp": "v2.0.0"})

	code, _, errOut := runSI(t, root, "v1.0.0")
	if code == 0 {
		t.Fatal("expected non-zero exit on version mismatch")
	}
	if !strings.Contains(errOut, "version mismatch") {
		t.Fatalf("want version mismatch error, got: %s", errOut)
	}
	if _, err := os.Stat(filepath.Join(root, "bin", "sofarpc")); err == nil {
		t.Fatal("binary should not have been installed on mismatch")
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
	// Both delivery-set binaries present at the same version → true no-op.
	if err := os.WriteFile(filepath.Join(binDir, "sofarpc"), []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "sofarpc-mcp"), []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	stubVersions(t, map[string]string{"sofarpc-mcp": "v1.2.3", "sofarpc": "v1.2.3"})

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
	if err := os.WriteFile(filepath.Join(binDir, "sofarpc-mcp"), []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	stubVersions(t, map[string]string{"sofarpc-mcp": "v1.2.3", "sofarpc": "v1.2.3"})
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

func TestSelfInstallReinstallsWhenMcpMissing(t *testing.T) {
	src := newFakeSource(t)
	root := t.TempDir()
	stubExecutable(t, src.sofarpc)
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// CLI present and same version, but sofarpc-mcp absent: must NOT no-op.
	if err := os.WriteFile(filepath.Join(binDir, "sofarpc"), []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	stubVersions(t, map[string]string{"sofarpc-mcp": "v1.0.0", "sofarpc": "v1.0.0"})

	code, out, errOut := runSI(t, root, "v1.0.0")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, errOut)
	}
	if strings.Contains(out, "Already installed") {
		t.Fatalf("must reinstall when sofarpc-mcp is missing, got no-op: %s", out)
	}
	if _, err := os.Stat(filepath.Join(binDir, "sofarpc-mcp")); err != nil {
		t.Fatalf("sofarpc-mcp must be installed during repair: %v", err)
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
	stubVersions(t, map[string]string{"sofarpc-mcp": "v1.0.0", "sofarpc": "v2.0.0"})

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
	stubVersions(t, map[string]string{"sofarpc-mcp": "v1.0.0-beta.1", "sofarpc": "v1.0.0-beta.2"})

	code, _, errOut := runSI(t, root, "v1.0.0-beta.1")
	if code == 0 {
		t.Fatal("prerelease downgrade should be blocked without --allow-downgrade")
	}
	if !strings.Contains(errOut, "older version") {
		t.Fatalf("want downgrade-block message, got: %s", errOut)
	}
}

func TestSelfInstallNoSiblingNoPathFallback(t *testing.T) {
	dir := t.TempDir()
	lonely := filepath.Join(dir, "sofarpc")
	if err := os.WriteFile(lonely, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	stubExecutable(t, lonely)
	root := t.TempDir()

	code, _, errOut := runSI(t, root, "v1.0.0")
	if code == 0 {
		t.Fatal("expected failure when sofarpc-mcp sibling is absent")
	}
	if !strings.Contains(errOut, "no PATH fallback") {
		t.Fatalf("want explicit no-PATH-fallback error, got: %s", errOut)
	}
}

func TestSelfInstallUninstallKeepsConfig(t *testing.T) {
	src := newFakeSource(t)
	root := t.TempDir()
	stubExecutable(t, src.sofarpc)
	stubVersions(t, map[string]string{"sofarpc-mcp": "v1.0.0"})
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
	stubVersions(t, map[string]string{"sofarpc-mcp": "v1.0.0"})

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
	}
	for _, c := range cases {
		got, usable := compareSemver(c.a, c.b)
		if usable != c.usable || (usable && got != c.want) {
			t.Fatalf("compareSemver(%q,%q)=(%d,%v), want (%d,%v)", c.a, c.b, got, usable, c.want, c.usable)
		}
	}
}
