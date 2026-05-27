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

func TestAdaptServiceTypeIsInterface(t *testing.T) {
	src := []byte(`package com.x.facade;
public interface AssetFacade {}`)
	cu, _ := javaparser.Parse(src, "AssetFacade.java")
	_, types := adaptCompilationUnit(cu, "AssetFacade.java", src, nil, nil)
	fqn := "com.x.facade.AssetFacade"
	schema, ok := types[fqn]
	if !ok {
		t.Fatalf("types = %v, want %s", types, fqn)
	}
	if schema.Kind != "interface" {
		t.Errorf("Kind = %q, want interface", schema.Kind)
	}
}

func TestAdaptServiceTypeIsClassWhenNoInterface(t *testing.T) {
	src := []byte(`package com.x.dto;
public class AssetDTO {}`)
	cu, _ := javaparser.Parse(src, "AssetDTO.java")
	_, types := adaptCompilationUnit(cu, "AssetDTO.java", src, nil, nil)
	schema, ok := types["com.x.dto.AssetDTO"]
	if !ok {
		t.Fatalf("types = %v", types)
	}
	if schema.Kind != "class" {
		t.Errorf("Kind = %q, want class", schema.Kind)
	}
}

func TestAdaptInterfaceWithMultipleTypesPicksInterfaceFirst(t *testing.T) {
	src := []byte(`package p;
class FirstHelper {}
interface PrimaryFacade {}
class LastHelper {}`)
	cu, _ := javaparser.Parse(src, "T.java")
	_, types := adaptCompilationUnit(cu, "T.java", src, nil, nil)
	for _, name := range []string{"FirstHelper", "PrimaryFacade", "LastHelper"} {
		if _, ok := types["p."+name]; !ok {
			t.Errorf("missing type %s", name)
		}
	}
}

func TestAdaptTypeSchemaTypeParams(t *testing.T) {
	src := []byte(`package com.x.dto;
public class Page<T, K extends Number> {}`)
	cu, _ := javaparser.Parse(src, "Page.java")
	_, types := adaptCompilationUnit(cu, "Page.java", src, nil, nil)
	page := types["com.x.dto.Page"]
	if len(page.TypeParams) != 2 || page.TypeParams[0] != "T" || page.TypeParams[1] != "K" {
		t.Errorf("Page.TypeParams = %v, want [T, K]", page.TypeParams)
	}
}

func TestAdaptNestedTypesFlatKeying(t *testing.T) {
	src := []byte(`package p;
class Outer {
	class Inner {}
	enum Status {}
}`)
	cu, _ := javaparser.Parse(src, "T.java")
	_, types := adaptCompilationUnit(cu, "T.java", src, nil, nil)
	for _, name := range []string{"Outer", "Inner", "Status"} {
		if _, ok := types["p."+name]; !ok {
			t.Errorf("missing flat-keyed nested type p.%s; got %v", name, types)
		}
	}
}

func TestAdaptAnnotationDeclarationSkipped(t *testing.T) {
	src := []byte(`package p;
public @interface Marker {}
public class Real {}`)
	cu, _ := javaparser.Parse(src, "T.java")
	_, types := adaptCompilationUnit(cu, "T.java", src, nil, nil)
	if _, ok := types["p.Marker"]; ok {
		t.Errorf("Marker(@interface) should be skipped: %v", types)
	}
	if _, ok := types["p.Real"]; !ok {
		t.Errorf("Real should be present: %v", types)
	}
}

func TestAdaptInterfaceMethodsBasic(t *testing.T) {
	src := []byte(`package com.x.facade;

import com.x.dto.AssetDTO;
import com.x.dto.AssetQuery;
import java.util.List;
import java.util.Map;

public interface AssetFacade {
    /** 查询资产 */
    List<AssetDTO> query(AssetQuery req);
    Map<String, List<Long>> findFilters(String key, int limit);
}`)
	cu, _ := javaparser.Parse(src, "AssetFacade.java")
	methods, _ := adaptCompilationUnit(cu, "AssetFacade.java", src, []string{"com.x.facade."}, nil)
	if len(methods) != 2 {
		t.Fatalf("methods = %v", methods)
	}
	m0 := methods[0]
	if m0.Method != "query" || m0.ReturnType != "List<AssetDTO>" {
		t.Errorf("query method = %+v", m0)
	}
	if len(m0.Parameters) != 1 || m0.Parameters[0].Name != "req" || m0.Parameters[0].Type != "AssetQuery" {
		t.Errorf("query params = %+v", m0.Parameters)
	}
	if m0.Summary != "查询资产" {
		t.Errorf("query summary = %q", m0.Summary)
	}
	if m0.Service != "com.x.facade.AssetFacade" || m0.Interface != "AssetFacade" || m0.Package != "com.x.facade" {
		t.Errorf("metadata = %+v", m0)
	}
	if m0.OutOfPrefix {
		t.Errorf("OutOfPrefix should be false for matching prefix")
	}
	if m0.SourceHash == "" || len(m0.SourceHash) != 16 {
		t.Errorf("SourceHash = %q, want 16-char hex", m0.SourceHash)
	}
	if m0.Imports["AssetDTO"] != "com.x.dto.AssetDTO" {
		t.Errorf("imports[AssetDTO] = %q", m0.Imports["AssetDTO"])
	}

	m1 := methods[1]
	if m1.ReturnType != "Map<String, List<Long>>" {
		t.Errorf("findFilters.ReturnType = %q", m1.ReturnType)
	}
	if len(m1.Parameters) != 2 || m1.Parameters[1].Name != "limit" || m1.Parameters[1].Type != "int" {
		t.Errorf("findFilters params = %+v", m1.Parameters)
	}
}

