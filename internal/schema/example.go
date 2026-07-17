package schema

import "strings"

// exampleMaxDepth is a final backstop against pathological DTO nesting; the
// path-scoped seen set (below) already cuts real cycles, this only guards against
// anything the seen set misses.
const (
	exampleMaxDepth = 8
	// exampleMaxNodes bounds the complete skeleton, across every top-level
	// parameter and recursive branch. Depth alone does not protect against a
	// shallow DTO with thousands of fields.
	exampleMaxNodes = 1024
)

type exampleTraversal struct {
	idx       *Index
	remaining int
	seen      map[string]bool
}

func (t *exampleTraversal) takeNode() bool {
	if t.remaining <= 0 {
		return false
	}
	t.remaining--
	return true
}

// ExampleArgumentsFor builds a named example-argument skeleton for a method:
// { paramName: <placeholder shaped like the parameter type> }. Placeholders are
// STRUCTURAL, not business-valid (String -> "", numbers -> 0, enum -> its first
// value); an agent copies the skeleton and fills real values before invoking.
//
// Nested DTOs expand recursively (inherited fields included). A self- or
// mutually-recursive DTO is cut with null on re-entry — seen is path-scoped
// (added on the way down, removed on the way back up), so a DTO appearing twice
// as siblings still expands fully; only a true ancestor cycle is cut. Depth is
// bounded as a last resort. Returns nil for a no-parameter method.
func ExampleArgumentsFor(idx *Index, method Method) map[string]interface{} {
	if len(method.Parameters) == 0 {
		return nil
	}
	traversal := &exampleTraversal{
		idx:       idx,
		remaining: exampleMaxNodes,
		seen:      map[string]bool{},
	}
	out := make(map[string]interface{}, len(method.Parameters))
	for _, p := range method.Parameters {
		out[p.Name] = traversal.value(p.Type, method.Package, method.Imports, 0)
	}
	return out
}

func (t *exampleTraversal) value(typ, pkg string, imports map[string]string, depth int) interface{} {
	if depth > exampleMaxDepth || !t.takeNode() {
		return nil
	}
	javaType := cleanType(typ)
	if javaType == "" {
		return nil
	}
	// Arrays: byte[] is Hessian binary (a string placeholder); other X[] -> [X].
	if strings.HasSuffix(javaType, "[]") {
		elem := strings.TrimSpace(strings.TrimSuffix(javaType, "[]"))
		if shortName(elem) == "byte" || shortName(elem) == "Byte" {
			return ""
		}
		return []interface{}{t.value(elem, pkg, imports, depth+1)}
	}
	base := eraseGeneric(javaType)
	// Collections -> [element]; maps -> {} (keys are runtime-specific, leave empty).
	if isCollectionType(base) {
		if args := genericArgs(javaType); len(args) >= 1 {
			return []interface{}{t.value(args[len(args)-1], pkg, imports, depth+1)}
		}
		return []interface{}{}
	}
	if isMapType(base) {
		return map[string]interface{}{}
	}
	resolved, ok := resolveType(t.idx, base, pkg, imports)
	if !ok {
		return nil
	}
	// Special scalars (java.time / BigInteger / BigDecimal / Date) are not
	// isBuiltin, so check the resolved FQN before the builtin/external branches.
	if ex, ok := specialExample(resolved.Type); ok {
		return ex
	}
	switch resolved.Kind {
	case "builtin":
		return builtinExample(resolved.Type)
	case "enum":
		if len(resolved.EnumValues) > 0 {
			return resolved.EnumValues[0]
		}
		return ""
	case "class":
		fqn := resolved.Type
		if t.seen[fqn] {
			return nil // ancestor cycle: cut with null
		}
		t.seen[fqn] = true
		defer delete(t.seen, fqn)
		obj := make(map[string]interface{})
		for _, of := range exampleFields(t.idx, resolved, map[string]bool{}) {
			if t.remaining <= 0 {
				break
			}
			if _, dup := obj[of.field.Name]; dup {
				continue
			}
			obj[of.field.Name] = t.value(of.field.Type, of.pkg, of.imports, depth+1)
		}
		return obj
	default: // external / unresolved DTO — cannot expand
		return nil
	}
}

// exampleOwnedField pairs a field with the package/imports of the type that
// declares it, so the field's type resolves in the right context (a field
// inherited from a parent in another package uses the parent's imports).
type exampleOwnedField struct {
	field   Field
	pkg     string
	imports map[string]string
}

