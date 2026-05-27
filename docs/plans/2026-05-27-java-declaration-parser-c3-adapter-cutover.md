# Java Declaration Parser — C.3 Adapter + Cutover Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把 [[c1-lexer]] + [[c2-ast]] 已 ship 的 `internal/javaparser` 接入 `internal/schema`,替换现有 regex parser。 顺带:(1)wildcard import 根治(`import a.b.*` 2-pass 展开);(2)[[rpc-types-generic-preservation]] P3 edge case 根治(`Method.TypeParams` / `TypeSchema.TypeParams` 精确识别 declared type variable);(3)删除老 regex 代码。 `BuildIndex / Search / Describe` 对外签名不变,所有下游(`internal/app/*`、`internal/mcp/server.go`)零修改。

**Architecture:**
- 新建 `internal/schema/adapter_javaparser.go`:`adaptCompilationUnit(cu, path, body, prefixes, allTypeFQNs) → []Method + map[string]TypeSchema`,把 javaparser AST 转成 schema 包既有 string-based 表达
- `internal/schema/schema.go::BuildIndex` 改成 2-pass:Pass 1 walk + parse 所有 .java → 收集全工程 type FQN 集合;Pass 2 in-memory 调 adapter,wildcard import 用 Pass 1 的 FQN 集合展开
- `schema.Method` 新加 `TypeParams []string`(declared type parameters,如 `<T, K>` → `["T", "K"]`),`TypeSchema` 同样加 `TypeParams []string`
- `internal/app/rpc_types.go` 的 `resolveBaseType` / `rpcParamTypeForMethod` / `rpcFieldTypeForType` 在 pkg fallback 之前加精确 `TypeParams` 查表 —— `class Page<T>` 同 package 真有 `com.x.dto.T` 类时正确按 type variable 处理,根治 Plan B 文档化的 P3 limitation
- Cache schemaVersion 从 `"3"` 升 `"4"`(新加 fields 之后旧缓存反序列化无法填充 TypeParams,需失效重建)
- 嵌套 type 沿用现有 flat keying(`pkg.Inner` 而非 `pkg.Outer.Inner`),保持 `resolveType` 行为不变;真正的 nested FQN 表达留作 follow-up

**为什么 cutover 整体放 1 份 plan:** adapter / wildcard 处理 / P3 fix / 删除老代码这 4 件事都依赖 `schema.Method` 字段扩展(TypeParams),互相绑死。 拆成独立 plan 中间态难 review。 单 plan 13 task,每 task 一个 commit,失败可单独 revert。

**Scope charter — 这是"adapter + cutover",不是"重新设计 schema 包"。** 既有 `BuildIndex / Search / Describe / LoadOrBuildIndex / SourceFingerprint / CleanupUnused / CachePath / DiscoverSourceRoots` 函数签名全部不动。 既有 `Method / TypeSchema / Field / Parameter / Description / Index / Project` struct 仅做**字段添加**,不改名 / 不删字段(JSON 反序列化向后兼容,旧字段 nil 默认)。 任何要不要扩 schema 包 API 的争论,用"是否帮助 C.3 cutover 落地"裁断。

**Tech Stack:** Go 1.21+,标准库,既有 `testing` 框架。 复用 `internal/javaparser/*`。 **不引入** 任何外部依赖。

---

## File Structure

| 文件 | 操作 | 责任 |
|---|---|---|
| `internal/schema/schema.go` | Modify | (a)`Method` 加 `TypeParams []string`、`TypeSchema` 加 `TypeParams []string`;(b)`BuildIndex` 2-pass 重写;(c)`parseJavaFile` 改成调 adapter;(d)删除老 regex parser 函数 |
| `internal/schema/cache.go` | Modify | `indexCacheVersion` 从 `"3"` 升 `"4"` |
| `internal/schema/adapter_javaparser.go` | Create | `adaptCompilationUnit` + helpers(`collectTypeFQNs / extractImports / typeRefToString / cleanJavadocText`)。 把 javaparser AST 转成 schema struct |
| `internal/schema/adapter_javaparser_test.go` | Create | 单元 test:adapter 各分支(class / interface / enum / record / nested / wildcard)+ wildcard import 展开 |
| `internal/schema/parser_golden_test.go` | Modify | 3 个既有 golden test 跑通(可能更新若干 assertion 接受 new parser 修正)+ 新加 wildcard / static / inner class case |
| `internal/schema/testdata/golden/wildcard/` | Create | 新 fixture:含 wildcard import 的 facade + DTO package |
| `internal/schema/testdata/golden/inner/` | Create | 新 fixture:含 nested type 的 facade + DTO |
| `internal/app/rpc_types.go` | Modify | `resolveBaseType` / `rpcParamTypeForMethod` / `rpcFieldTypeForType` 加 `TypeParams` 精确查表,根治 Plan B P3 |
| `internal/app/rpc_types_test.go` | Modify | 新加 case:`class Page<T>` 同 package 有 `T` schema 时不被误解析为 DTO |

**不动**:`internal/javaparser/*`(C.1 + C.2 工件,稳定)、`internal/app/invoke.go`、`internal/app/types.go`、`internal/mcp/server.go`、`internal/direct/*`、`internal/javavalue/*`、`internal/cli/*` —— 所有下游通过 `schema.Method` / `schema.TypeSchema` struct 字段访问数据,新字段(TypeParams)是 JSON-additive,下游不需要任何修改。

---

## Task 1:`Method.TypeParams` + `TypeSchema.TypeParams` 字段添加 + cache 版本升级

**Files:**
- Modify: `internal/schema/schema.go`(在 Method / TypeSchema struct 加字段)
- Modify: `internal/schema/cache.go`(`indexCacheVersion` `"3"` → `"4"`)
- Modify: `internal/schema/schema_test.go`(新增字段 round-trip test)

- [ ] **Step 1: 加字段到 `Method` 和 `TypeSchema` struct**

把 `internal/schema/schema.go` 里 `Method` struct(约 line 22)加最后一个字段:

```go
type Method struct {
	Service     string            `json:"service"`
	Interface   string            `json:"interface"`
	Package     string            `json:"package"`
	Method      string            `json:"method"`
	ReturnType  string            `json:"returnType"`
	Parameters  []Parameter       `json:"parameters"`
	Summary     string            `json:"summary,omitempty"`
	SourceFile  string            `json:"sourceFile"`
	Score       int               `json:"score,omitempty"`
	Evidence    []string          `json:"evidence,omitempty"`
	OutOfPrefix bool              `json:"outOfPrefix,omitempty"`
	SourceHash  string            `json:"sourceHash,omitempty"`
	Imports     map[string]string `json:"imports,omitempty"`
	// TypeParams 是方法 declared type parameters 的简单名列表(`<T, K extends X>` → ["T", "K"])。
	// rpc_types.go 用它精确识别 type variable,避免把同名 same-pkg class 误判为 DTO(Plan B P3 fix)。
	TypeParams []string `json:"typeParams,omitempty"`
}
```

把 `TypeSchema` struct(约 line 43)同样加:

```go
type TypeSchema struct {
	Type       string            `json:"type"`
	Kind       string            `json:"kind"`
	Fields     []Field           `json:"fields,omitempty"`
	EnumValues []string          `json:"enumValues,omitempty"`
	Unresolved bool              `json:"unresolved,omitempty"`
	SourceFile string            `json:"sourceFile,omitempty"`
	Imports    map[string]string `json:"imports,omitempty"`
	// TypeParams 是 class declared type parameters 的简单名列表(`class Page<T, K>` → ["T", "K"])。
	// rpc_types.go 用它精确识别 type variable(Plan B P3 fix)。
	TypeParams []string `json:"typeParams,omitempty"`
}
```

- [ ] **Step 2: 升级 cache version**

把 `internal/schema/cache.go:24` 的:

```go
const indexCacheVersion = "3"
```

改为:

```go
const indexCacheVersion = "4"
```

注释加一行说明:

```go
// indexCacheVersion 注意:每次 Method / TypeSchema struct 字段变化都要 bump,
// 旧 cache 反序列化时无法填充新字段,LoadOrBuildIndex 会强制重建。
const indexCacheVersion = "4"
```

- [ ] **Step 3: 加 round-trip test 确认 JSON 字段映射正确**

在 `internal/schema/schema_test.go` 末尾追加:

```go
func TestMethodTypeParamsJSONRoundTrip(t *testing.T) {
	original := Method{
		Service:    "com.x.facade.PageFacade",
		Method:     "query",
		TypeParams: []string{"T", "K"},
	}
	body, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(body), `"typeParams":["T","K"]`) {
		t.Errorf("json missing typeParams: %s", body)
	}
	var decoded Method
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(decoded.TypeParams) != 2 || decoded.TypeParams[0] != "T" || decoded.TypeParams[1] != "K" {
		t.Errorf("decoded.TypeParams = %v, want [T, K]", decoded.TypeParams)
	}
}

func TestTypeSchemaTypeParamsJSONRoundTrip(t *testing.T) {
	original := TypeSchema{
		Type:       "com.x.dto.Page",
		Kind:       "class",
		TypeParams: []string{"T"},
	}
	body, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(body), `"typeParams":["T"]`) {
		t.Errorf("json missing typeParams: %s", body)
	}
}

func TestTypeSchemaTypeParamsOmitEmpty(t *testing.T) {
	// TypeParams 为空时 JSON 不应包含字段(omitempty)
	original := TypeSchema{Type: "X", Kind: "class"}
	body, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(body), "typeParams") {
		t.Errorf("empty TypeParams should be omitted: %s", body)
	}
}
```

如果 `internal/schema/schema_test.go` 顶部还没 import `encoding/json` 或 `strings`,加上。

- [ ] **Step 4: 跑测试**

Run: `go test ./internal/schema/ -v -run 'TypeParams'`

Expected: 3 个新 test 全 PASS。 既有 `parser_golden_test.go` 因为依赖 BuildIndex(还没接入 adapter),应该还是按 regex parser 跑,继续 PASS。

```
go test ./internal/schema/ -v
```

Expected: 全 PASS,无 regression。 老 regex parser 此时不输出 TypeParams,字段为 nil(JSON omitempty 不出现),没事。

- [ ] **Step 5: commit**

