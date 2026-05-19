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

const modulePath = "github.com/diandian921/sofarpc-cli/cli"

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
