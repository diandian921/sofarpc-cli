package schema

import (
	"os"
	"path/filepath"
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

func TestAdaptClassFields(t *testing.T) {
	src := []byte(`package p;
public class Asset {
	private Long id;
	public String name = "default";
	protected final java.util.List<String> tags;
	private static final int CONST = 1;
}`)
	cu, _ := javaparser.Parse(src, "Asset.java")
	_, types := adaptCompilationUnit(cu, "Asset.java", src, nil, nil)
	asset := types["p.Asset"]
	if asset.Type == "" {
		t.Fatalf("missing Asset schema: %v", types)
	}
	// CONST is `static final` and must be excluded — Hessian serializes only
	// instance, non-transient fields. `tags` is `final` (not static) and stays.
	wantFields := map[string]string{
		"id":   "Long",
		"name": "String",
		"tags": "java.util.List<String>",
	}
	if len(asset.Fields) != len(wantFields) {
		t.Fatalf("Fields = %+v, want %d entries (static CONST excluded)", asset.Fields, len(wantFields))
	}
	for _, f := range asset.Fields {
		if f.Name == "CONST" {
			t.Errorf("static field CONST must be excluded (Hessian skips static)")
		}
		if want, ok := wantFields[f.Name]; !ok || f.Type != want {
			t.Errorf("Field %s = %q, want %q", f.Name, f.Type, want)
		}
	}
}

func TestAdaptEnumValues(t *testing.T) {
	src := []byte(`package p;
public enum Status {
	ACTIVE("a"),
	INACTIVE("i");
	private final String code;
	Status(String code) { this.code = code; }
}`)
	cu, _ := javaparser.Parse(src, "Status.java")
	_, types := adaptCompilationUnit(cu, "Status.java", src, nil, nil)
	status := types["p.Status"]
	if status.Kind != "enum" {
		t.Fatalf("Kind = %q", status.Kind)
	}
	wantValues := []string{"ACTIVE", "INACTIVE"}
	if len(status.EnumValues) != len(wantValues) {
		t.Fatalf("EnumValues = %v, want %v", status.EnumValues, wantValues)
	}
	for i, v := range wantValues {
		if status.EnumValues[i] != v {
			t.Errorf("EnumValues[%d] = %q, want %q", i, status.EnumValues[i], v)
		}
	}
	if len(status.Fields) != 1 || status.Fields[0].Name != "code" || status.Fields[0].Type != "String" {
		t.Errorf("enum Fields = %+v, want [{code, String}]", status.Fields)
	}
}

func TestAdaptRecordComponents(t *testing.T) {
	src := []byte(`package p;
public record Point(int x, int y, java.util.List<String> tags) {}`)
	cu, _ := javaparser.Parse(src, "Point.java")
	_, types := adaptCompilationUnit(cu, "Point.java", src, nil, nil)
	point := types["p.Point"]
	if point.Kind != "record" {
		t.Fatalf("Kind = %q", point.Kind)
	}
	wantFields := []Field{
		{Name: "x", Type: "int"},
		{Name: "y", Type: "int"},
		{Name: "tags", Type: "java.util.List<String>"},
	}
	if len(point.Fields) != len(wantFields) {
		t.Fatalf("Fields = %+v, want %v", point.Fields, wantFields)
	}
	for i, w := range wantFields {
		if point.Fields[i] != w {
			t.Errorf("Fields[%d] = %+v, want %+v", i, point.Fields[i], w)
		}
	}
}

func TestAdaptInterfaceTypeSchemaHasNoFields(t *testing.T) {
	src := []byte(`package p;
public interface Foo {
	int VERSION = 1;
	String hello();
}`)
	cu, _ := javaparser.Parse(src, "Foo.java")
	_, types := adaptCompilationUnit(cu, "Foo.java", src, nil, nil)
	foo := types["p.Foo"]
	if len(foo.Fields) != 1 || foo.Fields[0].Name != "VERSION" {
		t.Errorf("interface constants Fields = %+v", foo.Fields)
	}
}

func TestAdaptMultiDeclFieldsExpanded(t *testing.T) {
	src := []byte(`package p;
public class Bag {
	protected final long a = 1L, b, c = 3L;
}`)
	cu, _ := javaparser.Parse(src, "Bag.java")
	_, types := adaptCompilationUnit(cu, "Bag.java", src, nil, nil)
	bag := types["p.Bag"]
	if len(bag.Fields) != 3 {
		t.Fatalf("Fields = %+v, want 3 (multi-decl)", bag.Fields)
	}
	names := []string{bag.Fields[0].Name, bag.Fields[1].Name, bag.Fields[2].Name}
	want := []string{"a", "b", "c"}
	for i, w := range want {
		if names[i] != w {
			t.Errorf("Fields[%d].Name = %q, want %q", i, names[i], w)
		}
	}
}

func TestAdaptPackagePrivateFieldsIncluded(t *testing.T) {
	src := []byte(`package p;
public class Bag {
	String packagePrivate;
	public String pub;
}`)
	cu, _ := javaparser.Parse(src, "Bag.java")
	_, types := adaptCompilationUnit(cu, "Bag.java", src, nil, nil)
	bag := types["p.Bag"]
	if len(bag.Fields) != 2 {
		t.Fatalf("Fields = %+v, want 2 (package-private 也算)", bag.Fields)
	}
	names := []string{bag.Fields[0].Name, bag.Fields[1].Name}
	want := []string{"packagePrivate", "pub"}
	for i, w := range want {
		if names[i] != w {
			t.Errorf("Fields[%d].Name = %q, want %q", i, names[i], w)
		}
	}
}

func TestAdaptGenericRecordHeader(t *testing.T) {
	src := []byte(`package p;
public record Page<T>(int total, java.util.List<T> records) {}`)
	cu, _ := javaparser.Parse(src, "Page.java")
	_, types := adaptCompilationUnit(cu, "Page.java", src, nil, nil)
	page := types["p.Page"]
	if !sliceEq(page.TypeParams, []string{"T"}) {
		t.Errorf("Page.TypeParams = %v, want [T]", page.TypeParams)
	}
	if len(page.Fields) != 2 {
		t.Fatalf("Fields = %+v, want 2 (record components)", page.Fields)
	}
	if page.Fields[0].Name != "total" || page.Fields[0].Type != "int" {
		t.Errorf("Fields[0] = %+v", page.Fields[0])
	}
	if page.Fields[1].Name != "records" || page.Fields[1].Type != "java.util.List<T>" {
		t.Errorf("Fields[1] = %+v", page.Fields[1])
	}
}

func TestAdaptGenericClassFields(t *testing.T) {
	src := []byte(`package p;
public class Page<T, K> {
	private java.util.List<T> records;
	private K key;
}`)
	cu, _ := javaparser.Parse(src, "Page.java")
	_, types := adaptCompilationUnit(cu, "Page.java", src, nil, nil)
	page := types["p.Page"]
	if !sliceEq(page.TypeParams, []string{"T", "K"}) {
		t.Errorf("Page.TypeParams = %v, want [T, K]", page.TypeParams)
	}
	if page.Fields[0].Type != "java.util.List<T>" {
		t.Errorf("Fields[0].Type = %q, want java.util.List<T>", page.Fields[0].Type)
	}
	if page.Fields[1].Type != "K" {
		t.Errorf("Fields[1].Type = %q, want K", page.Fields[1].Type)
	}
}

func TestBuildIndexWildcardImportExpansion(t *testing.T) {
	tmp := t.TempDir()
	mustWriteFile(t, filepath.Join(tmp, "src/main/java/com/x/facade/MyFacade.java"), `package com.x.facade;
import com.x.dto.*;
public interface MyFacade {
	MyResp query(MyReq req);
}`)
	mustWriteFile(t, filepath.Join(tmp, "src/main/java/com/x/dto/MyReq.java"), `package com.x.dto;
public class MyReq {
	public String key;
}`)
	mustWriteFile(t, filepath.Join(tmp, "src/main/java/com/x/dto/MyResp.java"), `package com.x.dto;
public class MyResp {
	public String value;
}`)

	idx, err := BuildIndex(Project{Name: "wild", WorkspaceRoot: tmp, ServicePrefixes: []string{"com.x.facade."}})
	if err != nil {
		t.Fatalf("BuildIndex: %v", err)
	}

	var facadeMethod Method
	for _, m := range idx.Methods {
		if m.Service == "com.x.facade.MyFacade" && m.Method == "query" {
			facadeMethod = m
			break
		}
	}
	if facadeMethod.Method == "" {
		t.Fatalf("query method not found in index: %+v", idx.Methods)
	}
	if facadeMethod.Imports["MyReq"] != "com.x.dto.MyReq" {
		t.Errorf("MyReq import not expanded from wildcard: imports = %v", facadeMethod.Imports)
	}
	if facadeMethod.Imports["MyResp"] != "com.x.dto.MyResp" {
		t.Errorf("MyResp import not expanded from wildcard: imports = %v", facadeMethod.Imports)
	}

	desc, err := Describe(idx, "com.x.facade.MyFacade", "query")
	if err != nil {
		t.Fatalf("Describe: %v", err)
	}
	req := desc.Types["com.x.dto.MyReq"]
	if req.Type == "" {
		t.Fatalf("MyReq schema missing in desc.Types = %v", desc.Types)
	}
	if len(req.Fields) != 1 || req.Fields[0].Name != "key" {
		t.Errorf("MyReq.Fields = %+v", req.Fields)
	}
}

// mustWriteFile 是 test helper:写文件 + 父目录 mkdir。
func mustWriteFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func TestBuildIndexSilentlySkipsMalformedFiles(t *testing.T) {
	tmp := t.TempDir()
	mustWriteFile(t, filepath.Join(tmp, "src/main/java/com/x/Good.java"), `package com.x;
public class Good { public String name; }`)
	mustWriteFile(t, filepath.Join(tmp, "src/main/java/com/x/Broken.java"), `package com.x;
public class Broken {
	// 未闭合 string —— lexer 会报错
	String bad = "no close`)

	idx, err := BuildIndex(Project{Name: "skip", WorkspaceRoot: tmp})
	if err != nil {
		t.Fatalf("BuildIndex: %v (should silently skip malformed)", err)
	}
	if _, ok := idx.Types["com.x.Good"]; !ok {
		t.Errorf("Good.java should still be indexed: %+v", idx.Types)
	}
	if _, ok := idx.Types["com.x.Broken"]; ok {
		t.Errorf("Broken.java should be silently skipped (no schema), got %+v", idx.Types["com.x.Broken"])
	}
}
