package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type hostCall struct {
	name string
	args []string
}

func stubHost(t *testing.T, responder func(name string, args []string) (string, string, int, error)) *[]hostCall {
	t.Helper()
	prev := hostExec
	calls := &[]hostCall{}
	hostExec = func(name string, args ...string) (string, string, int, error) {
		*calls = append(*calls, hostCall{name: name, args: args})
		return responder(name, args)
	}
	t.Cleanup(func() { hostExec = prev })
	return calls
}

// stubPreflight makes the binary-layer check pass; tests that exercise the
// preflight path override it explicitly.
func stubPreflight(t *testing.T) {
	t.Helper()
	prev := mcpPreflight
	mcpPreflight = func(string) error { return nil }
	t.Cleanup(func() { mcpPreflight = prev })
}

func runSetupCmd(t *testing.T, root string, args ...string) (int, string, string) {
	t.Helper()
	t.Setenv("SOFARPC_HOME", root)
	stubPreflight(t)
	var out, errBuf bytes.Buffer
	code := runSetup(args, Env{Stdout: &out, Stderr: &errBuf})
	return code, out.String(), errBuf.String()
}

func TestSetupAbortsWhenBinaryMissing(t *testing.T) {
	root := t.TempDir()
	t.Setenv("SOFARPC_HOME", root)
	calls := stubHost(t, func(string, []string) (string, string, int, error) {
		return "", "", 0, nil
	})
	// No stubPreflight: the real check runs against a root with no binary.
	var out, errBuf bytes.Buffer
	code := runSetup([]string{"codex"}, Env{Stdout: &out, Stderr: &errBuf})
	if code == 0 {
		t.Fatal("setup must abort when sofarpc-mcp is missing")
	}
	if !strings.Contains(errBuf.String(), "binary check failed") {
		t.Fatalf("want binary check failure, got: %s", errBuf.String())
	}
	for _, c := range *calls {
		if isAdd(c) || isRemove(c) {
			t.Fatalf("must not touch host config when binary check fails: %+v", c)
		}
	}
}

func TestSetupPreflightRunsSelftest(t *testing.T) {
	root := t.TempDir()
	bin := filepath.Join(root, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	cmdPath := filepath.Join(bin, "sofarpc-mcp")
	if err := os.WriteFile(cmdPath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	var selftested bool
	prev := hostExec
	hostExec = func(name string, args ...string) (string, string, int, error) {
		if name == cmdPath && len(args) == 1 && args[0] == "--selftest" {
			selftested = true
			return "ok", "", 0, nil
		}
		if len(args) >= 2 && args[1] == "get" {
			return "", "", 1, nil
		}
		return "", "", 0, nil
	}
	t.Cleanup(func() { hostExec = prev })
	t.Setenv("SOFARPC_HOME", root)
	var out, errBuf bytes.Buffer
	code := runSetup([]string{"codex"}, Env{Stdout: &out, Stderr: &errBuf})
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, errBuf.String())
	}
	if !selftested {
		t.Fatal("setup must run <command> --selftest as the binary-layer check")
	}
}

func isGet(c hostCall) bool {
	return len(c.args) >= 2 && c.args[0] == "mcp" && c.args[1] == "get"
}
func isAdd(c hostCall) bool {
	return len(c.args) >= 2 && c.args[0] == "mcp" && c.args[1] == "add"
}
func isRemove(c hostCall) bool {
	return len(c.args) >= 2 && c.args[0] == "mcp" && c.args[1] == "remove"
}

func TestSetupCodexAddsWhenAbsent(t *testing.T) {
	root := t.TempDir()
	calls := stubHost(t, func(_ string, args []string) (string, string, int, error) {
		if len(args) >= 2 && args[1] == "get" {
			return "", "not found", 1, nil
		}
		return "", "", 0, nil
	})
	code, out, errOut := runSetupCmd(t, root, "codex")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, errOut)
	}
	var added bool
	for _, c := range *calls {
		if isAdd(c) {
			added = true
			joined := strings.Join(c.args, " ")
			want := filepath.Join(root, "bin", "sofarpc-mcp")
			if !strings.Contains(joined, want) {
				t.Fatalf("add must register expanded path %q, got: %s", want, joined)
			}
			if strings.Contains(joined, "~") {
				t.Fatalf("registered command must not contain ~: %s", joined)
			}
		}
	}
	if !added {
		t.Fatalf("expected codex mcp add to be called; out=%s", out)
	}
}

