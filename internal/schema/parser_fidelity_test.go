package schema

import (
	"path/filepath"
	"testing"
)

// TestParserFidelityInheritedFields pins that a DTO extending a base class (in a
// different package/file) surfaces the inherited fields: the superclass is linked
// via TypeSchema.Extends and pulled into desc.Types with its own fields. Hessian
// serializes inherited fields, so an agent must be able to see them.
func TestParserFidelityInheritedFields(t *testing.T) {
	root := filepath.Join("testdata", "golden", "inherit")
	idx, err := BuildIndex(Project{
		Name:            "inherit",
		WorkspaceRoot:   root,
		ServicePrefixes: []string{"com.acme.inherit.facade."},
	})
	if err != nil {
		t.Fatalf("BuildIndex: %v", err)
	}
	desc, err := Describe(idx, "com.acme.inherit.facade.OrderFacade", "createOrder")
	if err != nil {
		t.Fatalf("Describe: %v", err)
	}
	order := desc.Types["com.acme.inherit.dto.OrderDTO"]
	if order.Type == "" {
		t.Fatalf("OrderDTO schema missing: %#v", desc.Types)
	}
	assertFields(t, order, map[string]string{"orderId": "String"})

	if !containsString(order.Extends, "BaseDTO") {
		t.Fatalf("OrderDTO.Extends = %#v, want it to reference BaseDTO", order.Extends)
	}
	base := desc.Types["com.acme.inherit.dto.base.BaseDTO"]
	if base.Type == "" {
		t.Fatalf("BaseDTO not pulled into desc.Types (inherited fields invisible): %#v", desc.Types)
	}
	assertFields(t, base, map[string]string{"traceId": "String", "gmtCreate": "Long"})
}

// TestParserFidelityStaticTransientExcluded pins that static and transient fields
// (serialVersionUID, constants, transient caches) never appear in a DTO schema —
// Hessian skips them, so presenting them as arguments misleads the agent. The
// existing assertFields only checks a subset, so this asserts absence explicitly.
func TestParserFidelityStaticTransientExcluded(t *testing.T) {
	root := filepath.Join("testdata", "golden", "modifiers")
	idx, err := BuildIndex(Project{
		Name:            "modifiers",
		WorkspaceRoot:   root,
		ServicePrefixes: []string{"com.acme.modifiers.facade."},
	})
	if err != nil {
		t.Fatalf("BuildIndex: %v", err)
	}
	desc, err := Describe(idx, "com.acme.modifiers.facade.AccountFacade", "getAccount")
	if err != nil {
		t.Fatalf("Describe: %v", err)
	}
	account := desc.Types["com.acme.modifiers.dto.AccountDTO"]
	if account.Type == "" {
		t.Fatalf("AccountDTO schema missing: %#v", desc.Types)
	}
	assertFields(t, account, map[string]string{"mpCode": "Long", "name": "String"})
	for _, f := range account.Fields {
		switch f.Name {
		case "serialVersionUID", "CONST", "cache":
			t.Fatalf("static/transient field %q must be excluded (Hessian skips them); got fields %#v", f.Name, account.Fields)
		}
	}
}

// TestParserFidelityGenericSuperclass pins that a parameterized superclass is
// parsed with its type parameter and binding intact: OrderDTO.Extends carries
// `Base<OrderStatus>`, Base records TypeParams [T] with fields typed as T, and the
// bound enum is reachable. (The T -> OrderStatus substitution itself is exercised
// in the app layer's invoke coercion.)
func TestParserFidelityGenericSuperclass(t *testing.T) {
	root := filepath.Join("testdata", "golden", "genericinherit")
	idx, err := BuildIndex(Project{
		Name:            "genericinherit",
		WorkspaceRoot:   root,
		ServicePrefixes: []string{"com.acme.generic.facade."},
	})
	if err != nil {
		t.Fatalf("BuildIndex: %v", err)
	}
	desc, err := Describe(idx, "com.acme.generic.facade.OrderFacade", "createOrder")
	if err != nil {
		t.Fatalf("Describe: %v", err)
	}
	order := desc.Types["com.acme.generic.dto.OrderDTO"]
	if !containsString(order.Extends, "Base<OrderStatus>") {
		t.Fatalf("OrderDTO.Extends = %#v, want Base<OrderStatus>", order.Extends)
	}
	base := desc.Types["com.acme.generic.dto.base.Base"]
	if base.Type == "" {
		t.Fatalf("Base not pulled into desc.Types: %#v", desc.Types)
	}
	if len(base.TypeParams) != 1 || base.TypeParams[0] != "T" {
		t.Fatalf("Base.TypeParams = %#v, want [T]", base.TypeParams)
	}
	assertFields(t, base, map[string]string{"status": "T", "history": "List<T>"})
	if status := desc.Types["com.acme.generic.dto.OrderStatus"]; status.Kind != "enum" {
		t.Fatalf("OrderStatus (the type arg) not reachable as enum: %#v", desc.Types)
	}
}

// TestParserFidelityLombokAnnotatedDTO pins that Lombok/Jackson-annotation-dense
// source parses cleanly and the field set is the plain declared instance fields:
// @JSONField(name="user_id") does NOT rename the field (Hessian uses the Java field
// name, ignoring Jackson/Fastjson), Lombok adds no phantom fields, and
// transient/static are excluded.
func TestParserFidelityLombokAnnotatedDTO(t *testing.T) {
	root := filepath.Join("testdata", "golden", "lombok")
	idx, err := BuildIndex(Project{
		Name:            "lombok",
		WorkspaceRoot:   root,
		ServicePrefixes: []string{"com.acme.lombok.facade."},
	})
	if err != nil {
		t.Fatalf("BuildIndex: %v", err)
	}
	desc, err := Describe(idx, "com.acme.lombok.facade.UserFacade", "getUser")
	if err != nil {
		t.Fatalf("Describe: %v", err)
	}
	user := desc.Types["com.acme.lombok.dto.UserDTO"]
	if user.Type == "" {
		t.Fatalf("UserDTO schema missing: %#v", desc.Types)
	}
	assertFields(t, user, map[string]string{"userId": "Long", "name": "String"})
	for _, f := range user.Fields {
		switch f.Name {
		case "token", "serialVersionUID", "user_id":
			t.Fatalf("field %q must not appear (transient/static excluded; @JSONField does not rename for Hessian): %#v", f.Name, user.Fields)
		}
	}
}

func containsString(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}