func TestAdaptMethodTypeParams(t *testing.T) {
	src := []byte(`package p;
public interface Foo {
	<T, K extends Number> Page<T> query(T req, K key);
}`)
	cu, _ := javaparser.Parse(src, "Foo.java")
	methods, _ := adaptCompilationUnit(cu, "Foo.java", src, nil, nil)
	if len(methods) != 1 {
		t.Fatalf("methods = %v", methods)
	}
	if len(methods[0].TypeParams) != 2 || methods[0].TypeParams[0] != "T" || methods[0].TypeParams[1] != "K" {
		t.Errorf("TypeParams = %v, want [T, K]", methods[0].TypeParams)
	}
}

func TestAdaptMethodInheritsServiceTypeParams(t *testing.T) {
	src := []byte(`package p;
public interface Facade<T, K> {
	T get(K key);
	<X> X cast(K input);
}`)
	cu, _ := javaparser.Parse(src, "Facade.java")
	methods, _ := adaptCompilationUnit(cu, "Facade.java", src, nil, nil)
	if len(methods) != 2 {
		t.Fatalf("methods = %v", methods)
	}
	if !sliceEq(methods[0].TypeParams, []string{"T", "K"}) {
		t.Errorf("get.TypeParams = %v, want [T, K] (inherited from service)", methods[0].TypeParams)
	}
	if !sliceEq(methods[1].TypeParams, []string{"T", "K", "X"}) {
		t.Errorf("cast.TypeParams = %v, want [T, K, X] (service ++ method)", methods[1].TypeParams)
	}
}

func TestMergeTypeParamsDedup(t *testing.T) {
	got := mergeTypeParams([]string{"T", "K"}, []string{"T", "X"})
	want := []string{"T", "K", "X"}
	if !sliceEq(got, want) {
		t.Errorf("mergeTypeParams dedup = %v, want %v", got, want)
	}
	if mergeTypeParams(nil, nil) != nil {
		t.Errorf("nil+nil should return nil for JSON omitempty")
	}
}

func sliceEq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestAdaptClassDoesNotEmitMethods(t *testing.T) {
	src := []byte(`package p;
public class Helper {
	public String greet() { return "hi"; }
}`)
	cu, _ := javaparser.Parse(src, "Helper.java")
	methods, types := adaptCompilationUnit(cu, "Helper.java", src, nil, nil)
	if methods != nil {
		t.Errorf("class methods should be nil, got %v", methods)
	}
	if _, ok := types["p.Helper"]; !ok {
		t.Errorf("Helper class TypeSchema missing")
	}
}

func TestAdaptOutOfPrefix(t *testing.T) {
	src := []byte(`package com.other.facade;
public interface OtherFacade {
	void noop();
}`)
	cu, _ := javaparser.Parse(src, "OtherFacade.java")
	methods, _ := adaptCompilationUnit(cu, "OtherFacade.java", src, []string{"com.x.facade."}, nil)
	if len(methods) != 1 {
		t.Fatalf("methods = %v", methods)
	}
	if !methods[0].OutOfPrefix {
		t.Errorf("OutOfPrefix should be true for non-matching prefix")
	}
}

func TestAdaptInterfaceMethodSkipsCtor(t *testing.T) {
	src := []byte(`package p;
public interface Foo {
	String hello();
	default boolean ping() { return true; }
}`)
	cu, _ := javaparser.Parse(src, "Foo.java")
	methods, _ := adaptCompilationUnit(cu, "Foo.java", src, nil, nil)
	if len(methods) != 2 {
		t.Fatalf("methods = %v", methods)
	}
}
