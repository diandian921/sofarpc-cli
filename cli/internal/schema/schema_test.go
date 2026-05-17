package schema

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverSourceRoots(t *testing.T) {
	root := t.TempDir()
	mkdir(t, filepath.Join(root, "src", "main", "java"))
	mkdir(t, filepath.Join(root, "order-service", "src", "main", "java"))
	mkdir(t, filepath.Join(root, "order-service", "src", "test", "java"))
	mkdir(t, filepath.Join(root, "target", "src", "main", "java"))

	roots, err := DiscoverSourceRoots(root)
	if err != nil {
		t.Fatalf("DiscoverSourceRoots: %v", err)
	}
	if len(roots) != 2 {
		t.Fatalf("roots = %#v, want 2", roots)
	}
}

func TestTokenizeIncludesCJKBigram(t *testing.T) {
	tokens := Tokenize("查询用户 getUser")
	if !contains(tokens, "查询") || !contains(tokens, "用户") || !contains(tokens, "get") || !contains(tokens, "user") {
		t.Fatalf("tokens missing expected values: %#v", tokens)
	}
}

func TestSearchAndDescribeService(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src", "main", "java", "com", "example", "user")
	mkdir(t, src)
	write(t, filepath.Join(src, "UserService.java"), `
package com.example.user;

/** 用户服务 */
public interface UserService {
    /** 查询用户 */
    UserDTO getUser(String userId);
}
`)
	write(t, filepath.Join(src, "UserDTO.java"), `
package com.example.user;

public class UserDTO {
    private String id;
    private String name;
}
`)

	idx, err := BuildIndex(Project{Name: "user", WorkspaceRoot: root, ServicePrefixes: []string{"com.example.user."}})
	if err != nil {
		t.Fatalf("BuildIndex: %v", err)
	}
	results := Search(idx, "查询用户", 5, false)
	if len(results) != 1 {
		t.Fatalf("results = %#v", results)
	}
	desc, err := Describe(idx, "com.example.user.UserService", "getUser")
	if err != nil {
		t.Fatalf("Describe: %v", err)
	}
	if len(desc.Methods) != 1 {
		t.Fatalf("methods = %#v", desc.Methods)
	}
	if typ, ok := desc.Types["com.example.user.UserDTO"]; !ok || len(typ.Fields) != 2 {
		t.Fatalf("missing DTO fields: %#v", desc.Types)
	}
}

func TestLoadOrBuildIndexWritesCache(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	root := t.TempDir()
	src := filepath.Join(root, "src", "main", "java", "com", "example")
	mkdir(t, src)
	write(t, filepath.Join(src, "UserService.java"), `
package com.example;
public interface UserService {
    String getUser(String userId);
}
`)

	project := Project{Name: "user", WorkspaceRoot: root, ServicePrefixes: []string{"com.example."}}
	idx, err := LoadOrBuildIndex(project)
	if err != nil {
		t.Fatalf("LoadOrBuildIndex: %v", err)
	}
	if len(idx.Methods) != 1 {
		t.Fatalf("methods = %#v", idx.Methods)
	}
	path, err := CachePath(project)
	if err != nil {
		t.Fatalf("CachePath: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("cache not written: %v", err)
	}
}

func TestLoadOrBuildIndexIgnoresOldCacheVersion(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	root := t.TempDir()
	src := filepath.Join(root, "src", "main", "java", "com", "example")
	mkdir(t, src)
	write(t, filepath.Join(src, "UserService.java"), `
package com.example;
public interface UserService {
    String getUser(String userId);
}
`)

	project := Project{Name: "user", WorkspaceRoot: root, ServicePrefixes: []string{"com.example."}}
	fingerprint, err := SourceFingerprint(root)
	if err != nil {
		t.Fatalf("SourceFingerprint: %v", err)
	}
	path, err := CachePath(project)
	if err != nil {
		t.Fatalf("CachePath: %v", err)
	}
	if err := writeCache(path, cacheFile{
		Project:           project,
		SchemaVersion:     "old",
		SourceFingerprint: fingerprint,
		Index:             &Index{Project: project},
	}); err != nil {
		t.Fatalf("writeCache: %v", err)
	}

	idx, err := LoadOrBuildIndex(project)
	if err != nil {
		t.Fatalf("LoadOrBuildIndex: %v", err)
	}
	if len(idx.Methods) != 1 {
		t.Fatalf("stale cache was reused: %#v", idx.Methods)
	}
}

func TestLoadOrBuildIndexPreservesMethodImports(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	root := t.TempDir()
	src := filepath.Join(root, "src", "main", "java", "com", "example", "api")
	model := filepath.Join(root, "src", "main", "java", "com", "example", "model")
	mkdir(t, src)
	mkdir(t, model)
	write(t, filepath.Join(src, "UserService.java"), `
package com.example.api;

import com.example.model.UserRequest;

public interface UserService {
    String getUser(UserRequest request);
}
`)
	write(t, filepath.Join(model, "UserRequest.java"), `
package com.example.model;
public class UserRequest {
    private Long id;
}
`)

	project := Project{Name: "user", WorkspaceRoot: root, ServicePrefixes: []string{"com.example.api."}}
	if _, err := LoadOrBuildIndex(project); err != nil {
		t.Fatalf("first LoadOrBuildIndex: %v", err)
	}
	idx, err := LoadOrBuildIndex(project)
	if err != nil {
		t.Fatalf("second LoadOrBuildIndex: %v", err)
	}
	if len(idx.Methods) != 1 {
		t.Fatalf("methods = %#v", idx.Methods)
	}
	if got := idx.Methods[0].Imports["UserRequest"]; got != "com.example.model.UserRequest" {
		t.Fatalf("cached import = %q", got)
	}
}

func TestBuildIndexDoesNotExposeClassPrivateMethodsAsServiceCandidates(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src", "main", "java", "com", "example")
	mkdir(t, src)
	write(t, filepath.Join(src, "UserServiceImpl.java"), `
package com.example;
public class UserServiceImpl {
    private String getUser(String userId) {
        return userId;
    }
}
`)

	idx, err := BuildIndex(Project{Name: "user", WorkspaceRoot: root, ServicePrefixes: []string{"com.example."}})
	if err != nil {
		t.Fatalf("BuildIndex: %v", err)
	}
	if len(idx.Methods) != 0 {
		t.Fatalf("class methods should not be service candidates: %#v", idx.Methods)
	}
}

func TestParseFieldsIgnoresReturnStatements(t *testing.T) {
	fields := parseFields(`
public class OperationResult<T> {
    private T data;
    public T getData() {
        return data;
    }
}
`)
	if len(fields) != 1 || fields[0].Name != "data" {
		t.Fatalf("fields = %#v", fields)
	}
}

func mkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
}

func write(t *testing.T, path string, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func contains(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