```bash
git add internal/schema/schema.go internal/schema/cache.go internal/schema/schema_test.go
git commit -m "feat: schema 加 Method/TypeSchema.TypeParams + cache 版本升 4"
```

---

## Task 2:Adapter 骨架 + 烟雾测试

**Files:**
- Create: `internal/schema/adapter_javaparser.go`
- Create: `internal/schema/adapter_javaparser_test.go`

- [ ] **Step 1: 创建 `adapter_javaparser.go` 骨架**

```go
package schema

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"

	"github.com/diandian921/sofarpc-cli/internal/javaparser"
)

// adaptCompilationUnit 把 javaparser AST 转成 schema 包的 Method / TypeSchema 表达。
//
// 输入:
//   - cu: javaparser.Parse 的输出
//   - sourcePath: .java 文件绝对路径(给 Method.SourceFile / TypeSchema.SourceFile 用)
//   - body: 文件原始 bytes(给 Method.SourceHash 用)
//   - prefixes: project.ServicePrefixes,用于 Method.OutOfPrefix 判断
//   - topLevelFQNs: 工程内顶层 type 的 FQN 集合,用于 wildcard import 展开(C.3 BuildIndex Pass 1 收集)。
//     nested 类型不在内 —— Java wildcard import 只导入顶层 type
//
// 返回:
//   - methods: 只有 service type 是 interface 时才有(对齐既有 parseJavaFile 行为)
//   - types: 顶层 + 所有 nested type 的 TypeSchema,key 是 pkg + "." + Name(flat keying,与既有
//     resolveType 兼容,真正 nested FQN 留 follow-up)
//
// 形态保证:
//   - Method.TypeParams 来自 service.TypeParams + MethodDecl.TypeParams 拼接(`interface Facade<T>`
//     里的 method 也能拿到 `T`,根治 codex review #7 找的 service-level type param 漏 case)
//   - TypeSchema.TypeParams 来自 TypeDecl.TypeParams(nested 类不继承 outer's TypeParams,
//     follow-up 再扩 —— Java 语义里 nested non-static 类隐含继承,但 facade DTO 几乎不出)
//   - Method.ReturnType / Parameters[i].Type / Field.Type 全部走 TypeRef.String() → 保留泛型字符串
//
// Task 2 阶段返回空 stub,Task 3+ 逐步填充。
func adaptCompilationUnit(cu *javaparser.CompilationUnit, sourcePath string, body []byte, prefixes []string, topLevelFQNs map[string]bool) ([]Method, map[string]TypeSchema) {
	if cu == nil || cu.Package == nil || len(cu.Types) == 0 {
		return nil, nil
	}
	return nil, nil
}

// extractImports 把 ImportDecl 列表展平成既有 `map[shortName]fqn` 形态。
//
// **Java 语义**:single-type import(`import a.b.C;`)优先级 > wildcard import
// (`import a.b.*;`)。 同名时 explicit 胜。 见 JLS §7.5.1。
//
// **当前 flat keying 下的 wildcard 限制**(codex review #4):因为 C.3 沿用既有 flat keying
// (nested 类型也存成 `pkg.Inner`,丢失 outer 信息),wildcard 不能用 `strings.Contains(".")`
// 过滤 nested。 因此 wildcard 展开只用 `topLevelFQNs`(BuildIndex Pass 1 单独收集的顶层 type 集合),
// 跟 `allTypeFQNs` 解耦。 nested 类型不参与 wildcard 展开 —— 跟 Java 语义一致
// (Java wildcard 只导入 top-level types,nested 要 explicit qualified import)。
//
// **确定性**(codex review #5):map iteration 不确定。 处理顺序:
//  1. explicit non-wildcard imports 先全部写入(后写覆盖前写,但同一个 file 同名重复 import 是编译错,
//     不需要处理冲突)
//  2. wildcard imports 按 import 在源文件中的出现顺序遍历;同一 wildcard 内的匹配 FQN 按字典序排序
//  3. wildcard 写入时**只在 key 还不存在**时设入,避免覆盖 explicit
//
// **Static imports**(`import static a.b.C.foo` / `import static a.b.C.*`)**全部跳过**
// (codex review (round 2) #2):老 `importRE` regex 只匹配非 static import,因此 static import
// 从来没进过 `schema.Imports` 这个 type-resolution 用的 map。 新 adapter 不应改变这层语义 ——
// `import static x.Y.FOO` 若被当 type import 写入 `imports["FOO"]=...`,会 shadow wildcard
// 的同名 type 展开。 Static import 是给 method call 用的,跟 type resolution 无关。
// follow-up:若 C.4 真要支持 static import,在 Method 层加专用 `StaticImports` 字段。
func extractImports(imports []javaparser.ImportDecl, topLevelFQNs map[string]bool) map[string]string {
	out := map[string]string{}

	// Pass A:explicit non-static non-wildcard 优先
	for _, imp := range imports {
		if imp.Static || imp.Wildcard {
			continue
		}
		lastDot := strings.LastIndex(imp.Path, ".")
		if lastDot < 0 {
			continue
		}
		shortName := imp.Path[lastDot+1:]
		if shortName == "" {
			continue
		}
		out[shortName] = imp.Path
	}

	// Pass B:wildcard(只在 key 未占用时填入,匹配 FQN 按字典序保确定)
	for _, imp := range imports {
		if !imp.Wildcard || imp.Static {
			continue
		}
		prefix := imp.Path + "."
		var matches []string
		for fqn := range topLevelFQNs {
			if !strings.HasPrefix(fqn, prefix) {
				continue
			}
			rest := fqn[len(prefix):]
			if rest == "" || strings.Contains(rest, ".") {
				// 防御性:同 package 顶层 type 的 FQN 末段不含点。
				// strings.Contains(".") 这条目前永远不命中(topLevelFQNs 都是 pkg+"."+Name,无嵌套点),
				// 留 guard 是为了未来如果 keying 升级成 `pkg.Outer.Inner` 时不打破假设。
				continue
			}
			matches = append(matches, fqn)
		}
		sort.Strings(matches)
		for _, fqn := range matches {
			shortName := fqn[len(prefix):]
			if _, exists := out[shortName]; exists {
				continue
			}
			out[shortName] = fqn
		}
	}
	return out
}

// collectTypeFQNs 把一个 CompilationUnit 的 type FQN 写进 dst maps。
// 包含顶层 type 与递归的 nested type。 keying 沿用 pkg + "." + Name(flat)。
//
// dstAll  接收所有 type FQN(顶层 + nested),用于 resolveType / Describe 查表
// dstTop  只接收顶层 type FQN,用于 wildcard import 展开(JLS 语义:wildcard 只导入顶层)
// dst 可为 nil(若 caller 不需要其中一份)。
func collectTypeFQNs(pkg string, types []javaparser.TypeDecl, dstAll, dstTop map[string]bool) {
	for _, t := range types {
		fqn := pkg + "." + t.Name
		if dstAll != nil {
			dstAll[fqn] = true
		}
		if dstTop != nil {
			dstTop[fqn] = true
		}
		if len(t.NestedTypes) > 0 {
			// nested 只进 dstAll,不进 dstTop
			collectTypeFQNs(pkg, t.NestedTypes, dstAll, nil)
		}
	}
}

// computeSourceHash 跟既有 parseJavaFile 的 hash 计算保持一致(前 16 chars hex)。
func computeSourceHash(body []byte) string {
	hash := sha256.Sum256(body)
	return hex.EncodeToString(hash[:])[:16]
}
```

- [ ] **Step 2: 创建 `adapter_javaparser_test.go` 烟雾 test**

```go
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
	// 默认 package(没有 package 声明)的 .java 文件,既有 parseJavaFile 直接返回 nil。 adapter 保持。
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
	// codex review (round 2) #2:static import 不应进 type resolution map,会 shadow wildcard 同名 type
	imports := []javaparser.ImportDecl{
		{Path: "com.acme.util.Helpers.format", Static: true},  // 单 type static import → skip
		{Path: "com.acme.util.Constants", Static: true, Wildcard: true},  // static wildcard → skip
		{Path: "com.acme.dto.Asset"},  // 普通,保留
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
	// Static import 的 simple-name 跟 wildcard 同 package 真有的 type 同名时,
	// 不能 shadow 掉 wildcard 展开
	topFQNs := map[string]bool{
		"com.acme.dto.FOO": true,
	}
	imports := []javaparser.ImportDecl{
		{Path: "com.acme.util.Helpers.FOO", Static: true},  // static import 'FOO'
		{Path: "com.acme.dto", Wildcard: true},              // wildcard 也会展开 FOO
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
		"com.acme.other.Outside":  true, // 不同 package,不展开
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
	// `import a.b.*` 只展开 topLevelFQNs(JLS 语义)。 caller(BuildIndex Pass 1)负责
	// 把 nested 留在 allTypeFQNs 不进 topLevelFQNs。
	topFQNs := map[string]bool{
		"com.acme.dto.Outer": true,
		// "com.acme.dto.Inner" 故意不存(模拟 nested 不在 topLevel)
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
	// codex review #5:JLS 单类型 import 优先级 > wildcard。
	topFQNs := map[string]bool{
		"com.acme.dto.AssetQuery":   true, // wildcard 同名候选
		"com.acme.other.AssetQuery": true,
	}
	imports := []javaparser.ImportDecl{
		{Path: "com.acme.dto", Wildcard: true},
		{Path: "com.acme.other.AssetQuery"}, // explicit 应该胜出
	}
	out := extractImports(imports, topFQNs)
	if out["AssetQuery"] != "com.acme.other.AssetQuery" {
		t.Errorf("explicit import should win, got %q (out=%v)", out["AssetQuery"], out)
	}
}

func TestExtractImportsWildcardDeterministic(t *testing.T) {
	// 多次调相同输入,wildcard 展开结果 100% 确定(matches 按字典序排序)
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

	// allDst 含全部 5 个
	wantAll := []string{"p.Outer", "p.Inner", "p.Deep", "p.E", "p.Top2"}
	for _, w := range wantAll {
		if !allDst[w] {
			t.Errorf("missing %q in allDst = %v", w, allDst)
		}
	}
	if len(allDst) != len(wantAll) {
		t.Errorf("allDst size = %d, want %d (%v)", len(allDst), len(wantAll), allDst)
	}

	// topDst 只含 2 个顶层(Outer, Top2)
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
```

