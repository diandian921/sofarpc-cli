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

	// methods 只在 service 是 interface 时生成(对齐既有 parseJavaFile 行为)
	var methods []Method
	if service.Kind == javaparser.TypeKindInterface {
		methods = emitMethods(service, pkg, fqn, sourcePath, sourceHash, imports, prefixes)
	}
	return methods, out
}

// emitMethods 把 interface 的 MethodDecl 转成 schema.Method 切片。
// 与老 parseMethods 行为对齐:
//   - 跳过 ctor(IsConstructor == true)
//   - Summary 取 MethodDecl.Javadoc(C.2 parsePreamble 已经 cleanJavadocText,直接用)
//   - Service = fqn(interface 全限定名);Interface = service.Name(短名)
//   - OutOfPrefix 用 prefixes 判断
//   - Imports 在文件级 share(每个 method 都引用同一份)
//   - TypeParams = service.TypeParams + MethodDecl.TypeParams 拼接(codex review #7):
//     `interface Facade<T> { T get(); }` 里 method 没有自己的 type params,但 T 是 service
//     级 type variable,rpc_types.go 必须能精确识别 → method.TypeParams 要把 service 的也带上
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

// mergeTypeParams 把 service-level + method-level type param 名拼成去重 slice。
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
// 类型用 typeRefToString;名字 fallback `arg0` / `arg1`(对齐老 parseParameters 在缺名时的 fallback)。
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

// pickServiceType 找 service candidate(对齐老 serviceTypeKind 全文 regex 行为):
//   1. 顶层 interface 优先
//   2. 没顶层 interface 时,**递归 NestedTypes 找第一个 interface**(老 typeKindRE 全文扫
//      会匹配 nested,test TestNestedInterfaceCanBeServiceCandidate 依赖此行为)
//   3. 都没 interface,用第一个顶层 class/enum/record
//
// Annotation declaration (@interface) 不算 service type(老 parser 同样跳过)。
func pickServiceType(types []javaparser.TypeDecl) *javaparser.TypeDecl {
	for i := range types {
		if types[i].Kind == javaparser.TypeKindInterface {
			return &types[i]
		}
	}
	if nested := findNestedInterface(types); nested != nil {
		return nested
	}
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
// Task 5:除 Type / Kind / SourceFile / Imports / TypeParams 外,填 Fields / EnumValues / RecordComponents。
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
