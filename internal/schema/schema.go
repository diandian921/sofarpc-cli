package schema

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"github.com/diandian921/sofarpc-mcp/internal/javaparser"
)

type Project struct {
	Name            string
	WorkspaceRoot   string
	ServicePrefixes []string
}

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

type Parameter struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

type TypeSchema struct {
	Type       string            `json:"type"`
	Kind       string            `json:"kind"`
	Fields     []Field           `json:"fields,omitempty"`
	EnumValues []string          `json:"enumValues,omitempty"`
	Unresolved bool              `json:"unresolved,omitempty"`
	SourceFile string            `json:"sourceFile,omitempty"`
	Imports    map[string]string `json:"imports,omitempty"`
	// Extends lists the direct supertype refs as written (a class has at most one;
	// kept as a slice for interfaces). Describe follows these to surface inherited
	// fields, which Hessian serializes. Empty for types with no superclass.
	Extends []string `json:"extends,omitempty"`
	// TypeParams 是 class declared type parameters 的简单名列表(`class Page<T, K>` → ["T", "K"])。
	// rpc_types.go 用它精确识别 type variable(Plan B P3 fix)。
	TypeParams []string `json:"typeParams,omitempty"`
}

type Field struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

type Description struct {
	Service    string                 `json:"service"`
	Methods    []Method               `json:"methods"`
	Types      map[string]TypeSchema  `json:"types,omitempty"`
	Warnings   []string               `json:"warnings,omitempty"`
	SourceRoot string                 `json:"sourceRoot,omitempty"`
	Stats      map[string]interface{} `json:"stats,omitempty"`
}

type Index struct {
	Project Project
	Methods []Method
	Types   map[string]TypeSchema
}

// BuildIndex 走 2 pass:
//
//	Pass 1: walk + parse 所有 .java 文件,收集全工程 type FQN 集合
//	Pass 2: in-memory 调 adapter,wildcard import 用 Pass 1 的集合展开
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
// 收集顶层 type FQN 进 topLevelFQNs(用于 wildcard import 展开);nested 不在 topLevel。
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

