package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runInstallCmd(t *testing.T, root string, args ...string) (int, string, string, *[]hostCall) {
	t.Helper()
	src := newFakeSource(t)
	stubExecutable(t, src.sofarpc)
	stubVersions(t, nil)
	t.Setenv("SOFARPC_HOME", root)
	stubPreflight(t)
	calls := stubHost(t, func(_ string, args []string) (string, string, int, error) {
		// Default: get returns "not found" (code 1), add succeeds.
		if len(args) >= 2 && args[1] == "get" {
			return "", "", 1, nil
		}
		return "", "", 0, nil
	})
	var out, errBuf bytes.Buffer
	code := runInstall(args, Env{BuildVersion: "v1.0.0", Stdout: &out, Stderr: &errBuf})
	return code, out.String(), errBuf.String(), calls
}

func TestInstallNoHostJustSelfInstalls(t *testing.T) {
	root := t.TempDir()
	code, out, errOut, calls := runInstallCmd(t, root)
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, errOut)
	}
	if _, err := os.Stat(filepath.Join(root, "bin", "sofarpc")); err != nil {
		t.Fatalf("self-install must run when no host given: %v", err)
	}
	for _, c := range *calls {
		if c.name == "claude" || c.name == "codex" {
			t.Fatalf("must not contact any host CLI when no host given: %+v", c)
		}
	}
	if !strings.Contains(out, "register the MCP server") {
		t.Fatalf("must hint the next step, got: %s", out)
	}
}

func TestInstallWithHostChainsSetup(t *testing.T) {
	root := t.TempDir()
	code, _, errOut, calls := runInstallCmd(t, root, "codex")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, errOut)
	}
	if _, err := os.Stat(filepath.Join(root, "bin", "sofarpc")); err != nil {
		t.Fatalf("install must self-install first: %v", err)
	}
	var addedCodex bool
	for _, c := range *calls {
		if c.name == "codex" && isAdd(c) {
			addedCodex = true
		}
		if c.name == "claude" {
			t.Fatalf("install codex must not touch claude: %+v", c)
		}
	}
	if !addedCodex {
		t.Fatal("install codex must trigger codex mcp add")
	}
}

func TestInstallAllRegistersBothHosts(t *testing.T) {
	root := t.TempDir()
	code, _, errOut, calls := runInstallCmd(t, root, "all")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, errOut)
	}
	hosts := map[string]bool{}
	for _, c := range *calls {
		if isAdd(c) {
			hosts[c.name] = true
		}
	}
	if !hosts["claude"] || !hosts["codex"] {
		t.Fatalf("install all must add to both hosts, got: %+v", hosts)
	}
}

func TestInstallRejectsUnknownHost(t *testing.T) {
	root := t.TempDir()
	code, _, errOut, calls := runInstallCmd(t, root, "bogus")
	if code != 2 {
		t.Fatalf("unknown host must exit 2, got %d", code)
	}
	if !strings.Contains(errOut, "unknown host") {
		t.Fatalf("want unknown host error, got: %s", errOut)
	}
	if _, err := os.Stat(filepath.Join(root, "bin", "sofarpc")); err == nil {
		t.Fatal("must not self-install when host is invalid")
	}
	for _, c := range *calls {
		if isAdd(c) || isRemove(c) {
			t.Fatalf("must not touch host CLI on invalid arg: %+v", c)
		}
	}
}

func TestInstallStopsWhenSelfInstallFails(t *testing.T) {
	// Make self-install fail by pointing executable at a non-existent path
	// so the file copy step errors. We bypass runInstallCmd's stub to do this.
	stubVersions(t, nil)
	prev := executablePath
	executablePath = func() (string, error) { return "/no/such/path/sofarpc", nil }
	t.Cleanup(func() { executablePath = prev })
	stubPreflight(t)
	root := t.TempDir()
	t.Setenv("SOFARPC_HOME", root)
	calls := stubHost(t, func(string, []string) (string, string, int, error) {
		return "", "", 0, nil
	})
	var out, errBuf bytes.Buffer
	code := runInstall([]string{"codex"}, Env{BuildVersion: "v1.0.0", Stdout: &out, Stderr: &errBuf})
	if code == 0 {
		t.Fatal("must propagate self-install failure")
	}
	for _, c := range *calls {
		if isAdd(c) || isRemove(c) {
			t.Fatalf("must not run setup when self-install fails: %+v", c)
		}
	}
}
