package alias

import (
	"path/filepath"
	"testing"
)

func TestLoadMissingFileReturnsEmptyRegistry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "servers.json")
	reg, err := Load(path)
	if err != nil {
		t.Fatalf("Load missing file: %v", err)
	}
	if len(reg.Servers) != 0 {
		t.Fatalf("expected empty registry, got %d entries", len(reg.Servers))
	}
}

func TestAddAndSaveRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "servers.json")
	reg, _ := Load(path)
	if err := reg.Add("user-test", "10.74.194.40:12200", "test env user service"); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := Save(path, reg); err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	got, ok := loaded.Servers["user-test"]
	if !ok {
		t.Fatal("alias missing after roundtrip")
	}
	if got.Address != "10.74.194.40:12200" {
		t.Fatalf("address mismatch: %q", got.Address)
	}
	if got.Description != "test env user service" {
		t.Fatalf("description mismatch: %q", got.Description)
	}
}

func TestAddValidation(t *testing.T) {
	reg := &Registry{Servers: map[string]Server{}}
	cases := []struct {
		name    string
		address string
		wantErr bool
	}{
		{"ok", "127.0.0.1:12200", false},
		{"has.dots_and-dashes", "host:1", false},
		{"UPPER", "host:1", true},
		{"-leading-dash", "host:1", true},
		{"name", "no-port", true},
		{"name", "", true},
		{"", "host:1", true},
	}
	for _, tc := range cases {
		err := reg.Add(tc.name, tc.address, "")
		if (err != nil) != tc.wantErr {
			t.Errorf("Add(%q,%q) error=%v, wantErr=%v", tc.name, tc.address, err, tc.wantErr)
		}
	}
}

func TestResolvePassesThroughHostPort(t *testing.T) {
	reg := &Registry{Servers: map[string]Server{}}
	got, err := reg.Resolve("10.0.0.1:12200")
	if err != nil {
		t.Fatalf("Resolve literal: %v", err)
	}
	if got != "10.0.0.1:12200" {
		t.Fatalf("expected passthrough, got %q", got)
	}
}

func TestResolveLooksUpAlias(t *testing.T) {
	reg := &Registry{Servers: map[string]Server{
		"user-test": {Address: "10.74.194.40:12200"},
	}}
	got, err := reg.Resolve("user-test")
	if err != nil {
		t.Fatalf("Resolve alias: %v", err)
	}
	if got != "10.74.194.40:12200" {
		t.Fatalf("expected resolved address, got %q", got)
	}
}

func TestResolveMissingAliasLists(t *testing.T) {
	reg := &Registry{Servers: map[string]Server{
		"alpha": {Address: "h:1"},
		"beta":  {Address: "h:2"},
	}}
	_, err := reg.Resolve("gamma")
	if err == nil {
		t.Fatal("expected error on missing alias")
	}
	msg := err.Error()
	if !contains(msg, "alpha") || !contains(msg, "beta") {
		t.Fatalf("expected known aliases in error, got: %s", msg)
	}
}

func TestRemove(t *testing.T) {
	reg := &Registry{Servers: map[string]Server{"a": {Address: "h:1"}}}
	if err := reg.Remove("a"); err != nil {
		t.Fatalf("Remove existing: %v", err)
	}
	if err := reg.Remove("a"); err == nil {
		t.Fatal("expected error removing missing alias")
	}
}

func TestIsHostPort(t *testing.T) {
	cases := map[string]bool{
		"10.0.0.1:12200":    true,
		"localhost:8080":    true,
		"example.com:12200": true,
		"noport":            false,
		"alias":             false,
		":12200":            false,
		"host:":             false,
		"host:abc":          false,
	}
	for in, want := range cases {
		if got := IsHostPort(in); got != want {
			t.Errorf("IsHostPort(%q)=%v, want %v", in, got, want)
		}
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}
