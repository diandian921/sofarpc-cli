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

func containsString(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}