func TestSetupCodexNoopWhenMatching(t *testing.T) {
	root := t.TempDir()
	command := filepath.Join(root, "bin", "sofarpc-mcp")
	// SOFARPC_HOME is the temp root (non-default), so a truly-matching
	// entry must also carry the env. Nested under "transport" and in env_vars
	// form to prove the structured walk is not bound to a flat schema.
	calls := stubHost(t, func(_ string, args []string) (string, string, int, error) {
		if args[1] == "get" {
			return `{"transport":{"command":"` + command + `","env_vars":[{"key":"SOFARPC_HOME","value":"` + root + `"}]}}`, "", 0, nil
		}
		return "", "", 0, nil
	})
	code, out, errOut := runSetupCmd(t, root, "codex")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, errOut)
	}
	for _, c := range *calls {
		if isAdd(c) || isRemove(c) {
			t.Fatalf("matching entry must be a no-op, but mutation called: %+v", c)
		}
	}
	if !strings.Contains(out, "no change") {
		t.Fatalf("expected no-change message, got: %s", out)
	}
}

func TestSetupCodexDiffRequiresForce(t *testing.T) {
	root := t.TempDir()
	calls := stubHost(t, func(_ string, args []string) (string, string, int, error) {
		if args[1] == "get" {
			return `{"command":"/somewhere/else/sofarpc-mcp"}`, "", 0, nil
		}
		return "", "", 0, nil
	})
	code, _, errOut := runSetupCmd(t, root, "codex")
	if code == 0 {
		t.Fatal("differing entry without --force must fail")
	}
	if !strings.Contains(errOut, "--force") {
		t.Fatalf("want hint about --force, got: %s", errOut)
	}
	for _, c := range *calls {
		if isAdd(c) || isRemove(c) {
			t.Fatalf("must not mutate without --force: %+v", c)
		}
	}
}

func TestSetupCodexInvalidJSONRequiresForce(t *testing.T) {
	root := t.TempDir()
	command := filepath.Join(root, "bin", "sofarpc-mcp")
	calls := stubHost(t, func(_ string, args []string) (string, string, int, error) {
		if args[1] == "get" {
			return "human text mentions " + command, "", 0, nil
		}
		return "", "", 0, nil
	})
	code, _, errOut := runSetupCmd(t, root, "codex")
	if code == 0 {
		t.Fatal("invalid codex JSON must not be treated as a match")
	}
	if !strings.Contains(errOut, "--force") {
		t.Fatalf("want hint about --force, got: %s", errOut)
	}
	for _, c := range *calls {
		if isAdd(c) || isRemove(c) {
			t.Fatalf("must not mutate when codex JSON is not machine-readable: %+v", c)
		}
	}
}

func TestSetupCodexForceReplaces(t *testing.T) {
	root := t.TempDir()
	calls := stubHost(t, func(_ string, args []string) (string, string, int, error) {
		if args[1] == "get" {
			return `{"command":"/old/sofarpc-mcp"}`, "", 0, nil
		}
		return "", "", 0, nil
	})
	code, _, errOut := runSetupCmd(t, root, "codex", "--force")
	if code != 0 {
		t.Fatalf("force replace exit=%d stderr=%s", code, errOut)
	}
	var removed, added bool
	for _, c := range *calls {
		if isRemove(c) {
			removed = true
		}
		if isAdd(c) {
			added = true
		}
	}
	if !removed || !added {
		t.Fatalf("force must remove then add (removed=%v added=%v)", removed, added)
	}
}

func TestSetupCodexForceStopsWhenRemoveFails(t *testing.T) {
	root := t.TempDir()
	calls := stubHost(t, func(_ string, args []string) (string, string, int, error) {
		if args[1] == "get" {
			return `{"command":"/old/sofarpc-mcp"}`, "", 0, nil
		}
		if args[1] == "remove" {
			return "", "permission denied", 1, nil
		}
		return "", "", 0, nil
	})
	code, _, errOut := runSetupCmd(t, root, "codex", "--force")
	if code == 0 {
		t.Fatal("force replace must fail when remove exits non-zero")
	}
	if !strings.Contains(errOut, "remove existing") || !strings.Contains(errOut, "permission denied") {
		t.Fatalf("want remove failure detail, got: %s", errOut)
	}
	for _, c := range *calls {
		if isAdd(c) {
			t.Fatalf("must not add when remove failed: %+v", c)
		}
	}
}

func TestSetupClaudePresentRequiresForce(t *testing.T) {
	root := t.TempDir()
	stubHost(t, func(_ string, args []string) (string, string, int, error) {
		if args[1] == "get" {
			return "sofarpc: stdio server", "", 0, nil
		}
		return "", "", 0, nil
	})
	code, _, errOut := runSetupCmd(t, root, "claude")
	if code == 0 {
		t.Fatal("claude present without --force must fail (no JSON, cannot confirm equality)")
	}
	if !strings.Contains(errOut, "no JSON") {
		t.Fatalf("want explanation that claude has no JSON form, got: %s", errOut)
	}
}