func DiscoverSourceRoots(workspace string) ([]string, error) {
	info, err := os.Stat(workspace)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", workspace)
	}
	seen := map[string]bool{}
	var roots []string
	err = filepath.WalkDir(workspace, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() {
			return nil
		}
		if path != workspace && shouldIgnoreDir(d.Name()) {
			return filepath.SkipDir
		}
		rel, _ := filepath.Rel(workspace, path)
		if rel != "." && strings.Count(rel, string(os.PathSeparator)) > 8 {
			return filepath.SkipDir
		}
		if filepath.ToSlash(rel) == "src/main/java" || strings.HasSuffix(filepath.ToSlash(rel), "/src/main/java") {
			if !seen[path] {
				seen[path] = true
				roots = append(roots, path)
			}
			return filepath.SkipDir
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(roots)
	return roots, nil
}

func Describe(idx *Index, service string, methodFilter string) (Description, error) {
	desc := Description{Service: service, Types: map[string]TypeSchema{}, Stats: map[string]interface{}{}}
	for _, method := range idx.Methods {
		if method.Service != service {
			continue
		}
		if methodFilter != "" && method.Method != methodFilter {
			continue
		}
		desc.Methods = append(desc.Methods, method)
		for _, typ := range referencedTypes(method.ReturnType) {
			if schema, ok := resolveType(idx, typ, method.Package, method.Imports); ok {
				addDescribedType(idx, desc.Types, schema)
			}
		}
		for _, p := range method.Parameters {
			for _, typ := range referencedTypes(p.Type) {
				if schema, ok := resolveType(idx, typ, method.Package, method.Imports); ok {
					addDescribedType(idx, desc.Types, schema)
				}
			}
		}
	}
	if len(desc.Methods) == 0 {
		return desc, fmt.Errorf("service %q not found", service)
	}
	desc.Stats["methodCount"] = len(desc.Methods)
	desc.Stats["typeCount"] = len(desc.Types)
	return desc, nil
}

func addDescribedType(idx *Index, out map[string]TypeSchema, schema TypeSchema) {
	if schema.Type == "" {
		return
	}
	if _, exists := out[schema.Type]; exists {
		return
	}
	out[schema.Type] = schema
	pkg := packageFromType(schema.Type)
	// Follow the superclass chain so inherited fields are visible: Hessian
	// serializes them, and an agent walks leaf.Extends -> desc.Types[base]. The
	// base ref is written in this type's file, so resolve it with this type's
	// package and imports.
	for _, base := range schema.Extends {
		for _, typ := range referencedTypes(base) {
			parent, ok := resolveType(idx, typ, pkg, schema.Imports)
			if !ok {
				continue
			}
			addDescribedType(idx, out, parent)
		}
	}
	for _, field := range schema.Fields {
		for _, typ := range referencedTypes(field.Type) {
			child, ok := resolveType(idx, typ, pkg, schema.Imports)
			if !ok {
				continue
			}
			addDescribedType(idx, out, child)
		}
	}
}

func packageFromType(fqn string) string {
	if i := strings.LastIndex(fqn, "."); i >= 0 {
		return fqn[:i]
	}
	return ""
}

func resolveType(idx *Index, typ string, pkg string, imports map[string]string) (TypeSchema, bool) {
	base := eraseGeneric(cleanType(typ))
	if isBuiltin(base) {
		return TypeSchema{Type: base, Kind: "builtin"}, true
	}
	if schema, ok := idx.Types[base]; ok {
		return schema, true
	}
	if !strings.Contains(base, ".") {
		if imported, ok := imports[base]; ok {
			if schema, ok := idx.Types[imported]; ok {
				return schema, true
			}
			return TypeSchema{Type: imported, Kind: "external", Unresolved: true}, true
		}
		fqn := pkg + "." + base
		if schema, ok := idx.Types[fqn]; ok {
			return schema, true
		}
		return TypeSchema{Type: base, Kind: "external", Unresolved: true}, true
	}
	return TypeSchema{Type: base, Kind: "external", Unresolved: true}, true
}

func referencedTypes(typ string) []string {
	seen := map[string]bool{}
	var out []string
	var current []rune
	flush := func() {
		if len(current) == 0 {
			return
		}
		token := string(current)
		current = nil
		if token == "" || seen[token] || isBuiltin(token) {
			return
		}
		seen[token] = true
		out = append(out, token)
	}
	for _, r := range typ {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '.' {
			current = append(current, r)
		} else {
			flush()
		}
	}
	flush()
	return out
}

// cleanType / eraseGeneric 仍被 resolveType / referencedTypes 用着:它们处理 schema
// 内部已组装好的 TypeRef.String() 字符串(不再用于解析源码)。 等 resolveType 完全迁到
// javaparser 时这两个也可删。 splitCommaAware 在 regex parser 退场后已无调用方,已移除。
func cleanType(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "final ")
	s = strings.ReplaceAll(s, "\n", "")
	s = strings.Join(strings.Fields(s), " ")
	return strings.TrimSpace(s)
}

func eraseGeneric(s string) string {
	if idx := strings.Index(s, "<"); idx >= 0 {
		return strings.TrimSpace(s[:idx])
	}
	return strings.TrimSuffix(s, "[]")
}

func matchesAnyPrefix(service string, prefixes []string) bool {
	if len(prefixes) == 0 {
		return true
	}
	for _, prefix := range prefixes {
		if strings.HasPrefix(service, prefix) {
			return true
		}
	}
	return false
}

func shouldIgnoreDir(name string) bool {
	switch name {
	case "target", "build", ".git", ".idea", "node_modules":
		return true
	default:
		return false
	}
}

func isControlKeyword(s string) bool {
	switch s {
	case "if", "for", "while", "switch", "catch", "return", "new":
		return true
	default:
		return false
	}
}

func isBuiltin(s string) bool {
	switch s {
	case "void", "boolean", "byte", "short", "int", "long", "float", "double", "char",
		"Boolean", "Byte", "Short", "Integer", "Long", "Float", "Double", "Character",
		"String", "Object", "java.lang.String", "java.math.BigDecimal", "java.util.Date",
		"Date", "BigDecimal":
		return true
	default:
		return false
	}
}
