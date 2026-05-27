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

// pickServiceType 找第一个 interface(老 serviceTypeKind 行为),没 interface 用第一个顶层 type。
// Annotation declaration (@interface) 不算 service type(老 parser 同样跳过)。
func pickServiceType(types []javaparser.TypeDecl) *javaparser.TypeDecl {
	for i := range types {
		if types[i].Kind == javaparser.TypeKindInterface {
			return &types[i]
		}
	}
	for i := range types {
		switch types[i].Kind {
		case javaparser.TypeKindClass, javaparser.TypeKindEnum, javaparser.TypeKindRecord:
			return &types[i]
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
