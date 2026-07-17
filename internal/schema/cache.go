package schema

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/diandian921/sofarpc-mcp/internal/appconfig"
)

type cacheFile struct {
	Project           Project `json:"project"`
	SchemaVersion     string  `json:"schemaVersion,omitempty"`
	SourceFingerprint string  `json:"sourceFingerprint"`
	Index             *Index  `json:"index"`
	LastAccessedAt    int64   `json:"lastAccessedAt"`
}

// indexCacheVersion 注意:每次 Method / TypeSchema struct 字段变化都要 bump,
// 旧 cache 反序列化时无法填充新字段,LoadOrBuildIndex 会强制重建。
// "7":javaparser member recovery 让旧版会整文件跳过的源码产生 partial schema,
// 即使 source fingerprint 不变也必须重建 v6 cache。
const indexCacheVersion = "7"

func LoadOrBuildIndex(project Project) (*Index, error) {
	fingerprint, err := SourceFingerprint(project.WorkspaceRoot)
	if err != nil {
		return nil, err
	}
	path, err := CachePath(project)
	if err != nil {
		return nil, err
	}
	var result *Index
	err = appconfig.WithFileLock(cacheLockPath(path), func() error {
		if cached, readErr := readCache(path); readErr == nil && cached.SchemaVersion == indexCacheVersion && cached.SourceFingerprint == fingerprint && cached.Index != nil {
			now := time.Now()
			_ = os.Chtimes(path, now, now)
			result = cached.Index
			return nil
		}
		idx, buildErr := BuildIndex(project)
		if buildErr != nil {
			return buildErr
		}
		if writeErr := writeCache(path, cacheFile{
			Project:           project,
			SchemaVersion:     indexCacheVersion,
			SourceFingerprint: fingerprint,
			Index:             idx,
			LastAccessedAt:    time.Now().Unix(),
		}); writeErr != nil {
			return writeErr
		}
		result = idx
		return nil
	})
	return result, err
}

func CleanupUnused(maxAge time.Duration) error {
	home, err := appconfig.Home()
	if err != nil {
		return err
	}
	root := filepath.Join(home, "cache", "schema", "projects")
	cutoff := time.Now().Add(-maxAge)
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(root, entry.Name(), "index.json")
		if err := appconfig.WithFileLock(cacheLockPath(path), func() error {
			info, statErr := os.Stat(path)
			if os.IsNotExist(statErr) {
				return nil
			}
			if statErr != nil {
				return statErr
			}
			if info.ModTime().Before(cutoff) {
				return os.RemoveAll(filepath.Dir(path))
			}
			return nil
		}); err != nil {
			return err
		}
	}
	return nil
}

func CachePath(project Project) (string, error) {
	home, err := appconfig.Home()
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256([]byte(project.WorkspaceRoot))
	workspaceHash := hex.EncodeToString(hash[:])[:12]
	name := sanitizeCacheSegment(project.Name)
	if name == "" {
		name = "project"
	}
	return filepath.Join(home, "cache", "schema", "projects", name+"-"+workspaceHash, "index.json"), nil
}

// sanitizeCacheSegment keeps a project name usable as a single path segment so a
// name containing "/", "\" or ".." cannot escape the cache projects directory.
// Uniqueness is preserved by the workspace-hash suffix appended by the caller.
func sanitizeCacheSegment(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}

func SourceFingerprint(workspace string) (string, error) {
	roots, err := DiscoverSourceRoots(workspace)
	if err != nil {
		return "", err
	}
	var rows []string
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				if shouldIgnoreDir(d.Name()) {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".java") {
				return nil
			}
			body, err := os.ReadFile(path)
			if err != nil {
				return nil
			}
			fileHash := sha256.Sum256(body)
			rel, _ := filepath.Rel(workspace, path)
			rows = append(rows, rel+"|"+hex.EncodeToString(fileHash[:]))
			return nil
		})
		if err != nil {
			return "", err
		}
	}
	sort.Strings(rows)
	hash := sha256.Sum256([]byte(strings.Join(rows, "\n")))
	return hex.EncodeToString(hash[:]), nil
}

func readCache(path string) (cacheFile, error) {
	var cached cacheFile
	body, err := os.ReadFile(path)
	if err != nil {
		return cached, err
	}
	err = json.Unmarshal(body, &cached)
	return cached, err
}

func writeCache(path string, cached cacheFile) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	body, err := json.MarshalIndent(cached, "", "  ")
	if err != nil {
		return err
	}
	body = append(body, '\n')
	tmp, err := os.CreateTemp(filepath.Dir(path), ".index-*.json")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }
	if _, err := tmp.Write(body); err != nil {
		_ = tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Chmod(0o644); err != nil {
		_ = tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return err
	}
	if runtime.GOOS == "windows" {
		_ = os.Remove(path)
	}
	if err := os.Rename(tmpName, path); err != nil {
		cleanup()
		return err
	}
	return nil
}

func cacheLockPath(indexPath string) string {
	projectDir := filepath.Dir(indexPath)
	projectsRoot := filepath.Dir(projectDir)
	return filepath.Join(projectsRoot, ".locks", filepath.Base(projectDir)+".lock")
}
