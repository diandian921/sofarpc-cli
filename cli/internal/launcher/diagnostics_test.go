package launcher

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestResolveJarPathMissingExplicitPathReturnsDiagnostic(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.jar")

	_, err := ResolveJarPath(missing)
	diag, ok := AsDiagnostic(err)
	if !ok {
		t.Fatalf("expected diagnostic error, got %T: %v", err, err)
	}
	if diag.Reason != ReasonEngineJarNotFound {
		t.Fatalf("reason = %q, want %q", diag.Reason, ReasonEngineJarNotFound)
	}
	if got := diag.Details["jarPath"]; got != missing {
		t.Fatalf("jarPath detail = %v, want %s", got, missing)
	}
}

func TestValidateJavaMissingBinaryReturnsDiagnostic(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing-java")

	err := ValidateJava(missing)
	diag, ok := AsDiagnostic(err)
	if !ok {
		t.Fatalf("expected diagnostic error, got %T: %v", err, err)
	}
	if diag.Reason != ReasonJavaNotFound {
		t.Fatalf("reason = %q, want %q", diag.Reason, ReasonJavaNotFound)
	}
	if got := diag.Details["javaBin"]; got != missing {
		t.Fatalf("javaBin detail = %v, want %s", got, missing)
	}
}

func TestTailFileReturnsTail(t *testing.T) {
	path := filepath.Join(t.TempDir(), "engine.log")
	if err := os.WriteFile(path, []byte("0123456789"), 0o644); err != nil {
		t.Fatalf("write log: %v", err)
	}

	tail, err := TailFile(path, 4)
	if err != nil {
		t.Fatalf("TailFile: %v", err)
	}
	if tail != "6789" {
		t.Fatalf("tail = %q, want 6789", tail)
	}
}

func TestWaitForReadyTimeoutReturnsDiagnosticWithLogTail(t *testing.T) {
	dir := t.TempDir()
	logFile := filepath.Join(dir, "daemon.log")
	if err := os.WriteFile(logFile, []byte("boot failed\nstack trace\n"), 0o644); err != nil {
		t.Fatalf("write log: %v", err)
	}

	cfg := Config{
		Paths: Paths{
			BaseDir:   dir,
			StateFile: filepath.Join(dir, "state.json"),
			LockFile:  filepath.Join(dir, "daemon.lock"),
			LogFile:   logFile,
		},
		SpawnBudget:  5 * time.Millisecond,
		PollInterval: time.Millisecond,
	}

	_, err := waitForReady(cfg)
	diag, ok := AsDiagnostic(err)
	if !ok {
		t.Fatalf("expected diagnostic error, got %T: %v", err, err)
	}
	if diag.Reason != ReasonEngineStartTimeout {
		t.Fatalf("reason = %q, want %q", diag.Reason, ReasonEngineStartTimeout)
	}
	if !errors.Is(err, diag.Cause) {
		t.Fatalf("diagnostic should unwrap its cause")
	}
	tail, _ := diag.Details["logTail"].(string)
	if !strings.Contains(tail, "boot failed") {
		t.Fatalf("logTail missing expected content: %q", tail)
	}
}