- [ ] **Step 3: 跑测试**

Run: `go test ./internal/schema/ -v -run 'TestAdapt|TestExtract|TestCollect'`

Expected: 全 PASS。 注意 `TestAdaptEmptyReturnsNil` / `TestAdaptDefaultPackageReturnsNil` 走的是 stub return (nil, nil),自然 pass。

- [ ] **Step 4: commit**

```bash
git add internal/schema/adapter_javaparser.go internal/schema/adapter_javaparser_test.go
git commit -m "feat: javaparser → schema adapter 骨架 + imports helper"
```

---

## Task 3:Adapter 识别 service type(class / interface / enum / record / annotation)

**Files:**
- Modify: `internal/schema/adapter_javaparser.go`(替换 `adaptCompilationUnit` stub)
- Modify: `internal/schema/adapter_javaparser_test.go`

- [ ] **Step 1: 实现 `adaptCompilationUnit` 的 service type 识别**

替换 `adaptCompilationUnit` 函数体:

```go
func adaptCompilationUnit(cu *javaparser.CompilationUnit, sourcePath string, body []byte, prefixes []string, topLevelFQNs map[string]bool) ([]Method, map[string]TypeSchema) {
	if cu == nil || cu.Package == nil || len(cu.Types) == 0 {
		return nil, nil
	}
	pkg := cu.Package.Name
	imports := extractImports(cu.Imports, topLevelFQNs)
	sourceHash := computeSourceHash(body)

	// service type:优先找第一个 interface;无则取第一个顶层 type(对齐既有
	// serviceTypeKind 行为)
	service := pickServiceType(cu.Types)
	if service == nil {
		return nil, nil
	}
	fqn := pkg + "." + service.Name

	out := map[string]TypeSchema{}

	// 顶层 + 嵌套全部产 TypeSchema(flat keying)
	emitTypeSchemas(cu.Types, pkg, sourcePath, imports, out)

	// 没有显式 service type schema 时补一个(对齐既有 parseTypes 末尾的兜底)
	if _, ok := out[fqn]; !ok {
		out[fqn] = TypeSchema{Type: fqn, Kind: "class", SourceFile: sourcePath, Imports: imports}
	}

	// methods 只在 service 是 interface 时生成 —— Task 4 接入,Task 3 阶段先空
	var methods []Method
	_ = sourceHash
	_ = prefixes
	return methods, out
}

// pickServiceType 找 service candidate(对齐老 serviceTypeKind 全文 regex 行为):
//   1. 顶层 interface 优先
//   2. 没顶层 interface 时,**递归 NestedTypes 找第一个 interface**(老 typeKindRE 全文扫
//      会匹配 nested,test `TestNestedInterfaceCanBeServiceCandidate` 依赖此行为)
//   3. 都没 interface,用第一个顶层 class/enum/record
//
// Annotation declaration (@interface) 不算 service type(老 parser 同样跳过)。
// **subagent T6 execution flag**:T3 stub 只搜顶层 → 漏 nested interface。
func pickServiceType(types []javaparser.TypeDecl) *javaparser.TypeDecl {
	// Pass 1: 顶层 interface
	for i := range types {
		if types[i].Kind == javaparser.TypeKindInterface {
			return &types[i]
		}
	}
	// Pass 2: 递归 nested interface(对齐老 typeKindRE 全文扫语义)
	if nested := findNestedInterface(types); nested != nil {
		return nested
	}
	// Pass 3: 第一个顶层 class/enum/record fallback
	for i := range types {
		switch types[i].Kind {
		case javaparser.TypeKindClass, javaparser.TypeKindEnum, javaparser.TypeKindRecord:
			return &types[i]
		}
	}
	return nil
}

// findNestedInterface 递归在 NestedTypes 找第一个 interface。
func findNestedInterface(types []javaparser.TypeDecl) *javaparser.TypeDecl {
	for i := range types {
		if types[i].Kind == javaparser.TypeKindInterface {
			return &types[i]
		}
		if len(types[i].NestedTypes) > 0 {
			if inner := findNestedInterface(types[i].NestedTypes); inner != nil {
				return inner
			}
		}
	}
	return nil
}

// emitTypeSchemas 把 types(含递归 NestedTypes)全部转成 TypeSchema 写进 out。
// Flat keying:pkg + "." + Name(沿用既有 parseTypes 行为,跟 resolveType 兼容)。
// Task 3 阶段只填 Type / Kind / SourceFile / Imports / TypeParams;Fields / EnumValues 在 Task 5 接入。
func emitTypeSchemas(types []javaparser.TypeDecl, pkg, sourcePath string, imports map[string]string, out map[string]TypeSchema) {
	for _, t := range types {
		// annotation declaration 不产 TypeSchema(老 parser 用 typeKindRE 不匹配 @interface,跟齐)
		if t.Kind == javaparser.TypeKindAnnotation {
			if len(t.NestedTypes) > 0 {
				emitTypeSchemas(t.NestedTypes, pkg, sourcePath, imports, out)
			}
			continue
		}
		fqn := pkg + "." + t.Name
		schema := TypeSchema{
			Type:       fqn,
			Kind:       typeKindName(t.Kind),
			SourceFile: sourcePath,
			Imports:    imports,
			TypeParams: typeParamNames(t.TypeParams),
		}
		out[fqn] = schema
		if len(t.NestedTypes) > 0 {
			emitTypeSchemas(t.NestedTypes, pkg, sourcePath, imports, out)
		}
	}
}

// typeKindName 把 javaparser TypeKind 映射成 schema.TypeSchema.Kind 字符串
// (对齐既有 typeKindRE 输出:"class" / "interface" / "enum" / "record")。
// 注意:@interface declaration 既有 regex parser 也跳过,这里不应被调用,默认 fallback "class"。
func typeKindName(k javaparser.TypeKind) string {
	switch k {
	case javaparser.TypeKindClass:
		return "class"
	case javaparser.TypeKindInterface:
		return "interface"
	case javaparser.TypeKindEnum:
		return "enum"
	case javaparser.TypeKindRecord:
		return "record"
	}
	return "class"
}

// typeParamNames 只取 TypeParam.Name(bound 不存进 schema —— Plan B P3 fix 只需要 name 列表)。
// nil 输入返回 nil,空 slice 也返回 nil(让 JSON omitempty 起效)。
func typeParamNames(params []javaparser.TypeParam) []string {
	if len(params) == 0 {
		return nil
	}
	out := make([]string, len(params))
	for i, p := range params {
		out[i] = p.Name
	}
	return out
}
```

- [ ] **Step 2: 加 service type + TypeSchema 识别测试**

```go
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
	// 老 serviceTypeKind:有 interface 时优先选 interface,即使它不在第一个
	src := []byte(`package p;
class FirstHelper {}
interface PrimaryFacade {}
class LastHelper {}`)
	cu, _ := javaparser.Parse(src, "T.java")
	_, types := adaptCompilationUnit(cu, "T.java", src, nil, nil)
	// 这里只验所有 3 个 type 都出现;service type 选择不直接体现在 TypeSchema 上,
	// 而是体现在 Methods 是否生成(Task 4 验)。
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
	// @interface declaration 老 parser 跳过,adapter 跟齐
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
```

- [ ] **Step 3: 跑测试**

Run: `go test ./internal/schema/ -v -run 'TestAdapt'`

Expected: 全 PASS。

- [ ] **Step 4: commit**

```bash
git add internal/schema/adapter_javaparser.go internal/schema/adapter_javaparser_test.go
git commit -m "feat: adapter 识别 service type + TypeSchema flat keying + TypeParams"
```

---

## Task 4:Adapter 转 method declaration

**Files:**
- Modify: `internal/schema/adapter_javaparser.go`(在 `adaptCompilationUnit` 里接入 methods)
- Modify: `internal/schema/adapter_javaparser_test.go`

- [ ] **Step 1: 在 `adaptCompilationUnit` 接入 method emission + 加 `emitMethods` / `typeRefToString` helpers**

把 `adaptCompilationUnit` 末尾的 stub `var methods []Method; _ = sourceHash; _ = prefixes; return methods, out` 替换为真正的 method 生成:

```go
	var methods []Method
	if service.Kind == javaparser.TypeKindInterface {
		methods = emitMethods(service, pkg, fqn, sourcePath, sourceHash, imports, prefixes)
	}
	return methods, out
}

// emitMethods 把 interface 的 MethodDecl 转成 schema.Method 切片。
// 既有 parseMethods 行为对齐:
//   - 跳过 ctor(`IsConstructor == true`)
//   - Summary 取 MethodDecl.Javadoc —— C.2 parsePreamble 已经 cleanJavadocText,直接用
//   - **Service=fqn**(interface 全限定名,如 `com.x.facade.AssetFacade`),**Interface=service.Name**(短名,如 `AssetFacade`)。 老 parseMethods 同形(见 schema.go 老 parseMethods)
//   - OutOfPrefix 用 prefixes 判断
//   - Imports 在文件级 share(每个 method 都引用同一份)
//   - **TypeParams = service.TypeParams + MethodDecl.TypeParams 拼接**(codex review #7):
//     `interface Facade<T> { T get(); }` 里 method 没有自己的 type params,但 `T` 是 service 级
//     type variable,rpc_types.go 必须能精确识别 → method.TypeParams 要把 service 的也带上
//   - ReturnType / Parameters[i].Type 走 typeRefToString(等同 TypeRef.String() 但留扩展点)
func emitMethods(service *javaparser.TypeDecl, pkg, fqn, sourcePath, sourceHash string, imports map[string]string, prefixes []string) []Method {
	if len(service.Methods) == 0 {
		return nil
	}
	serviceTypeParams := typeParamNames(service.TypeParams)
	out := make([]Method, 0, len(service.Methods))
	for _, m := range service.Methods {
		if m.IsConstructor {
			continue
		}
		methodTypeParams := mergeTypeParams(serviceTypeParams, typeParamNames(m.TypeParams))
		method := Method{
			Service:     fqn,
			Interface:   service.Name,
			Package:     pkg,
			Method:      m.Name,
			ReturnType:  typeRefToString(m.ReturnType),
			Parameters:  buildParameters(m.Params),
			Summary:     m.Javadoc,
			SourceFile:  sourcePath,
			SourceHash:  sourceHash,
			OutOfPrefix: !matchesAnyPrefix(fqn, prefixes),
			Imports:     imports,
			TypeParams:  methodTypeParams,
		}
		out = append(out, method)
	}
	return out
}

// mergeTypeParams 把 service-level + method-level type param 名拼成一个去重的 slice。
// 顺序:service 在前,method 在后(method 可能 shadow,但都视为 type variable,顺序只影响展示)。
// 都为 nil / 空时 return nil(JSON omitempty)。
func mergeTypeParams(serviceParams, methodParams []string) []string {
	if len(serviceParams) == 0 && len(methodParams) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(serviceParams)+len(methodParams))
	out := make([]string, 0, len(serviceParams)+len(methodParams))
	for _, p := range serviceParams {
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	for _, p := range methodParams {
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	return out
}

// buildParameters 把 javaparser.ParamDecl 转成 schema.Parameter 切片。
// 类型用 typeRefToString。
// 名字 fallback:`arg0` / `arg1`(对齐老 parseParameters 在缺名时的 fallback)。
func buildParameters(params []javaparser.ParamDecl) []Parameter {
	if len(params) == 0 {
		return nil
	}
	out := make([]Parameter, 0, len(params))
	for i, p := range params {
		name := p.Name
		if name == "" {
			name = fallbackParamName(i)
		}
		out = append(out, Parameter{Name: name, Type: typeRefToString(p.Type)})
	}
	return out
}

func fallbackParamName(i int) string {
	return "arg" + itoa(i)
}

// itoa 是 strconv.Itoa 的简化别名 —— 避免 adapter 文件再 import strconv,只此一处。
// 极少出现 i > 9 的 facade method,简单实现即可。
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := false
	if i < 0 {
		neg = true
		i = -i
	}
	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}

// typeRefToString 是 TypeRef.String() 的轻包装,留扩展点(C.3 之后真要做 nested type
// FQN 时,在这里集中改)。
func typeRefToString(t javaparser.TypeRef) string {
	return t.String()
}
```