// exampleFields collects a DTO's fields plus inherited fields up the Extends
// chain, child-first with subclass fields shadowing same-named inherited ones.
// chainSeen guards against a cyclic superclass chain.
func exampleFields(idx *Index, s TypeSchema, chainSeen map[string]bool) []exampleOwnedField {
	if s.Type == "" || chainSeen[s.Type] {
		return nil
	}
	chainSeen[s.Type] = true
	pkg := packageFromType(s.Type)
	seenName := map[string]bool{}
	var out []exampleOwnedField
	for _, f := range s.Fields {
		if seenName[f.Name] {
			continue
		}
		seenName[f.Name] = true
		out = append(out, exampleOwnedField{field: f, pkg: pkg, imports: s.Imports})
	}
	for _, base := range s.Extends {
		for _, typ := range referencedTypes(base) {
			parent, ok := resolveType(idx, typ, pkg, s.Imports)
			if !ok || parent.Kind != "class" {
				continue
			}
			for _, pf := range exampleFields(idx, parent, chainSeen) {
				if seenName[pf.field.Name] {
					continue
				}
				seenName[pf.field.Name] = true
				out = append(out, pf)
			}
		}
	}
	return out
}

// specialExample returns the placeholder for a Hessian special scalar, keyed by
// short name or FQN. Kept in sync with the direct writer's special types.
func specialExample(t string) (interface{}, bool) {
	switch t {
	case "java.time.LocalDate", "LocalDate":
		return "2024-01-01", true
	case "java.time.LocalDateTime", "LocalDateTime":
		return "2024-01-01T00:00:00", true
	case "java.time.LocalTime", "LocalTime":
		return "00:00:00", true
	case "java.time.Instant", "Instant":
		return "2024-01-01T00:00:00Z", true
	case "java.math.BigInteger", "BigInteger":
		return "0", true
	case "java.math.BigDecimal", "BigDecimal":
		return "0", true
	case "java.util.Date", "Date":
		return 0, true
	}
	return nil, false
}

// builtinExample returns the placeholder for a primitive / boxed / String type.
func builtinExample(t string) interface{} {
	switch t {
	case "boolean", "Boolean", "java.lang.Boolean":
		return false
	case "byte", "short", "int", "long", "Byte", "Short", "Integer", "Long",
		"java.lang.Byte", "java.lang.Short", "java.lang.Integer", "java.lang.Long":
		return 0
	case "float", "double", "Float", "Double", "java.lang.Float", "java.lang.Double":
		return 0
	case "char", "Character", "java.lang.Character", "String", "java.lang.String":
		return ""
	default: // void / Object / unknown
		return nil
	}
}

func shortName(t string) string {
	if i := strings.LastIndex(t, "."); i >= 0 {
		return t[i+1:]
	}
	return t
}

func isCollectionType(base string) bool {
	switch shortName(base) {
	case "List", "ArrayList", "LinkedList", "Set", "HashSet", "TreeSet",
		"LinkedHashSet", "Collection", "Iterable", "Queue", "Deque":
		return true
	}
	return false
}

func isMapType(base string) bool {
	switch shortName(base) {
	case "Map", "HashMap", "LinkedHashMap", "TreeMap", "ConcurrentHashMap",
		"SortedMap", "NavigableMap":
		return true
	}
	return false
}

// genericArgs returns the top-level type arguments inside the outermost <...> of
// t, e.g. "Map<Long, List<Foo>>" -> ["Long", "List<Foo>"]. Nil when t has no
// generic arguments. This is example-skeleton local; wire encoding uses
// app.extractGenericArgs (different layer/concern).
func genericArgs(t string) []string {
	open := strings.Index(t, "<")
	if open < 0 {
		return nil
	}
	depth, end := 0, -1
	for i := open; i < len(t); i++ {
		switch t[i] {
		case '<':
			depth++
		case '>':
			depth--
			if depth == 0 {
				end = i
			}
		}
		if end >= 0 {
			break
		}
	}
	if end < 0 {
		return nil
	}
	inner := t[open+1 : end]
	var args []string
	d, start := 0, 0
	for i, r := range inner {
		switch r {
		case '<':
			d++
		case '>':
			d--
		case ',':
			if d == 0 {
				args = append(args, strings.TrimSpace(inner[start:i]))
				start = i + 1
			}
		}
	}
	args = append(args, strings.TrimSpace(inner[start:]))
	return args
}
