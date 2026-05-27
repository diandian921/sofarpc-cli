package schema

import (
	"testing"

	"github.com/diandian921/sofarpc-cli/internal/javaparser"
)

func TestAdaptEmptyReturnsNil(t *testing.T) {
	cu, _ := javaparser.Parse([]byte(""), "Empty.java")
	methods, types := adaptCompilationUnit(cu, "Empty.java", []byte(""), nil, nil)
	if methods != nil {
		t.Errorf("methods = %v, want nil", methods)
	}
	if types != nil {
		t.Errorf("types = %v, want nil", types)
	}
}

func TestAdaptDefaultPackageReturnsNil(t *testing.T) {
	src := `class Loose {}`
	cu, _ := javaparser.Parse([]byte(src), "Loose.java")
	methods, types := adaptCompilationUnit(cu, "Loose.java", []byte(src), nil, nil)
	if methods != nil || types != nil {
		t.Errorf("methods=%v types=%v want both nil (no package)", methods, types)
	}
}

func TestExtractImportsRegular(t *testing.T) {
	imports := []javaparser.ImportDecl{
		{Path: "com.acme.dto.Asset"},
		{Path: "java.util.List"},
	}
	out := extractImports(imports, nil)
	want := map[string]string{
		"Asset": "com.acme.dto.Asset",
		"List":  "java.util.List",
	}
	if len(out) != len(want) {
		t.Fatalf("imports = %v, want %v", out, want)
	}
	for k, v := range want {
		if out[k] != v {
			t.Errorf("imports[%q] = %q, want %q", k, out[k], v)
		}
	}
}

func TestExtractImportsSkipsStatic(t *testing.T) {
	imports := []javaparser.ImportDecl{
		{Path: "com.acme.util.Helpers.format", Static: true},
		{Path: "com.acme.util.Constants", Static: true, Wildcard: true},
		{Path: "com.acme.dto.Asset"},
	}
	out := extractImports(imports, nil)
	if _, ok := out["format"]; ok {
		t.Errorf("static import `format` should NOT be in imports map: %v", out)
	}
	if _, ok := out["Constants"]; ok {
		t.Errorf("static wildcard prefix `Constants` should NOT be in imports: %v", out)
	}
	if out["Asset"] != "com.acme.dto.Asset" {
		t.Errorf("non-static `Asset` missing: %v", out)
	}
}

func TestExtractImportsStaticDoesNotShadowWildcard(t *testing.T) {
	topFQNs := map[string]bool{
		"com.acme.dto.FOO": true,
	}
	imports := []javaparser.ImportDecl{
		{Path: "com.acme.util.Helpers.FOO", Static: true},
		{Path: "com.acme.dto", Wildcard: true},
	}
	out := extractImports(imports, topFQNs)
	if out["FOO"] != "com.acme.dto.FOO" {
		t.Errorf("static 不应 shadow wildcard 同名 type:imports[FOO] = %q", out["FOO"])
	}
}

func TestExtractImportsWildcardExpansion(t *testing.T) {
	topFQNs := map[string]bool{
		"com.acme.dto.Asset":      true,
		"com.acme.dto.AssetQuery": true,
		"com.acme.dto.AssetTag":   true,
		"com.acme.other.Outside":  true,
	}
	imports := []javaparser.ImportDecl{
		{Path: "com.acme.dto", Wildcard: true},
	}
	out := extractImports(imports, topFQNs)
	want := map[string]string{
		"Asset":      "com.acme.dto.Asset",
		"AssetQuery": "com.acme.dto.AssetQuery",
		"AssetTag":   "com.acme.dto.AssetTag",
	}
	if len(out) != len(want) {
		t.Fatalf("wildcard expanded imports = %v, want %v", out, want)
	}
	for k, v := range want {
		if out[k] != v {
			t.Errorf("imports[%q] = %q, want %q", k, out[k], v)
		}
	}
}

func TestExtractImportsWildcardSkipsNested(t *testing.T) {
	topFQNs := map[string]bool{
		"com.acme.dto.Outer": true,
	}
	imports := []javaparser.ImportDecl{{Path: "com.acme.dto", Wildcard: true}}
	out := extractImports(imports, topFQNs)
	if _, ok := out["Inner"]; ok {
		t.Errorf("nested 不应被 wildcard 展开:out = %v", out)
	}
	if out["Outer"] != "com.acme.dto.Outer" {
		t.Errorf("Outer 缺失:out = %v", out)
	}
}

func TestExtractImportsExplicitWinsOverWildcard(t *testing.T) {
	topFQNs := map[string]bool{
		"com.acme.dto.AssetQuery":   true,
		"com.acme.other.AssetQuery": true,
	}
	imports := []javaparser.ImportDecl{
		{Path: "com.acme.dto", Wildcard: true},
		{Path: "com.acme.other.AssetQuery"},
	}
	out := extractImports(imports, topFQNs)
	if out["AssetQuery"] != "com.acme.other.AssetQuery" {
		t.Errorf("explicit import should win, got %q (out=%v)", out["AssetQuery"], out)
	}
}

func TestExtractImportsWildcardDeterministic(t *testing.T) {
	topFQNs := map[string]bool{
		"com.acme.dto.B": true,
		"com.acme.dto.A": true,
		"com.acme.dto.C": true,
	}
	imports := []javaparser.ImportDecl{{Path: "com.acme.dto", Wildcard: true}}
	for i := 0; i < 20; i++ {
		out := extractImports(imports, topFQNs)
		if out["A"] != "com.acme.dto.A" || out["B"] != "com.acme.dto.B" || out["C"] != "com.acme.dto.C" {
			t.Fatalf("nondeterministic wildcard expansion iter %d: %v", i, out)
		}
	}
}

func TestCollectTypeFQNsRecursive(t *testing.T) {
	cu, err := javaparser.Parse([]byte(`package p;
class Outer {
	class Inner {
		class Deep {}
	}
	enum E {}
}
interface Top2 {}`), "T.java")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	allDst := map[string]bool{}
	topDst := map[string]bool{}
	collectTypeFQNs(cu.Package.Name, cu.Types, allDst, topDst)

	wantAll := []string{"p.Outer", "p.Inner", "p.Deep", "p.E", "p.Top2"}
	for _, w := range wantAll {
		if !allDst[w] {
			t.Errorf("missing %q in allDst = %v", w, allDst)
		}
	}
	if len(allDst) != len(wantAll) {
		t.Errorf("allDst size = %d, want %d (%v)", len(allDst), len(wantAll), allDst)
	}

	wantTop := []string{"p.Outer", "p.Top2"}
	for _, w := range wantTop {
		if !topDst[w] {
			t.Errorf("missing %q in topDst = %v", w, topDst)
		}
	}
	if len(topDst) != len(wantTop) {
		t.Errorf("topDst size = %d, want %d (%v)", len(topDst), len(wantTop), topDst)
	}
}
