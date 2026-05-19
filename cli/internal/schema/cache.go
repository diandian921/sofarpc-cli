package schema

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/diandian921/sofarpc-cli/cli/internal/appconfig"
)

type cacheFile struct {
	Project           Project `json:"project"`
	SchemaVersion     string  `json:"schemaVersion,omitempty"`
	SourceFingerprint string  `json:"sourceFingerprint"`
	Index             *Index  `json:"index"`
	LastAccessedAt    int64   `json:"lastAccessedAt"`
}

const indexCacheVersion = "3"

func LoadOrBuildIndex(project Project) (*Index, error) {
	fingerprint, err := SourceFingerprint(project.WorkspaceRoot)
	if err != nil {
		return nil, err
	}
	path, err := CachePath(project)
	if err != nil {
		return nil, err
	}
	if cached, err := readCache(path); err == nil && cached.SchemaVersion == indexCacheVersion && cached.SourceFingerprint == fingerprint && cached.Index != nil {
		cached.LastAccessedAt = time.Now().Unix()
		_ = writeCache(path, cached)
		return cached.Index, nil
	}
	idx, err := BuildIndex(project)
	if err != nil {
		return nil, err
	}
	_ = writeCache(path, cacheFile{
		Project:           project,
		SchemaVersion:     indexCacheVersion,
		SourceFingerprint: fingerprint,
		Index:             idx,
		LastAccessedAt:    time.Now().Unix(),
	})
	return idx, nil
}

func CleanupUnused(maxAge time.Duration) error {
	home, err := appconfig.Home()
	if err != nil {
		return err
	}
	root := filepath.Join(home, "cache", "schema", "projects")
	cutoff := time.Now().Add(-maxAge).Unix()
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
		cached, err := readCache(path)
		if err != nil {
			continue
		}
		if cached.LastAccessedAt > 0 && cached.LastAccessedAt < cutoff {
			_ = os.RemoveAll(filepath.Dir(path))
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
	name := project.Name
	if name == "" {
		name = "project"
	}
	return filepath.Join(home, "cache", "schema", "projects", name+"-"+workspaceHash, "index.json"), nil
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
	return os.WriteFile(path, body, 0o644)
}