注意:`matchesAnyPrefix` 是 `schema.go` 既有的函数,本文件直接用。

- [ ] **Step 2: 加 method 转换测试**

```go
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
	// codex review #7:`interface Facade<T> { T get(); }` 里的 method 没有自己的 type params,
	// 但 T 是 service 级 type variable。 Method.TypeParams 必须包含 service.TypeParams。
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
	// get 继承 service 的 [T, K]
	if !sliceEq(methods[0].TypeParams, []string{"T", "K"}) {
		t.Errorf("get.TypeParams = %v, want [T, K] (inherited from service)", methods[0].TypeParams)
	}
	// cast 合并 service [T, K] + 自己 [X] = [T, K, X]
	if !sliceEq(methods[1].TypeParams, []string{"T", "K", "X"}) {
		t.Errorf("cast.TypeParams = %v, want [T, K, X] (service ++ method)", methods[1].TypeParams)
	}
}

func TestMergeTypeParamsDedup(t *testing.T) {
	// method 跟 service 重名时 dedup(method shadow service,但语义上都是 type var)
	got := mergeTypeParams([]string{"T", "K"}, []string{"T", "X"})
	want := []string{"T", "K", "X"}
	if !sliceEq(got, want) {
		t.Errorf("mergeTypeParams dedup = %v, want %v", got, want)
	}
	if mergeTypeParams(nil, nil) != nil {
		t.Errorf("nil+nil should return nil for JSON omitempty")
	}
}

// sliceEq 是 test 内的简单 string slice 相等判断。
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
	// 老 parseJavaFile 只在 service 是 interface 时生成 methods。 class 即使有方法也不出。
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
	// interface 里写 ctor 是编译错的但 parser 容错;adapter 应跳过(IsConstructor=true)
	// 真实场景:class 里的 ctor 不进 methods,但 class 本身不产 methods,所以测试用 interface
	// 的 default method 替代 ctor 形态。 真正的 ctor 跳过测试通过 class 间接验。
	// 这里只验:interface 的所有非 ctor 方法都出。
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
```

- [ ] **Step 3: 跑测试**

Run: `go test ./internal/schema/ -v -run 'TestAdapt'`

Expected: 全 PASS。

- [ ] **Step 4: commit**

```bash
git add internal/schema/adapter_javaparser.go internal/schema/adapter_javaparser_test.go
git commit -m "feat: adapter 转 method declaration(含 TypeParams)"
```

---

## Task 5:Adapter 填 fields / enum values / record components

**Files:**
- Modify: `internal/schema/adapter_javaparser.go`(增强 `emitTypeSchemas`)
- Modify: `internal/schema/adapter_javaparser_test.go`

- [ ] **Step 1: 扩展 `emitTypeSchemas` 填充 body 内容**

把 `emitTypeSchemas` 函数替换为完整版本(在 Task 3 stub 基础上,加 Fields / EnumValues / RecordComponents 填充):

```go
func emitTypeSchemas(types []javaparser.TypeDecl, pkg, sourcePath string, imports map[string]string, out map[string]TypeSchema) {
	for _, t := range types {
		if t.Kind == javaparser.TypeKindAnnotation {
			if len(t.NestedTypes) > 0 {
				emitTypeSchemas(t.NestedTypes, pkg, sourcePath, imports, out)
			}
			continue
		}
		fqn := pkg + "." + t.Name
		schema := TypeSchema{
			Type:       fqn,
			Kind:       typeKindName(t.Kind),
			SourceFile: sourcePath,
			Imports:    imports,
			TypeParams: typeParamNames(t.TypeParams),
		}
		switch t.Kind {
		case javaparser.TypeKindEnum:
			schema.EnumValues = buildEnumValues(t.EnumValues)
			// enum body 里也可能有 Fields(`private final String label;`),老 parser 走
			// parseFields,这里 adapter 也填上
			schema.Fields = buildFieldsForType(t)
		case javaparser.TypeKindRecord:
			// record 组件视为 Fields(老 parseRecordFields 行为)
			schema.Fields = buildRecordFields(t.RecordComponents)
			// record body 内的额外字段(`private final int extra = 1;`)也追加进去 ——
			// 罕见但合法,跟齐老 parseFields 全文 regex 覆盖行为
			if extra := buildFieldsForType(t); len(extra) > 0 {
				schema.Fields = append(schema.Fields, extra...)
			}
		default:
			schema.Fields = buildFieldsForType(t)
		}
		out[fqn] = schema
		if len(t.NestedTypes) > 0 {
			emitTypeSchemas(t.NestedTypes, pkg, sourcePath, imports, out)
		}
	}
}

// buildFieldsForType 把 TypeDecl.Fields 转成 schema.Field 列表。
// 跳过 control keyword(if/for/while/...)—— 老 parseFields 通过 isControlKeyword
// 过滤,javaparser 走 AST 不会产生这种 noise,但保留 guard 防 future regression。
func buildFieldsForType(t javaparser.TypeDecl) []Field {
	if len(t.Fields) == 0 {
		return nil
	}
	out := make([]Field, 0, len(t.Fields))
	for _, f := range t.Fields {
		if isControlKeyword(f.Name) {
			continue
		}
		out = append(out, Field{Name: f.Name, Type: typeRefToString(f.Type)})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// buildRecordFields 把 RecordComponents([]ParamDecl)转成 Field 列表。
// 老 parseRecordFields 走 parseParameters → Field{Name, Type},等价。
func buildRecordFields(components []javaparser.ParamDecl) []Field {
	if len(components) == 0 {
		return nil
	}
	out := make([]Field, 0, len(components))
	for _, c := range components {
		name := c.Name
		if name == "" {
			name = fallbackParamName(len(out))
		}
		out = append(out, Field{Name: name, Type: typeRefToString(c.Type)})
	}
	return out
}

// buildEnumValues 把 EnumValue 列表只取 Name(老 parseEnumValues 同样只存名)。
func buildEnumValues(values []javaparser.EnumValue) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, len(values))
	for i, v := range values {
		out[i] = v.Name
	}
	return out
}
```

- [ ] **Step 2: 加 field / enum / record 测试**

```go
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
	wantFields := map[string]string{
		"id":    "Long",
		"name":  "String",
		"tags":  "java.util.List<String>",
		"CONST": "int",
	}
	if len(asset.Fields) != len(wantFields) {
		t.Fatalf("Fields = %+v, want %d entries", asset.Fields, len(wantFields))
	}
	for _, f := range asset.Fields {
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
	// enum body 内的 field 也要存
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
	// interface 没有 instance field,只可能有常量(public static final);javaparser 把 constant
	// 当 FieldDecl 收集,adapter 跟齐(老 parser 也收)
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
	// codex review #10:老 regex `fieldRE` 要求 private|protected|public,package-private
	// (无 modifier)的 field 被过滤掉。 新 adapter 走 AST,默认收所有 FieldDecl。
	// 这是**行为变化** —— 文档化:接受新行为(更完整,且 facade DTO 一般都有 access modifier,
	// package-private field 是 corner case)。
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
	// codex review #11:`record Page<T>(List<T> records)` —— record header 用 service-level
	// type variable,Page.TypeParams 应该带 `T`,Field types 保留 `List<T>`。
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
	// codex review #11:`class Page<T> { List<T> records; }` —— class TypeParams + Field types
	// 保留 type variable。 rpc_types.go P3 fix 依赖 TypeSchema.TypeParams 来识别 `T`。
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
```

- [ ] **Step 3: 跑测试**

Run: `go test ./internal/schema/ -v -run 'TestAdapt'`

Expected: 全 PASS。

- [ ] **Step 4: commit**

```bash
git add internal/schema/adapter_javaparser.go internal/schema/adapter_javaparser_test.go
git commit -m "feat: adapter 填 fields / enum values / record components"
```

---

## Task 6:`BuildIndex` 2-pass + wildcard import 展开

**Files:**
- Modify: `internal/schema/schema.go`(重写 `BuildIndex`,新加 `gatherCompilationUnits`)
- Modify: `internal/schema/schema_test.go`(新增 wildcard import unit test)

- [ ] **Step 1: 在 `internal/schema/schema.go` 重写 `BuildIndex`**

把现有 `BuildIndex` 函数(约 line 82)替换为 2-pass 版本。 注意保留既有签名:

