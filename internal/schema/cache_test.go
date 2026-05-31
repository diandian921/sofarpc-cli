package schema

import (
	"path/filepath"
	"testing"
	"time"
)

// TestLoadOrBuildIndexRebuildsStaleV4Cache pins the specific v4->v5 transition:
// a v4 cache predates TypeSchema.Extends, so with an unchanged source (matching
// fingerprint) only the version bump can force a rebuild. It complements the
// version-agnostic TestLoadOrBuildIndexIgnoresOldCacheVersion by asserting the
// rebuilt index actually carries Extends (the reason for the bump) and guards
// against reverting indexCacheVersion back to "4".
func TestLoadOrBuildIndexRebuildsStaleV4Cache(t *testing.T) {
	t.Setenv("SOFARPC_HOME", t.TempDir())
	root := filepath.Join("testdata", "golden", "inherit")
	project := Project{
		Name:            "inherit-v4",
		WorkspaceRoot:   root,
		ServicePrefixes: []string{"com.acme.inherit.facade."},
	}

	path, err := CachePath(project)
	if err != nil {
		t.Fatalf("CachePath: %v", err)
	}
	fp, err := SourceFingerprint(root)
	if err != nil {
		t.Fatalf("SourceFingerprint: %v", err)
	}
	if err := writeCache(path, cacheFile{
		Project:           project,
		SchemaVersion:     "4",
		SourceFingerprint: fp,
		Index:             &Index{Project: project},
		LastAccessedAt:    time.Now().Unix(),
	}); err != nil {
		t.Fatalf("writeCache: %v", err)
	}

	idx, err := LoadOrBuildIndex(project)
	if err != nil {
		t.Fatalf("LoadOrBuildIndex: %v", err)
	}
	order, ok := idx.Types["com.acme.inherit.dto.OrderDTO"]
	if !ok || len(order.Extends) == 0 {
		t.Fatalf("v4 cache must rebuild into a v5 index carrying Extends; got %#v", order)
	}
	reread, err := readCache(path)
	if err != nil {
		t.Fatalf("readCache: %v", err)
	}
	if reread.SchemaVersion != indexCacheVersion {
		t.Fatalf("rewritten cache version = %q, want %q", reread.SchemaVersion, indexCacheVersion)
	}
}
