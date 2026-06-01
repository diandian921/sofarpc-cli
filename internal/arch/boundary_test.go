package arch

import (
	"bytes"
	"encoding/json"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const modulePath = "github.com/diandian921/sofarpc-mcp"

func TestPackageBoundaries(t *testing.T) {
	rules := []struct {
		pkg       string
		forbidden []string
	}{
		{
			pkg: modulePath + "/internal/direct",
			forbidden: []string{
				modulePath + "/internal/app",
				modulePath + "/internal/cli",
				modulePath + "/internal/mcp",
				modulePath + "/internal/presentation",
			},
		},
		{
			pkg: modulePath + "/internal/schema",
			forbidden: []string{
				modulePath + "/internal/app",
				modulePath + "/internal/cli",
				modulePath + "/internal/direct",
				modulePath + "/internal/mcp",
				modulePath + "/internal/presentation",
			},
		},
		{
			pkg: modulePath + "/internal/presentation",
			forbidden: []string{
				modulePath + "/internal/app",
				modulePath + "/internal/cli",
				modulePath + "/internal/direct",
				modulePath + "/internal/mcp",
				modulePath + "/internal/schema",
			},
		},
		{
			pkg: modulePath + "/internal/app",
			forbidden: []string{
				modulePath + "/internal/cli",
				modulePath + "/internal/mcp",
			},
		},
	}

	for _, rule := range rules {
		t.Run(shortPackageName(rule.pkg), func(t *testing.T) {
			deps := packageDeps(t, rule.pkg)
			for _, forbidden := range rule.forbidden {
				for dep := range deps {
					if samePackageOrChild(dep, forbidden) {
						t.Fatalf("%s must not depend on %s; found dependency %s", rule.pkg, forbidden, dep)
					}
				}
			}
		})
	}
}

// packageExists reports whether go list can load pkg. It returns false before a
// package is created, so the MCP layering rules below can be committed ahead of
// the packages they govern and activate automatically as each lands.
func packageExists(pkg string) bool {
	cmd := exec.Command("go", "list", pkg)
	cmd.Dir = filepath.Join("..", "..")
	return cmd.Run() == nil
}

// TestMCPLayerBoundaries enforces the three-layer dependency direction
// cli/mcp facade -> tools -> server -> proto. proto depends only on stdlib;
// server never imports tools or app (transitively). tools never imports proto
// *directly* (it reaches progress/logging through the server.Runtime interface);
// the transitive tools -> server -> proto edge is expected and allowed.
func TestMCPLayerBoundaries(t *testing.T) {
	rules := []struct {
		pkg        string
		forbidden  []string
		directOnly bool
	}{
		{
			pkg: modulePath + "/internal/mcp/proto",
			forbidden: []string{
				modulePath + "/internal/app",
				modulePath + "/internal/mcp/server",
				modulePath + "/internal/mcp/tools",
				modulePath + "/internal/cli",
				modulePath + "/internal/schema",
				modulePath + "/internal/appconfig",
				modulePath + "/internal/direct",
			},
		},
		{
			pkg: modulePath + "/internal/mcp/server",
			forbidden: []string{
				modulePath + "/internal/mcp/tools",
				modulePath + "/internal/app",
			},
		},
		{
			pkg: modulePath + "/internal/mcp/tools",
			forbidden: []string{
				modulePath + "/internal/mcp/proto",
			},
			directOnly: true,
		},
	}
	for _, rule := range rules {
		t.Run(shortPackageName(rule.pkg), func(t *testing.T) {
			if !packageExists(rule.pkg) {
				t.Skipf("%s not created yet", rule.pkg)
			}
			deps := packageDeps(t, rule.pkg)
			if rule.directOnly {
				deps = directImports(t, rule.pkg)
			}
			for _, forbidden := range rule.forbidden {
				for dep := range deps {
					if samePackageOrChild(dep, forbidden) {
						t.Fatalf("%s must not depend on %s; found dependency %s", rule.pkg, forbidden, dep)
					}
				}
			}
		})
	}
}

// directImports returns only the packages pkg imports directly (not the full
// transitive closure), used where a transitive edge through a lower layer is
// legitimate but a direct import is not.
func directImports(t *testing.T, pkg string) map[string]bool {
	t.Helper()
	cmd := exec.Command("go", "list", "-f", "{{range .Imports}}{{.}}\n{{end}}", pkg)
	cmd.Dir = filepath.Join("..", "..")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go list imports for %s: %v\n%s", pkg, err, output)
	}
	deps := map[string]bool{}
	for _, line := range strings.Split(string(output), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			deps[line] = true
		}
	}
	return deps
}

func packageDeps(t *testing.T, pkg string) map[string]bool {
	t.Helper()
	cmd := exec.Command("go", "list", "-deps", "-json", pkg)
	cmd.Dir = filepath.Join("..", "..")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go list deps for %s: %v\n%s", pkg, err, output)
	}
	dec := json.NewDecoder(bytes.NewReader(output))
	deps := map[string]bool{}
	for {
		var item struct {
			ImportPath string
		}
		err := dec.Decode(&item)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("decode go list output for %s: %v", pkg, err)
		}
		if item.ImportPath != "" && item.ImportPath != pkg {
			deps[item.ImportPath] = true
		}
	}
	return deps
}

func samePackageOrChild(path, prefix string) bool {
	return path == prefix || strings.HasPrefix(path, prefix+"/")
}

func shortPackageName(pkg string) string {
	return strings.TrimPrefix(pkg, modulePath+"/")
}