```go
// BuildIndex 走 2 pass:
//   Pass 1: walk + parse 所有 .java 文件,收集全工程 type FQN 集合
//   Pass 2: in-memory 调 adapter,wildcard import 用 Pass 1 的集合展开
//
// 老的 1-pass parseJavaFile 在 Task 7 cutover 之后从 schema 包内部被 adapter 替换;
// 这里 BuildIndex 主循环已经走 javaparser + adapter 路径。
func BuildIndex(project Project) (*Index, error) {
	roots, err := DiscoverSourceRoots(project.WorkspaceRoot)
	if err != nil {
		return nil, err
	}
	idx := &Index{Project: project, Types: map[string]TypeSchema{}}

	parsed, topLevelFQNs, err := gatherCompilationUnits(roots)
	if err != nil {
		return nil, err
	}

	for _, p := range parsed {
		methods, types := adaptCompilationUnit(p.cu, p.path, p.body, project.ServicePrefixes, topLevelFQNs)
		idx.Methods = append(idx.Methods, methods...)
		for fqn, typ := range types {
			idx.Types[fqn] = typ
		}
	}

	sort.Slice(idx.Methods, func(i, j int) bool {
		if idx.Methods[i].Service == idx.Methods[j].Service {
			return idx.Methods[i].Method < idx.Methods[j].Method
		}
		return idx.Methods[i].Service < idx.Methods[j].Service
	})
	return idx, nil
}

// parsedFile 把每个 .java 文件的解析结果跟原始 bytes 一起缓存,避免 Pass 2 再 parse 一遍。
//
// 内存 trade-off(codex review #2):假设 100 个 .java 文件 / 每个 10KB body + 30KB AST,
// 总 cache ≈ 4MB。 facade 工程典型规模(fundsalesmrksupport ~600 个 .java 文件,平均 6KB)
// 估算上限 ~25MB,可接受。 大型 monorepo(>5000 文件)真撞到再切 2-pass re-parse 模式。
type parsedFile struct {
	path string
	body []byte
	cu   *javaparser.CompilationUnit
}

// gatherCompilationUnits 是 BuildIndex 的 Pass 1。
// 遍历所有 source root,parse 每个 .java 文件;失败 file 静默跳过(对齐老 parseJavaFile
// 在 os.ReadFile 错误时 return nil, nil 行为 —— codex review #3:syntax 错误也静默跳过,
// **不向 caller 报告**;若未来要 logging 加观测,在这一层加 callback,本 plan 暂不引入)。
//
// 收集顶层 type FQN 进 topLevelFQNs(用于 wildcard import 展开);nested 不在 topLevel,
// 走 allFQNs(但 allFQNs 当前 caller 不需要,只为 future 扩展保留;本 plan 不返回)。
func gatherCompilationUnits(roots []string) ([]parsedFile, map[string]bool, error) {
	var parsed []parsedFile
	topLevelFQNs := map[string]bool{}
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
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
			body, readErr := os.ReadFile(path)
			if readErr != nil {
				return nil
			}
			cu, parseErr := javaparser.Parse(body, path)
			if parseErr != nil || cu == nil {
				return nil
			}
			if cu.Package != nil {
				// dstAll = nil: BuildIndex 只需要 topLevel 给 wildcard 用,nested 已通过
				// 各文件 adapter 路径单独 emit 进 idx.Types
				collectTypeFQNs(cu.Package.Name, cu.Types, nil, topLevelFQNs)
			}
			parsed = append(parsed, parsedFile{path: path, body: body, cu: cu})
			return nil
		})
		if err != nil {
			return nil, nil, err
		}
	}
	return parsed, topLevelFQNs, nil
}
```

需要在 `schema.go` 顶部 import 里加 `"github.com/diandian921/sofarpc-cli/internal/javaparser"`(若还没有)。 既有 `parseJavaFile` 函数 Task 8 才删,Task 6 里保留(已经不再被 `BuildIndex` 主路径调用 —— 但删除前可能仍被 test 或者 dead code 链引用,Task 8 一次清理)。

- [ ] **Step 2: 加 wildcard import unit test**

在 `internal/schema/adapter_javaparser_test.go` 末尾追加:

```go
func TestBuildIndexWildcardImportExpansion(t *testing.T) {
	// 用 testdata 临时目录构造 wildcard import 场景:facade 用 `import a.b.*;` 引用 a.b 包下的 DTO
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

	// MyFacade.query 的 imports 应该包含 wildcard 展开后的 MyReq / MyResp
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

	// Describe 也能正确解析 MyReq / MyResp 字段
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
```

需要 `internal/schema/adapter_javaparser_test.go` 顶部 import `"os"`、`"path/filepath"`。

- [ ] **Step 3: 加 malformed-file silent-skip regression test**