func TestSetupClaudeAddsWhenAbsentWithScope(t *testing.T) {
	root := t.TempDir()
	calls := stubHost(t, func(_ string, args []string) (string, string, int, error) {
		if args[1] == "get" {
			return "", "No MCP server found", 1, nil
		}
		return "", "", 0, nil
	})
	code, _, errOut := runSetupCmd(t, root, "claude", "--claude-scope", "user")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, errOut)
	}
	var ok bool
	for _, c := range *calls {
		if isAdd(c) && strings.Contains(strings.Join(c.args, " "), "--scope user") {
			ok = true
		}
	}
	if !ok {
		t.Fatal("claude add must include --scope user")
	}
}

func TestSetupRejectsInvalidClaudeScopeBeforeMutation(t *testing.T) {
	root := t.TempDir()
	calls := stubHost(t, func(_ string, _ []string) (string, string, int, error) {
		return "", "", 0, nil
	})
	code, _, errOut := runSetupCmd(t, root, "claude", "--claude-scope", "workspace")
	if code != 2 {
		t.Fatalf("invalid claude scope exit=%d, want 2; stderr=%s", code, errOut)
	}
	if !strings.Contains(errOut, "invalid --claude-scope") {
		t.Fatalf("want local scope validation error, got: %s", errOut)
	}
	if len(*calls) != 0 {
		t.Fatalf("invalid scope must not call host CLI, got: %+v", *calls)
	}
}

func TestSetupAllRejectsInvalidClaudeScopeBeforeCodexMutation(t *testing.T) {
	root := t.TempDir()
	calls := stubHost(t, func(_ string, _ []string) (string, string, int, error) {
		return "", "", 0, nil
	})
	code, _, errOut := runSetupCmd(t, root, "all", "--claude-scope", "workspace")
	if code != 2 {
		t.Fatalf("invalid claude scope exit=%d, want 2; stderr=%s", code, errOut)
	}
	if len(*calls) != 0 {
		t.Fatalf("invalid claude scope under all must not mutate codex either, got: %+v", *calls)
	}
}

func TestSetupDryRunMutatesNothing(t *testing.T) {
	root := t.TempDir()
	calls := stubHost(t, func(_ string, _ []string) (string, string, int, error) {
		return "", "", 0, nil
	})
	code, out, errOut := runSetupCmd(t, root, "all", "--dry-run")
	if code != 0 {
		t.Fatalf("dry-run exit=%d stderr=%s", code, errOut)
	}
	if len(*calls) != 0 {
		t.Fatalf("dry-run must not invoke host CLI, got calls: %+v", *calls)
	}
	if !strings.Contains(out, "[dry-run] claude") || !strings.Contains(out, "[dry-run] codex") {
		t.Fatalf("dry-run must print planned commands for both hosts, got: %s", out)
	}
}

func TestSetupMissingHostCLIPrintsManualSnippet(t *testing.T) {
	root := t.TempDir()
	stubHost(t, func(_ string, _ []string) (string, string, int, error) {
		return "", "", 0, execNotFound{}
	})
	code, _, errOut := runSetupCmd(t, root, "codex")
	if code == 0 {
		t.Fatal("missing host CLI must fail")
	}
	if !strings.Contains(errOut, "Register manually") {
		t.Fatalf("want manual snippet, got: %s", errOut)
	}
	if !strings.Contains(errOut, "--env SOFARPC_HOME="+root) {
		t.Fatalf("manual snippet must include SOFARPC_HOME env, got: %s", errOut)
	}
	if strings.Contains(errOut, "also set env") {
		t.Fatalf("manual snippet should be directly copy-pasteable, got: %s", errOut)
	}
}

func TestSetupMissingHostCLIQuotesManualSnippet(t *testing.T) {
	root := filepath.Join(t.TempDir(), "home with space")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	stubHost(t, func(_ string, _ []string) (string, string, int, error) {
		return "", "", 0, execNotFound{}
	})
	code, _, errOut := runSetupCmd(t, root, "codex")
	if code == 0 {
		t.Fatal("missing host CLI must fail")
	}
	if !strings.Contains(errOut, "'SOFARPC_HOME="+root+"'") {
		t.Fatalf("manual snippet must quote SOFARPC_HOME with spaces, got: %s", errOut)
	}
	if !strings.Contains(errOut, "'"+filepath.Join(root, "bin", "sofarpc-mcp")+"'") {
		t.Fatalf("manual snippet must quote command path with spaces, got: %s", errOut)
	}
}

type execNotFound struct{}

func (execNotFound) Error() string { return "exec: \"codex\": executable file not found in $PATH" }
