package launcher

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureTokenCreatesAndReusesToken(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "token")

	first, err := EnsureToken(path)
	if err != nil {
		t.Fatalf("EnsureToken first: %v", err)
	}
	second, err := EnsureToken(path)
	if err != nil {
		t.Fatalf("EnsureToken second: %v", err)
	}
	if first == "" || first != second {
		t.Fatalf("token should be stable, first=%q second=%q", first, second)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat token: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("token mode = %v, want 0600", got)
	}
}