在 `internal/schema/adapter_javaparser_test.go` 末尾追加(codex review #3:确认 parse error 静默跳过,不污染 BuildIndex 结果):

```go
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
```

- [ ] **Step 4: 跑测试 + 确认无 regression**

Run: `go test ./internal/schema/ -v -run 'TestBuildIndexWildcardImportExpansion|TestBuildIndexSilentlySkipsMalformedFiles'`

Expected: 新加 2 个 test PASS。

**然后跑全套 `go test ./internal/schema/`**(codex review #12:Task 6 commit 不应让 repo 处于 red 状态):

Run: `go test ./internal/schema/`

Expected: 全 PASS,**包括既有的 3 个 `TestParserGolden*` 老 golden test**。 因为这些 fixture 简单(无 wildcard、无 generic edge case),adapter 输出应该跟 regex parser bit-for-bit 相同。

**如果有任何 golden test fail**:
- 不要直接 commit
- 不要修 golden assertion(那是 Task 7 的事)
- 回头排查:adapter 输出哪里跟 regex 不一样?是 adapter 缺一种 case,还是 regex 之前漏了?
- 修完 adapter 再回到这一步

Task 7 是一个 follow-up audit step,正常情况下应该是 no-op(adapter 输出对齐 regex);只有发现 regex 本来就有 bug 时才会需要更新 golden。

- [ ] **Step 5: commit**

```bash
git add internal/schema/schema.go internal/schema/adapter_javaparser_test.go
git commit -m "feat: BuildIndex 2-pass + wildcard import 展开 + malformed-file 静默跳过"
```

---

## Task 7:Golden test cutover audit

**Files:**
- Modify: `internal/schema/parser_golden_test.go`(必要时调整 assertion)

- [ ] **Step 1: 跑现有 3 个 golden test 看是否 PASS**

Run: `go test ./internal/schema/ -v -run 'TestParserGolden'`

预期结果:**绝大多数应该 PASS**(adapter 输出对齐既有 parseJavaFile),但允许少数 assertion fail —— 因为老 regex parser 在某些 edge case 处理不一致(typeKindRE / methodRE 的边界判断),adapter 走 AST 更精确。

如果有 fail,**逐条审查**:
- 老 parser 漏匹配(adapter 补回来):接受,留 assertion 不动,改 adapter 让结果对齐(若需要)
- 老 parser 误匹配(adapter 修正):接受,更新 assertion 反映真实情况
- adapter 缺漏(新 bug):STOP,fix adapter,不动 assertion

- [ ] **Step 2: 如果有任何 assertion 需要调整,记录在这一 task 的 commit message 里,逐条说明**

例如(假设 `TestParserGoldenModernJavaFacade` 的 `Page.Fields` 顺序变了):

```go
// 老 regex parser:Fields 是 [records, total](按 regex 匹配顺序)
// 新 adapter:Fields 是 [records, total](按 AST 源码顺序,等价)
// → 无修改
```

如果发现真要改 assertion,Edit 对应 test 文件,并把改动原因写进 commit。

- [ ] **Step 3: 跑全套 `go test ./internal/schema/`**

Run: `go test ./internal/schema/`

Expected: 全 PASS。

- [ ] **Step 4: 跑全项目 regression(下游 internal/app / internal/mcp 可能依赖 schema 输出形态)**

Run: `go test ./...`

Expected: 全 PASS。 如果 `internal/app/rpc_types_test.go` 或 `internal/mcp/server_test.go` fail,说明 adapter 输出的 Method.Imports / TypeSchema.Fields 顺序 / 内容跟老 parser 不一致 —— 这种通常是 adapter 缺一个 case,回到 Task 5/6 修。

- [ ] **Step 5: commit(必要时)**

```bash
git add internal/schema/parser_golden_test.go
git commit -m "test: golden cutover audit(adjustments: <list>)"
```

如果没有任何 test 改动(全 PASS without modification),跳过 commit。

---

## Task 8:删除老 regex parser 代码

**Files:**
- Modify: `internal/schema/schema.go`(删除 regex 定义 + 老 parseJavaFile/parseMethods/parseTypes/parseFields/parseImports/parseRecordFields/parseEnumValues/parseParameters/cleanType/cleanJavadoc/stripAnnotations/serviceTypeKind/firstSubmatch/eraseGeneric/splitCommaAware/isJavaIdentByte 等)

- [ ] **Step 1: 用 grep 找出 schema.go 内仍被 BuildIndex / Search / Describe / resolveType / referencedTypes / Tokenize / addDescribedType 引用的 helper,确认安全删除范围**

Run: `grep -nE '^func ' internal/schema/schema.go`

预期看到一堆函数定义。 对每个函数判断是否还有 caller:

```bash
for fn in parseJavaFile parseMethods parseTypes parseFields parseImports parseRecordFields parseEnumValues parseParameters cleanType cleanJavadoc stripAnnotations serviceTypeKind firstSubmatch eraseGeneric splitCommaAware isJavaIdentByte isControlKeyword; do
  count=$(grep -c "\b$fn\b" internal/schema/*.go)
  echo "$fn: $count refs"
done
```

理论上(adapter 完全接管后):
- `parseJavaFile / parseMethods / parseTypes / parseFields / parseImports / parseRecordFields / parseEnumValues / parseParameters / serviceTypeKind / firstSubmatch / cleanJavadoc / stripAnnotations / isJavaIdentByte` —— 只剩 self 定义(无 caller)→ 可删
- `cleanType / eraseGeneric / splitCommaAware` —— 可能被 `resolveType / referencedTypes` 还引用,需要检查;若引用,保留并标 "kept for resolveType",若无引用,删
- `isControlKeyword` —— `buildFieldsForType` 用着,保留
- regex 变量(`packageRE / importRE / typeKindRE / methodRE / fieldRE / enumValueRE`)→ 全删

- [ ] **Step 2: 删除已无 caller 的函数和 regex**

参考 Step 1 的结果,从 `internal/schema/schema.go` 删:

```go
// 删除以下变量:
var (
	packageRE   = regexp.MustCompile(...)
	importRE    = regexp.MustCompile(...)
	typeKindRE  = regexp.MustCompile(...)
	methodRE    = regexp.MustCompile(...)
	fieldRE     = regexp.MustCompile(...)
	enumValueRE = regexp.MustCompile(...)
)

// 删除以下函数:
func parseJavaFile(...) { ... }
func parseMethods(...) { ... }
func parseTypes(...) { ... }
func parseParameters(...) { ... }
func parseFields(...) { ... }
func parseRecordFields(...) { ... }
func parseEnumValues(...) { ... }
func parseImports(...) { ... }
func cleanJavadoc(...) { ... }
func stripAnnotations(...) { ... }
func isJavaIdentByte(...) { ... }
func firstSubmatch(...) { ... }
func serviceTypeKind(...) { ... }
```

`cleanType / eraseGeneric / splitCommaAware` 三个保留,但移到文件末尾,加注释说明:

```go
// cleanType / eraseGeneric / splitCommaAware 三个 helper 还被 resolveType /
// referencedTypes 用着(它们处理 schema 内部已经组装好的 TypeRef.String() 字符串,
// 不再用于解析源码)。 等 C.3 后续 refactor 把 resolveType 完全迁到 javaparser 时再删。
```

`isControlKeyword` 保留:

```go
// isControlKeyword 仍被 adapter_javaparser.go::buildFieldsForType 防御性 guard 引用。
func isControlKeyword(s string) bool { ... }
```

删完后 `internal/schema/schema.go` 顶部 import 区域:`"regexp"` 应当无引用,删除。 `"crypto/sha256"` / `"encoding/hex"` 因 `parseJavaFile` 已删,需要检查 —— sourceHash 在 adapter 里算,schema.go 不再需要,删 import。 `"unicode"` 还被 splitIdentifier / containsCJK / isCJK 用着,保留。

跑 `go build ./internal/schema/` 验证 import 清理是否正确。

- [ ] **Step 3: 跑全套测试确认无 regression**

Run: `go vet ./internal/schema/ && go test ./internal/schema/ -v`

Expected: vet 无 warning,test 全 PASS。

Run: `go test ./...`

Expected: 全项目 PASS。

- [ ] **Step 4: commit**

```bash
git add internal/schema/schema.go
git commit -m "refactor: 删除 schema 包 regex parser 代码(已被 javaparser adapter 替换)"
```

---

## Task 9:`rpc_types.go` P3 fix —— 用 `TypeParams` 精确识别 type variable

**Files:**
- Modify: `internal/app/rpc_types.go`(`resolveBaseType / rpcParamTypeForMethod / rpcFieldTypeForType` 加 TypeParams 检查)
- Modify: `internal/app/rpc_types_test.go`(新增 P3 regression case)

- [ ] **Step 1: 在 `resolveBaseType` 加 declared type params 精确查表**

打开 `internal/app/rpc_types.go`,找到 `resolveBaseType` 函数(约 line 245)。 把它替换为:

```go
// resolveBaseType 把无泛型的短名解析成 FQN。
// 顺序:Java built-in → 已带 "." → 显式 import → declared type params 精确匹配 → same-pkg
// schema lookup → type variable 启发式 fallback → pkg fallback。
//
// declaredTypeParams 是当前 method 或 type 的 declared type parameter 简单名列表
// (`<T, K>` → ["T", "K"])。 在 pkg fallback 之前精确匹配:命中即 return as-is,
// 不进 same-pkg lookup。 根治 [[rpc-types-generic-preservation]] P3(`class Page<T>`
// 同 package 真有 `com.x.dto.T` 类时不被误解析为 DTO)。
//
// declaredTypeParams 为 nil 时(老 schema cache 还没填 TypeParams,或调用方没传)
// 退化为老的启发式行为(`isLikelyTypeVariable`)。
func resolveBaseType(base string, imports map[string]string, pkg string, types map[string]schema.TypeSchema, declaredTypeParams []string) string {
	if base == "" {
		return base
	}
	mapped := rpcParamType(base)
	if mapped != base || strings.Contains(mapped, ".") || isPrimitiveRPCType(mapped) {
		return mapped
	}
	if imported, ok := imports[base]; ok {
		return imported
	}
	// 精确匹配 declared type params —— same-pkg DTO 同名时按 type var 处理
	for _, tp := range declaredTypeParams {
		if tp == base {
			return base
		}
	}
	if pkg != "" {
		fqn := pkg + "." + base
		if _, ok := types[fqn]; ok {
			return fqn
		}
	}
	if isLikelyTypeVariable(base) {
		return base
	}
	if pkg != "" {
		return pkg + "." + base
	}
	return base
}
```

- [ ] **Step 2: 修改 `resolveGenericType` 签名加 declaredTypeParams 参数**

`resolveGenericType`(约 line 197)签名扩展:

```go
func resolveGenericType(typ string, imports map[string]string, pkg string, types map[string]schema.TypeSchema, declaredTypeParams []string) string {
```

函数体内调 `resolveBaseType` / 递归调 `resolveGenericType` 都把 `declaredTypeParams` 透传下去:

```go
	resolvedBase := resolveBaseType(base, imports, pkg, types, declaredTypeParams)
	// ...
	for i, arg := range args {
		resolved[i] = resolveGenericType(arg, imports, pkg, types, declaredTypeParams)
	}
```

**重要**(codex review #8):这是 ABI-breaking 签名变更,**所有 4 处 test callsite 必须同步**(不更新会 build fail)。 在 `internal/app/rpc_types_test.go` 找:

```bash
grep -n 'resolveGenericType\|resolveBaseType' internal/app/rpc_types_test.go
```

预期看到 ~4 处 caller,全部在末尾加 `nil` 参数(nil 触发老 heuristic fallback,行为不变):

```go
// before
got := resolveGenericType(tc.in, imports, pkg, nil)
// after
got := resolveGenericType(tc.in, imports, pkg, nil, nil)
```

```go
// before
got := resolveGenericType("T", imports, "com.example.pkg", nil)
// after
got := resolveGenericType("T", imports, "com.example.pkg", nil, nil)
```

```go
// before
got := resolveGenericType("URL", nil, "com.example.dto", types)
// after
got := resolveGenericType("URL", nil, "com.example.dto", types, nil)
```

```go
// before
got := resolveGenericType("URL", nil, "com.example.dto", nil)
// after
got := resolveGenericType("URL", nil, "com.example.dto", nil, nil)
```

更新完跑 `go build ./internal/app/` 确认编译过(签名一致)。

- [ ] **Step 3: 更新 `rpcValueTypeForMethod` 和 `rpcValueTypeForType` 把 TypeParams 传进去**

`rpcValueTypeForMethod`(约 line 339):

```go
func rpcValueTypeForMethod(typ string, method schema.Method, types map[string]schema.TypeSchema) string {
	return resolveGenericType(typ, method.Imports, method.Package, types, method.TypeParams)
}
```

`rpcValueTypeForType`(约 line 368):

```go
func rpcValueTypeForType(typ string, owner schema.TypeSchema, types map[string]schema.TypeSchema) string {
	pkg := ""
	if owner.Type != "" {
		if lastDot := strings.LastIndex(owner.Type, "."); lastDot > 0 {
			pkg = owner.Type[:lastDot]
		}
	}
	return resolveGenericType(typ, owner.Imports, pkg, types, owner.TypeParams)
}
```

- [ ] **Step 4: 更新 `rpcParamTypeForMethod` 和 `rpcFieldTypeForType` identity 路径也加 TypeParams 检查**

`rpcParamTypeForMethod`(约 line 314)替换为:

```go
func rpcParamTypeForMethod(typ string, method schema.Method) string {
	base := eraseRPCGeneric(typ)
	if base == "" {
		return typ
	}
	mapped := rpcParamType(base)
	if mapped != base || strings.Contains(mapped, ".") || isPrimitiveRPCType(mapped) {
		return mapped
	}
	if imported, ok := method.Imports[base]; ok {
		return imported
	}
	// declared type param 精确匹配 → 不 pkg fallback,return as-is
	for _, tp := range method.TypeParams {
		if tp == base {
			return base
		}
	}
	if method.Package != "" {
		return method.Package + "." + base
	}
	return base
}
```

`rpcFieldTypeForType`(约 line 346)替换为:

```go
func rpcFieldTypeForType(typ string, owner schema.TypeSchema) string {
	base := eraseRPCGeneric(typ)
	if base == "" {
		return typ
	}
	mapped := rpcParamType(base)
	if mapped != base || strings.Contains(mapped, ".") || isPrimitiveRPCType(mapped) {
		return mapped
	}
	if imported, ok := owner.Imports[base]; ok {
		return imported
	}
	for _, tp := range owner.TypeParams {
		if tp == base {
			return base
		}
	}
	if owner.Type != "" {
		if lastDot := strings.LastIndex(owner.Type, "."); lastDot > 0 {
			return owner.Type[:lastDot+1] + base
		}
	}
	return base
}
```

- [ ] **Step 5: 加 P3 regression test**

在 `internal/app/rpc_types_test.go` 末尾追加:

```go
func TestResolveBaseTypeP3DeclaredTypeParamShadowsSamePkgClass(t *testing.T) {
	// [[rpc-types-generic-preservation]] P3 fix:`class Page<T>` 同 package 真有
	// `com.x.dto.T` 类时,T 应按 type var 处理(return as-is),不被 same-pkg lookup
	// 误解析为 DTO。
	imports := map[string]string{}
	pkg := "com.x.dto"
	types := map[string]schema.TypeSchema{
		"com.x.dto.T": {Type: "com.x.dto.T", Kind: "class"},
	}
	got := resolveBaseType("T", imports, pkg, types, []string{"T", "K"})
	if got != "T" {
		t.Errorf("resolveBaseType(T) = %q, want T (declared type param wins over same-pkg lookup)", got)
	}
	// 但同 package 的 `K` 没有 schema,也是 declared type param → 也是 return as-is
	got = resolveBaseType("K", imports, pkg, types, []string{"T", "K"})
	if got != "K" {
		t.Errorf("resolveBaseType(K) = %q, want K", got)
	}
	// 同 package 的 `MaterialItem` 不在 declared type params 里,有 schema → resolve to FQN
	types["com.x.dto.MaterialItem"] = schema.TypeSchema{Type: "com.x.dto.MaterialItem", Kind: "class"}
	got = resolveBaseType("MaterialItem", imports, pkg, types, []string{"T", "K"})
	if got != "com.x.dto.MaterialItem" {
		t.Errorf("resolveBaseType(MaterialItem) = %q, want com.x.dto.MaterialItem", got)
	}
}

func TestResolveBaseTypeP3NilDeclaredTypeParamsFallsBackToHeuristic(t *testing.T) {
	// declaredTypeParams 为 nil 时退化为老启发式 —— 兼容老 cache 没填 TypeParams 的情况
	imports := map[string]string{}
	pkg := "com.x.dto"
	types := map[string]schema.TypeSchema{}
	got := resolveBaseType("T", imports, pkg, types, nil)
	if got != "T" {
		t.Errorf("nil TypeParams + likely type var → %q, want T (heuristic still fires)", got)
	}
	// 全大写但 same-pkg 真有 schema → schema lookup 优先(老行为)
	types["com.x.dto.ID"] = schema.TypeSchema{Type: "com.x.dto.ID", Kind: "class"}
	got = resolveBaseType("ID", imports, pkg, types, nil)
	if got != "com.x.dto.ID" {
		t.Errorf("nil TypeParams + ID with schema → %q, want com.x.dto.ID", got)
	}
}

func TestRpcParamTypeForMethodP3SkipsTypeParamPkgFallback(t *testing.T) {
	method := schema.Method{
		Package:    "com.x.facade",
		TypeParams: []string{"T"},
		Imports:    map[string]string{},
	}
	got := rpcParamTypeForMethod("T", method)
	if got != "T" {
		t.Errorf("rpcParamTypeForMethod(T) = %q, want T (TypeParam should bypass pkg fallback)", got)
	}
	// 非 type param 仍走 pkg fallback
	got = rpcParamTypeForMethod("MaterialItem", method)
	if got != "com.x.facade.MaterialItem" {
		t.Errorf("rpcParamTypeForMethod(MaterialItem) = %q, want com.x.facade.MaterialItem", got)
	}
}

func TestRpcFieldTypeForTypeP3SkipsTypeParamPkgFallback(t *testing.T) {
	owner := schema.TypeSchema{
		Type:       "com.x.dto.Page",
		Kind:       "class",
		TypeParams: []string{"T"},
		Imports:    map[string]string{},
	}
	got := rpcFieldTypeForType("T", owner)
	if got != "T" {
		t.Errorf("rpcFieldTypeForType(T) = %q, want T (class TypeParam should bypass pkg fallback)", got)
	}
}
```

- [ ] **Step 6: 跑 rpc_types 测试**

Run: `go test ./internal/app/ -v -run 'TestResolveBaseType|TestRpcParamTypeForMethodP3|TestRpcFieldTypeForTypeP3|TestTypedArguments|TestResolveGenericType|TestExtractGenericArgs'`

Expected: 新 P3 test 全 PASS,既有 Plan B test 全部继续 PASS。 注意:`resolveGenericType` 签名扩展**不是** ABI-additive(Go 必须更新所有 callsite 才能 compile);Step 2 已经列了 4 处 test 改动,确认全部按 `nil` 传入(退化老 heuristic 行为)。

- [ ] **Step 7: 跑全项目 regression**

Run: `go test ./...`

Expected: 全 PASS。

- [ ] **Step 8: commit**

```bash
git add internal/app/rpc_types.go internal/app/rpc_types_test.go
git commit -m "fix: rpc-types P3 根治 —— 用 declared TypeParams 精确识别 type variable"
```

---

## Task 10:新加 3 个 golden case(wildcard import / inner class / pollution-guard)

**Files:**
- Create: `internal/schema/testdata/golden/wildcard/src/main/java/com/acme/wildcard/facade/WildcardFacade.java`
- Create: `internal/schema/testdata/golden/wildcard/src/main/java/com/acme/wildcard/dto/WildReq.java`
- Create: `internal/schema/testdata/golden/wildcard/src/main/java/com/acme/wildcard/dto/WildResp.java`
- Create: `internal/schema/testdata/golden/wildcard/src/main/java/com/acme/wildcard/dto/WildContainer.java`(nested type 防污染)
- Create: `internal/schema/testdata/golden/wildcard/src/main/java/com/acme/wildcard/other/Unrelated.java`(跨 package 防污染)
- Create: `internal/schema/testdata/golden/inner/src/main/java/com/acme/inner/facade/OuterFacade.java`
- Modify: `internal/schema/parser_golden_test.go`(新增 3 个 test)

- [ ] **Step 1: 创建 wildcard fixture**

`internal/schema/testdata/golden/wildcard/src/main/java/com/acme/wildcard/facade/WildcardFacade.java`:

```java
package com.acme.wildcard.facade;

import com.acme.wildcard.dto.*;

/**
 * Wildcard import facade.
 */
public interface WildcardFacade {
    /** 查询资产 */
    WildResp query(WildReq req);
}
```

`internal/schema/testdata/golden/wildcard/src/main/java/com/acme/wildcard/dto/WildReq.java`:

```java
package com.acme.wildcard.dto;

public class WildReq {
    private String key;
    private Long mpCode;
}
```

`internal/schema/testdata/golden/wildcard/src/main/java/com/acme/wildcard/dto/WildResp.java`:

```java
package com.acme.wildcard.dto;

import java.util.List;

public class WildResp {
    private boolean success;
    private List<String> messages;
}
```

`internal/schema/testdata/golden/wildcard/src/main/java/com/acme/wildcard/dto/WildContainer.java`(用来验证 nested 不被 wildcard 误展开):

```java
package com.acme.wildcard.dto;

public class WildContainer {
    public static class WildInner {
        public String detail;
    }
}
```

`internal/schema/testdata/golden/wildcard/src/main/java/com/acme/wildcard/other/Unrelated.java`(不同 package,必须不被 wildcard 展开):

```java
package com.acme.wildcard.other;

public class Unrelated {
    public String name;
}
```

- [ ] **Step 2: 创建 inner class fixture**

`internal/schema/testdata/golden/inner/src/main/java/com/acme/inner/facade/OuterFacade.java`:

```java
package com.acme.inner.facade;

import java.util.List;

/**
 * Outer facade with inner DTOs.
 */
public interface OuterFacade {
    /** 列出所有 page */
    List<PageResult> listPages(PageQuery query);

    class PageQuery {
        public Long mpCode;
        public int offset;
    }

    class PageResult {
        public String name;
        public List<String> tags;
    }
}
```

- [ ] **Step 3: 加 3 个新 golden test**

在 `internal/schema/parser_golden_test.go` 末尾追加:

```go
func TestParserGoldenWildcardImport(t *testing.T) {
	// codex C.2/C.3 长期方案的业务症状:wildcard import 应被正确展开。
	root := filepath.Join("testdata", "golden", "wildcard")
	idx, err := BuildIndex(Project{
		Name:            "wildcard",
		WorkspaceRoot:   root,
		ServicePrefixes: []string{"com.acme.wildcard.facade."},
	})
	if err != nil {
		t.Fatalf("BuildIndex: %v", err)
	}
	desc, err := Describe(idx, "com.acme.wildcard.facade.WildcardFacade", "query")
	if err != nil {
		t.Fatalf("Describe: %v", err)
	}
	if len(desc.Methods) != 1 {
		t.Fatalf("methods = %#v", desc.Methods)
	}
	method := desc.Methods[0]
	if method.ReturnType != "WildResp" {
		t.Errorf("ReturnType = %q, want WildResp", method.ReturnType)
	}
	if method.Parameters[0].Type != "WildReq" {
		t.Errorf("param[0].Type = %q, want WildReq", method.Parameters[0].Type)
	}
	// wildcard 应展开成显式 short→FQN 映射
	if method.Imports["WildReq"] != "com.acme.wildcard.dto.WildReq" {
		t.Errorf("imports[WildReq] = %q, want com.acme.wildcard.dto.WildReq (wildcard expanded?)", method.Imports["WildReq"])
	}
	if method.Imports["WildResp"] != "com.acme.wildcard.dto.WildResp" {
		t.Errorf("imports[WildResp] = %q, want com.acme.wildcard.dto.WildResp", method.Imports["WildResp"])
	}
	// Describe 应该把 WildReq / WildResp 一起带出来
	req := desc.Types["com.acme.wildcard.dto.WildReq"]
	if req.Type == "" {
		t.Fatalf("WildReq schema missing in desc.Types = %v", desc.Types)
	}
	assertFields(t, req, map[string]string{"key": "String", "mpCode": "Long"})

	resp := desc.Types["com.acme.wildcard.dto.WildResp"]
	if resp.Type == "" {
		t.Fatalf("WildResp schema missing in desc.Types = %v", desc.Types)
	}
	assertFields(t, resp, map[string]string{"success": "boolean", "messages": "List<String>"})
}

func TestParserGoldenInnerClass(t *testing.T) {
	// nested type:OuterFacade 内嵌 PageQuery / PageResult。 老 regex parser 用
	// typeKindRE 全文扫,也把 nested 当成顶层(flat keying);新 adapter 跟齐。
	root := filepath.Join("testdata", "golden", "inner")
	idx, err := BuildIndex(Project{
		Name:            "inner",
		WorkspaceRoot:   root,
		ServicePrefixes: []string{"com.acme.inner.facade."},
	})
	if err != nil {
		t.Fatalf("BuildIndex: %v", err)
	}
	desc, err := Describe(idx, "com.acme.inner.facade.OuterFacade", "listPages")
	if err != nil {
		t.Fatalf("Describe: %v", err)
	}
	method := desc.Methods[0]
	if method.ReturnType != "List<PageResult>" {
		t.Errorf("ReturnType = %q, want List<PageResult>", method.ReturnType)
	}
	if method.Parameters[0].Type != "PageQuery" {
		t.Errorf("param.Type = %q, want PageQuery", method.Parameters[0].Type)
	}
	// nested DTO 通过 same-pkg lookup 解析(flat keying:com.acme.inner.facade.PageQuery)
	query := desc.Types["com.acme.inner.facade.PageQuery"]
	if query.Type == "" {
		t.Fatalf("PageQuery schema missing: %v", desc.Types)
	}
	assertFields(t, query, map[string]string{"mpCode": "Long", "offset": "int"})

	result := desc.Types["com.acme.inner.facade.PageResult"]
	if result.Type == "" {
		t.Fatalf("PageResult schema missing: %v", desc.Types)
	}
	assertFields(t, result, map[string]string{"name": "String", "tags": "List<String>"})
}

func TestParserGoldenWildcardExpansionDoesNotPolluteUnrelatedPackages(t *testing.T) {
	// 防御性 regression(codex round 1 #5 + round 2 #7):
	//   - wildcard `import com.acme.wildcard.dto.*` 只展开同 package 顶层 type
	//   - 不能拉进 nested type(`WildContainer.WildInner` 不要变成 imports["WildInner"])
	//   - 不能拉进不同 package(`com.acme.wildcard.other.Unrelated` 不要进 imports)
	root := filepath.Join("testdata", "golden", "wildcard")
	idx, err := BuildIndex(Project{
		Name:            "wildcard",
		WorkspaceRoot:   root,
		ServicePrefixes: []string{"com.acme.wildcard.facade."},
	})
	if err != nil {
		t.Fatalf("BuildIndex: %v", err)
	}
	desc, _ := Describe(idx, "com.acme.wildcard.facade.WildcardFacade", "query")
	method := desc.Methods[0]
	// imports 应含 wildcard 同 package 顶层 type:WildReq / WildResp / WildContainer = 3 条
	if len(method.Imports) != 3 {
		t.Fatalf("imports = %v, want exactly 3 (top-level of wildcard package only)", method.Imports)
	}
	want := []string{"WildReq", "WildResp", "WildContainer"}
	for _, w := range want {
		if _, ok := method.Imports[w]; !ok {
			t.Errorf("expected import %q missing: %v", w, method.Imports)
		}
	}
	// 防 nested 污染
	if _, ok := method.Imports["WildInner"]; ok {
		t.Errorf("WildInner (nested) should NOT be in wildcard imports: %v", method.Imports)
	}
	// 防跨 package 污染
	if _, ok := method.Imports["Unrelated"]; ok {
		t.Errorf("Unrelated (different package) should NOT be in wildcard imports: %v", method.Imports)
	}
}
```

- [ ] **Step 4: 跑新 golden test**

Run: `go test ./internal/schema/ -v -run 'TestParserGoldenWildcardImport|TestParserGoldenInnerClass|TestParserGoldenWildcardExpansionDoesNotPolluteUnrelatedPackages'`

Expected: 3 个 test 全 PASS。 若 fail:
- wildcard 没展开 → 检查 Task 6 `gatherCompilationUnits` 是否正确收集 FQN,Task 2 `extractImports` 是否正确展开
- inner class 找不到 → 检查 Task 5 `emitTypeSchemas` 是否递归 NestedTypes
- imports 数量超过 2 → 检查 Task 2 `extractImports` 的 nested 跳过逻辑(`strings.Contains(rest, ".")` 那行)

- [ ] **Step 5: commit**

```bash
git add internal/schema/testdata/golden/wildcard internal/schema/testdata/golden/inner internal/schema/parser_golden_test.go
git commit -m "test: 加 wildcard import / inner class golden case 覆盖 C.3 cutover"
```

---

## Task 11:Coverage + vet + build + 全套 regression + 最终 commit

**Files:** all created / modified above + plan 文档

- [ ] **Step 1: 跑 coverage**

Run: `go test ./internal/schema/ -cover -v`

Expected: coverage ≥ 80%。 跟 C.2 javaparser 类似 ballpark。 若某些分支没覆盖到,补 test。 重点检查:
- `extractImports` 的 static wildcard 分支(C.3 OOS,代码可能没分支)
- `emitTypeSchemas` 的 annotation declaration 分支
- `BuildIndex` 的 EOF / 错误 file 跳过分支

```
go test ./internal/app/ -cover
```

rpc_types.go P3 fix 也应该被覆盖 ≥ 80%。

- [ ] **Step 2: vet + build**

Run: `go vet ./... && go build ./...`

Expected: 无 warning / error。 注意 build 全项目,不只是 schema。

- [ ] **Step 3: 跑全套 regression(含 dogfood-style 验证)**

Run: `go test ./...`

Expected: 全 PASS。

如果之前留下的 `~/.sofarpc/cache/schema/projects/*` 缓存还在:它们是 schemaVersion="3" 的旧 cache。 LoadOrBuildIndex 会自动判断版本不匹配 → 重新 BuildIndex。 不需要手工清缓存,但**值得手动验证一次**:

```bash
sofarpc-cli config doctor  # 或者其他会触发 LoadOrBuildIndex 的命令
ls ~/.sofarpc/cache/schema/projects/  # 看到新生成的 cache,schemaVersion="4"
```

- [ ] **Step 4: 看 git status 与 commits ahead of main**

Run: `git status && git log main..HEAD --oneline | head -20`

预期最终新增 / 修改文件汇总(10 commit + 1 docs commit):
```
M  internal/schema/schema.go
M  internal/schema/cache.go
M  internal/schema/schema_test.go
A  internal/schema/adapter_javaparser.go
A  internal/schema/adapter_javaparser_test.go
M  internal/schema/parser_golden_test.go
A  internal/schema/testdata/golden/wildcard/...
A  internal/schema/testdata/golden/inner/...
M  internal/app/rpc_types.go
M  internal/app/rpc_types_test.go
A  docs/plans/2026-05-27-java-declaration-parser-c3-adapter-cutover.md
```

**不能动**:`internal/javaparser/`(C.1+C.2 已 ship)、`internal/app/invoke.go`、`internal/app/types.go`、`internal/mcp/*`、`internal/direct/*`、`internal/javavalue/*`、`internal/cli/*` —— 所有下游对 schema.Method / TypeSchema 的访问都是 struct 字段读,新 TypeParams 字段 nil 时退化老行为,零修改。

- [ ] **Step 5: 最终 commit(plan 文档)**

```bash
git add docs/plans/2026-05-27-java-declaration-parser-c3-adapter-cutover.md
git commit -m "docs: 加 C.3 Java declaration parser adapter + cutover plan"
```

---

## Verification

完成全部 Task 后:

```bash
go test ./internal/schema/ -v       # 全 PASS,新加 wildcard / inner case 在内
go test ./internal/schema/ -cover   # coverage ≥ 80%
go test ./internal/app/ -v          # rpc_types P3 test 全 PASS,Plan B test 全 PASS
go test ./...                       # 全项目 PASS,无 regression
go vet ./...                        # 无 warning
go build ./...                      # 无 error
```

完成标志:
- `schema.Method` / `schema.TypeSchema` 多了 `TypeParams` 字段(JSON omitempty)
- `internal/schema/adapter_javaparser.go` 完整接管 .java 解析,老 regex parser 代码删完(只剩 `cleanType` / `eraseGeneric` / `splitCommaAware` / `isControlKeyword` 4 个 helper 还被 schema 内部 resolver 用)
- `BuildIndex` 2-pass,wildcard import 正确展开
- `internal/app/rpc_types.go` 的 `resolveBaseType / rpcParamTypeForMethod / rpcFieldTypeForType` 在 pkg fallback 之前精确查 TypeParams,根治 Plan B P3 limitation
- 新 3 个 golden case 覆盖 wildcard / inner class / pollution-guard
- 老 schema cache(version "3")自动失效重建为 version "4"
- 下游 `internal/app/*` / `internal/mcp/*` / `internal/direct/*` 零修改,接口完全兼容

## Out of Scope(留作 follow-up)

- **真正的 nested type FQN**:当前沿用 flat keying(`pkg.Inner` 而非 `pkg.Outer.Inner`),与既有 `resolveType` 兼容。 真正的 `Outer.Inner` FQN 支持需要扩 `resolveType`,scope 蔓延,留 follow-up。
- **Static wildcard import**(`import static a.b.C.*`):当前 `extractImports` 不展开 —— 没有 method-level 数据可用。 真业务 facade 几乎不出现,defer。
- **嵌套 method body parsing**:scope charter 明确不解析。 method body 的实现细节(返回值表达式、内部调用)对 schema 无用,permanent OOS。
- **Generic-qualified inner type `Outer<T>.Inner<U>`**(继承自 C.2 OOS):TypeRef 不支持这种 segmented 表达,真业务 facade 罕见。
- **TypeRef 的真正 segmented representation**(继承自 C.2 OOS):当前用 flat dotted string 配合 `resolveType` 解析。 C.3 adapter 接入后压力是否真有,等 production dogfood 反馈。
- **基于 javaparser 的 `resolveType` 重构**:当前 `cleanType / eraseGeneric / splitCommaAware` 还用着,处理已经组装好的 `TypeRef.String()` 字符串(非源码)。 future plan 可把 `resolveType` 也改成走 AST,把这 3 个 helper 删干净。

## What Comes Next

C.3 完成后,javaparser 在 sofarpc-cli 内的接入完整闭环:
- C.1 lexer + C.2 parser + C.3 adapter = pure-Go Java declaration indexer
- 下游所有 RPC 工具(invoke / describe / mcp tools)透明地从 javaparser 输出受益
- Plan B 的 P3 limitation 根治

**真正的下一步**(不在本 plan 范围):
- **Dogfood validate**:用 fundsalesmrksupport-test 真打 MaterialBgFacade / AcActivityBgFacade,确认 wildcard / nested type / P3 edge case 在 production 全部 work
- **Performance bench**:javaparser parse + adapter 比老 regex parser 慢多少?如果显著(>2x),BuildIndex 加 worker pool 并行 parse
- **Schema 包 `resolveType` 重构**:把剩余 3 个 cleanType/eraseGeneric/splitCommaAware helper 也用 AST 替换
- **真 nested type FQN**:`com.x.dto.Outer$Inner` 形态(对齐 Java reflection)


