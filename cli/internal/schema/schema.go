package schema

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

type Project struct {
	Name            string
	WorkspaceRoot   string
	ServicePrefixes []string
}

type Method struct {
	Service       string            `json:"service"`
	Interface     string            `json:"interface"`
	Package       string            `json:"package"`
	Method        string            `json:"method"`
	ReturnType    string            `json:"returnType"`
	Parameters    []Parameter       `json:"parameters"`
	Summary       string            `json:"summary,omitempty"`
	SourceFile    string            `json:"sourceFile"`
	Score         int               `json:"score,omitempty"`
	Evidence      []string          `json:"evidence,omitempty"`
	OutOfPrefix   bool              `json:"outOfPrefix,omitempty"`
	SourceHash    string            `json:"sourceHash,omitempty"`
	ParseWarnings []string          `json:"parseWarnings,omitempty"`
	Imports       map[string]string `json:"imports,omitempty"`
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

var (
	packageRE   = regexp.MustCompile(`(?m)^\s*package\s+([A-Za-z_][\w.]*)\s*;`)
	importRE    = regexp.MustCompile(`(?m)^\s*import\s+([A-Za-z_][\w.]*\.[A-Za-z_]\w*)\s*;`)
	typeRE      = regexp.MustCompile(`(?m)\b(?:public\s+)?(?:interface|class|enum)\s+([A-Za-z_]\w*)\b`)
	typeKindRE  = regexp.MustCompile(`(?m)\b(?:public\s+)?(interface|class|enum)\s+([A-Za-z_]\w*)\b`)
	methodRE    = regexp.MustCompile(`(?s)(/\*\*.*?\*/)?\s*(?:public\s+)?(?:default\s+)?(?:static\s+)?(?:<[^;{}()]+>\s*)?([A-Za-z_][\w.<>\[\]?,\s]*?)\s+([A-Za-z_]\w*)\s*\(([^{};]*)\)\s*(?:;|\{)`)
	fieldRE     = regexp.MustCompile(`(?m)^\s*(?:private|protected|public)\s+(?:static\s+)?(?:final\s+)?([A-Za-z_][\w.<>\[\]?,\s]*)\s+([A-Za-z_]\w*)\s*(?:=|;)`)
	enumValueRE = regexp.MustCompile(`(?s)\benum\s+[A-Za-z_]\w*\s*\{(.*?)\}`)
)

func BuildIndex(project Project) (*Index, error) {
	roots, err := DiscoverSourceRoots(project.WorkspaceRoot)
	if err != nil {
		return nil, err
	}
	idx := &Index{Project: project, Types: map[string]TypeSchema{}}
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
			methods, types := parseJavaFile(path, project.ServicePrefixes)
			idx.Methods = append(idx.Methods, methods...)
			for fqn, typ := range types {
				idx.Types[fqn] = typ
			}
			return nil
		})
		if err != nil {
			return nil, err
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

func Search(idx *Index, query string, limit int, includeOutOfPrefix bool) []Method {
	if limit <= 0 {
		limit = 5
	}
	if limit > 20 {
		limit = 20
	}
	queryTokens := Tokenize(query)
	var scored []Method
	for _, method := range idx.Methods {
		if method.OutOfPrefix && !includeOutOfPrefix {
			continue
		}
		score, evidence := scoreMethod(method, queryTokens)
		if score == 0 && strings.TrimSpace(query) != "" {
			continue
		}
		m := method
		m.Score = score
		m.Evidence = evidence
		scored = append(scored, m)
	}
	sort.Slice(scored, func(i, j int) bool {
		if scored[i].Score == scored[j].Score {
			if scored[i].Service == scored[j].Service {
				return scored[i].Method < scored[j].Method
			}
			return scored[i].Service < scored[j].Service
		}
		return scored[i].Score > scored[j].Score
	})
	if len(scored) > limit {
		scored = scored[:limit]
	}
	return scored
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
				desc.Types[schema.Type] = schema
			}
		}
		for _, p := range method.Parameters {
			for _, typ := range referencedTypes(p.Type) {
				if schema, ok := resolveType(idx, typ, method.Package, method.Imports); ok {
					desc.Types[schema.Type] = schema
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

func Tokenize(s string) []string {
	seen := map[string]bool{}
	add := func(token string, out *[]string) {
		token = strings.ToLower(strings.TrimSpace(token))
		if token == "" || seen[token] {
			return
		}
		seen[token] = true
		*out = append(*out, token)
	}
	var out []string
	for _, part := range splitIdentifier(s) {
		add(part, &out)
		if containsCJK(part) {
			runes := []rune(part)
			if len(runes) > 1 {
				for i := 0; i < len(runes)-1; i++ {
					add(string(runes[i:i+2]), &out)
				}
			}
		}
	}
	return out
}

func parseJavaFile(path string, prefixes []string) ([]Method, map[string]TypeSchema) {
	bodyBytes, err := os.ReadFile(path)
	if err != nil {
		return nil, nil
	}
	body := string(bodyBytes)
	pkg := firstSubmatch(packageRE, body)
	imports := parseImports(body)
	kind, typeName := firstTypeKind(body)
	if pkg == "" || typeName == "" {
		return nil, nil
	}
	fqn := pkg + "." + typeName
	hash := sha256.Sum256(bodyBytes)
	sourceHash := hex.EncodeToString(hash[:])[:16]
	var methods []Method
	if kind == "interface" {
		methods = parseMethods(body, pkg, fqn, typeName, path, sourceHash, prefixes, imports)
	}
	types := parseTypes(body, pkg, typeName, path, imports)
	return methods, types
}

func parseMethods(body, pkg, service, iface, path, sourceHash string, prefixes []string, imports map[string]string) []Method {
	matches := methodRE.FindAllStringSubmatch(body, -1)
	var methods []Method
	for _, m := range matches {
		if len(m) != 5 {
			continue
		}
		name := strings.TrimSpace(m[3])
		if isControlKeyword(name) {
			continue
		}
		method := Method{
			Service:     service,
			Interface:   iface,
			Package:     pkg,
			Method:      name,
			ReturnType:  cleanType(m[2]),
			Parameters:  parseParameters(m[4]),
			Summary:     cleanJavadoc(m[1]),
			SourceFile:  path,
			SourceHash:  sourceHash,
			OutOfPrefix: !matchesAnyPrefix(service, prefixes),
			Imports:     imports,
		}
		methods = append(methods, method)
	}
	return methods
}

func parseTypes(body, pkg, typeName, path string, imports map[string]string) map[string]TypeSchema {
	out := map[string]TypeSchema{}
	kindMatches := typeKindRE.FindAllStringSubmatch(body, -1)
	for _, m := range kindMatches {
		if len(m) != 3 {
			continue
		}
		fqn := pkg + "." + m[2]
		schema := TypeSchema{Type: fqn, Kind: m[1], SourceFile: path, Imports: imports}
		if m[1] == "enum" {
			schema.EnumValues = parseEnumValues(body)
		} else {
			schema.Fields = parseFields(body)
		}
		out[fqn] = schema
	}
	if _, ok := out[pkg+"."+typeName]; !ok {
		out[pkg+"."+typeName] = TypeSchema{Type: pkg + "." + typeName, Kind: "class", Fields: parseFields(body), SourceFile: path, Imports: imports}
	}
	return out
}

func parseParameters(raw string) []Parameter {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := splitCommaAware(raw)
	params := make([]Parameter, 0, len(parts))
	for i, part := range parts {
		part = strings.TrimSpace(stripAnnotations(part))
		fields := strings.Fields(part)
		if len(fields) == 0 {
			continue
		}
		name := fmt.Sprintf("arg%d", i)
		typ := strings.Join(fields, " ")
		if len(fields) >= 2 {
			name = strings.TrimPrefix(fields[len(fields)-1], "...")
			typ = strings.Join(fields[:len(fields)-1], " ")
		}
		params = append(params, Parameter{Name: name, Type: cleanType(typ)})
	}
	return params
}

func parseFields(body string) []Field {
	matches := fieldRE.FindAllStringSubmatch(body, -1)
	var fields []Field
	for _, m := range matches {
		if len(m) != 3 || isControlKeyword(m[2]) {
			continue
		}
		fields = append(fields, Field{Name: m[2], Type: cleanType(m[1])})
	}
	return fields
}

func parseEnumValues(body string) []string {
	m := enumValueRE.FindStringSubmatch(body)
	if len(m) != 2 {
		return nil
	}
	beforeSemicolon := strings.Split(m[1], ";")[0]
	parts := splitCommaAware(beforeSemicolon)
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		p := strings.TrimSpace(part)
		if p == "" {
			continue
		}
		if idx := strings.Index(p, "("); idx >= 0 {
			p = p[:idx]
		}
		if p != "" {
			values = append(values, p)
		}
	}
	return values
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

func parseImports(body string) map[string]string {
	out := map[string]string{}
	matches := importRE.FindAllStringSubmatch(body, -1)
	for _, m := range matches {
		if len(m) != 2 {
			continue
		}
		fqn := m[1]
		if lastDot := strings.LastIndex(fqn, "."); lastDot >= 0 {
			out[fqn[lastDot+1:]] = fqn
		}
	}
	return out
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

func scoreMethod(method Method, queryTokens []string) (int, []string) {
	if len(queryTokens) == 0 {
		return 1, nil
	}
	haystack := strings.Join(append(Tokenize(method.Service+" "+method.Interface+" "+method.Method+" "+method.Summary), strings.ToLower(method.Service), strings.ToLower(method.Method)), " ")
	score := 0
	var evidence []string
	for _, token := range queryTokens {
		if strings.Contains(haystack, token) {
			score += 10
			evidence = append(evidence, token)
		}
	}
	if strings.Contains(strings.ToLower(method.Method), strings.ToLower(strings.Join(queryTokens, ""))) {
		score += 20
	}
	return score, evidence
}

func splitIdentifier(s string) []string {
	var out []string
	var current []rune
	flush := func() {
		if len(current) > 0 {
			out = append(out, string(current))
			current = nil
		}
	}
	var prev rune
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || isCJK(r) {
			if len(current) > 0 && unicode.IsUpper(r) && (unicode.IsLower(prev) || unicode.IsDigit(prev)) {
				flush()
			}
			current = append(current, r)
			prev = r
			continue
		}
		flush()
		prev = 0
	}
	flush()
	return out
}

func splitCommaAware(raw string) []string {
	var parts []string
	depth := 0
	start := 0
	for i, r := range raw {
		switch r {
		case '<', '(', '[':
			depth++
		case '>', ')', ']':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				parts = append(parts, raw[start:i])
				start = i + len(string(r))
			}
		}
	}
	parts = append(parts, raw[start:])
	return parts
}

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

func cleanJavadoc(raw string) string {
	raw = strings.TrimSpace(raw)
	if idx := strings.LastIndex(raw, "/**"); idx > 0 {
		raw = raw[idx:]
	}
	raw = strings.TrimPrefix(raw, "/**")
	raw = strings.TrimSuffix(raw, "*/")
	lines := strings.Split(raw, "\n")
	var parts []string
	for _, line := range lines {
		line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "*"))
		if line != "" && !strings.HasPrefix(line, "@") {
			parts = append(parts, line)
		}
	}
	return strings.Join(parts, " ")
}

func stripAnnotations(s string) string {
	fields := strings.Fields(s)
	var out []string
	for _, field := range fields {
		if strings.HasPrefix(field, "@") {
			continue
		}
		out = append(out, field)
	}
	return strings.Join(out, " ")
}

func firstSubmatch(re *regexp.Regexp, s string) string {
	m := re.FindStringSubmatch(s)
	if len(m) < 2 {
		return ""
	}
	return m[1]
}

func firstTypeKind(s string) (string, string) {
	m := typeKindRE.FindStringSubmatch(s)
	if len(m) < 3 {
		return "", ""
	}
	return m[1], m[2]
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
	case "target", "build", ".git", ".idea", "node_modules", "src/test/java":
		return true
	default:
		return false
	}
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
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

func containsCJK(s string) bool {
	for _, r := range s {
		if isCJK(r) {
			return true
		}
	}
	return false
}

func isCJK(r rune) bool {
	return unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul)
}
