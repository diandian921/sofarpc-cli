package app

import (
	"strings"

	"github.com/diandian921/sofarpc-mcp/internal/schema"
)

// ownedField pairs a field with its declaring type, so an inherited field's type
// resolves against the class that declared it (its own imports/package), not the
// leaf subclass.
type ownedField struct {
	field schema.Field
	owner schema.TypeSchema
}

// collectFieldsWithInherited returns a class's own fields followed by those of its
// superclasses, walked via TypeSchema.Extends and resolved within types, so invoke
// type-coerces inherited fields instead of falling back to an empty type (which
// mis-encodes numbers / dates / enums). seen breaks inheritance cycles. Own fields
// come first so the caller keeps the subclass field when names collide. A
// parameterized superclass (`Child extends Base<OrderStatus>`) is substituted, so
// an inherited field typed as the type variable `T` becomes `OrderStatus`.
func collectFieldsWithInherited(typ schema.TypeSchema, types map[string]schema.TypeSchema, seen map[string]bool) []ownedField {
	return collectFieldsSubst(typ, types, seen, nil)
}

// collectFieldsSubst is collectFieldsWithInherited carrying the type-variable
// binding for the class being visited (nil at the leaf). It applies subst to each
// field type and composes the binding when descending into a generic superclass.
func collectFieldsSubst(typ schema.TypeSchema, types map[string]schema.TypeSchema, seen map[string]bool, subst map[string]string) []ownedField {
	if typ.Type == "" || seen[typ.Type] {
		return nil
	}
	seen[typ.Type] = true
	out := make([]ownedField, 0, len(typ.Fields))
	for _, f := range typ.Fields {
		ft := f.Type
		if len(subst) > 0 {
			ft = substituteTypeVars(ft, subst)
		}
		out = append(out, ownedField{field: schema.Field{Name: f.Name, Type: ft}, owner: typ})
	}
	for _, base := range typ.Extends {
		parent, ok := resolveExtendsType(base, typ, types)
		if !ok {
			continue
		}
		out = append(out, collectFieldsSubst(parent, types, seen, parentSubst(base, parent, typ, types, subst))...)
	}
	return out
}

// parentSubst maps the parent's type-variable names to the concrete types the
// subclass binds them to (`extends Base<OrderStatus>` -> {T: <FQN of OrderStatus>}).
// inbound (the child's own binding) is applied to the extends ref first so nested
// inheritance composes (Child<U> extends Base<U> with U already bound). A concrete
// arg is resolved to its FQN so downstream resolution is context-free; an
// unresolved arg / type variable is left as-is (degrades to the unsubstituted
// behavior). Returns nil when the parent is non-generic or the arity does not line up.
func parentSubst(baseRef string, parent, child schema.TypeSchema, types map[string]schema.TypeSchema, inbound map[string]string) map[string]string {
	if len(parent.TypeParams) == 0 {
		return nil
	}
	ref := baseRef
	if len(inbound) > 0 {
		ref = substituteTypeVars(ref, inbound)
	}
	args := extractGenericArgs(ref)
	if len(args) != len(parent.TypeParams) {
		return nil
	}
	m := make(map[string]string, len(args))
	for i, p := range parent.TypeParams {
		m[p] = resolveTypeTokensToFQN(strings.TrimSpace(args[i]), child, types)
	}
	return m
}

// resolveTypeTokensToFQN resolves every bare type name in a (possibly generic or
// array) type string to its FQN using owner's imports/package, preserving the
// structure — so `Page<OrderStatus>` becomes `com.x.Page<com.x.OrderStatus>`
// instead of being erased to the raw base by resolveExtendsType. Builtins,
// already-qualified names, and unresolved tokens (e.g. type variables) are kept.
func resolveTypeTokensToFQN(ref string, owner schema.TypeSchema, types map[string]schema.TypeSchema) string {
	var b, ident strings.Builder
	flush := func() {
		if ident.Len() == 0 {
			return
		}
		name := ident.String()
		ident.Reset()
		if !strings.Contains(name, ".") {
			if t, ok := resolveExtendsType(name, owner, types); ok && t.Type != "" {
				b.WriteString(t.Type)
				return
			}
		}
		b.WriteString(name)
	}
	for _, r := range ref {
		if isIdentRune(r) {
			ident.WriteRune(r)
			continue
		}
		flush()
		b.WriteRune(r)
	}
	flush()
	return b.String()
}

// substituteTypeVars replaces whole-identifier type variables in a type string
// using subst, e.g. {T: OrderStatus} turns "List<T>" into "List<OrderStatus>".
// Dotted FQNs are single tokens, so a single-letter key never matches inside one.
func substituteTypeVars(typ string, subst map[string]string) string {
	var b, ident strings.Builder
	flush := func() {
		if ident.Len() == 0 {
			return
		}
		s := ident.String()
		ident.Reset()
		if repl, ok := subst[s]; ok {
			b.WriteString(repl)
		} else {
			b.WriteString(s)
		}
	}
	for _, r := range typ {
		if isIdentRune(r) {
			ident.WriteRune(r)
			continue
		}
		flush()
		b.WriteRune(r)
	}
	flush()
	return b.String()
}

func isIdentRune(r rune) bool {
	return r == '_' || r == '$' || r == '.' ||
		(r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
}

// resolveExtendsType resolves a superclass ref (as written, e.g. "BaseDTO" or a
// generic "Base<String>") to its TypeSchema within types, using the subclass's
// imports/package — mirroring schema.resolveType over the described type map.
func resolveExtendsType(ref string, owner schema.TypeSchema, types map[string]schema.TypeSchema) (schema.TypeSchema, bool) {
	base := eraseRPCGeneric(ref)
	if t, ok := types[base]; ok {
		return t, true
	}
	if strings.Contains(base, ".") {
		return schema.TypeSchema{}, false
	}
	if fqn, ok := owner.Imports[base]; ok {
		if t, ok := types[fqn]; ok {
			return t, true
		}
	}
	if lastDot := strings.LastIndex(owner.Type, "."); lastDot > 0 {
		if t, ok := types[owner.Type[:lastDot]+"."+base]; ok {
			return t, true
		}
	}
	return schema.TypeSchema{}, false
}
