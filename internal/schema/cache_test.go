package schema

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
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

func TestLoadOrBuildIndexRebuildsStaleV6CacheForMemberRecovery(t *testing.T) {
	t.Setenv("SOFARPC_HOME", t.TempDir())
	workspace := t.TempDir()
	mustWriteFile(t, filepath.Join(workspace, "src/main/java/com/x/RecoverFacade.java"), `package com.x;
public interface RecoverFacade {
	Outer<String>.Inner<Integer> unsupported();
	String good();
}`)
	project := Project{Name: "recover-v6", WorkspaceRoot: workspace, ServicePrefixes: []string{"com.x."}}
	path, err := CachePath(project)
	if err != nil {
		t.Fatalf("CachePath: %v", err)
	}
	fingerprint, err := SourceFingerprint(workspace)
	if err != nil {
		t.Fatalf("SourceFingerprint: %v", err)
	}
	if err := writeCache(path, cacheFile{
		Project:           project,
		SchemaVersion:     "6",
		SourceFingerprint: fingerprint,
		Index:             &Index{Project: project, Types: map[string]TypeSchema{}},
		LastAccessedAt:    time.Now().Unix(),
	}); err != nil {
		t.Fatalf("writeCache: %v", err)
	}

	idx, err := LoadOrBuildIndex(project)
	if err != nil {
		t.Fatalf("LoadOrBuildIndex: %v", err)
	}
	if len(idx.Methods) != 1 || idx.Methods[0].Method != "good" || len(idx.Warnings) != 1 {
		t.Fatalf("v6 cache was not rebuilt with member recovery: methods=%+v warnings=%+v", idx.Methods, idx.Warnings)
	}
	reread, err := readCache(path)
	if err != nil {
		t.Fatalf("readCache: %v", err)
	}
	if reread.SchemaVersion != "7" {
		t.Fatalf("rewritten cache version = %q, want 7", reread.SchemaVersion)
	}
}

func TestLoadOrBuildIndexConcurrentWritersKeepValidCache(t *testing.T) {
	t.Setenv("SOFARPC_HOME", t.TempDir())
	project := Project{
		Name: "concurrent", WorkspaceRoot: filepath.Join("testdata", "golden", "inherit"),
		ServicePrefixes: []string{"com.acme.inherit.facade."},
	}
	const workers = 12
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			idx, err := LoadOrBuildIndex(project)
			if err == nil && len(idx.Methods) == 0 {
				err = fmt.Errorf("empty index")
			}
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent cache: %v", err)
		}
	}
	path, _ := CachePath(project)
	if _, err := readCache(path); err != nil {
		t.Fatalf("final cache invalid: %v", err)
	}
}

func TestCleanupUnusedUsesCacheModTime(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SOFARPC_HOME", home)
	project := Project{Name: "stale", WorkspaceRoot: filepath.Join("testdata", "golden", "inherit")}
	path, err := CachePath(project)
	if err != nil {
		t.Fatalf("CachePath: %v", err)
	}
	if err := writeCache(path, cacheFile{Project: project, SchemaVersion: indexCacheVersion, Index: &Index{Project: project}}); err != nil {
		t.Fatalf("writeCache: %v", err)
	}
	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}
	if err := CleanupUnused(24 * time.Hour); err != nil {
		t.Fatalf("CleanupUnused: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("stale cache still exists: %v", err)
	}
}
