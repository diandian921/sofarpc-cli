package mcp

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sofarpc/cli/internal/schema"
)

func TestToolsListCanDisableConfigWriteTools(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	out := &bytes.Buffer{}
	s := &Server{
		BuildVersion:       "test",
		Stdin:              strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}` + "\n"),
		Stdout:             out,
		Stderr:             &bytes.Buffer{},
		DisableConfigWrite: true,
	}
	if code := s.Run(); code != 0 {
		t.Fatalf("Run exit = %d", code)
	}
	if strings.Contains(out.String(), "add_project") {
		t.Fatalf("config write tool should be hidden: %s", out.String())
	}
	if !strings.Contains(out.String(), "invoke_method") {
		t.Fatalf("expected read tool in list: %s", out.String())
	}
}

func TestAddAndListProjectTool(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	workspace := filepath.Join(home, "workspace")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}

	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"add_project","arguments":{"name":"user","workspaceRoot":"` + workspace + `","servicePrefixes":["com.example"]}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"list_projects","arguments":{}}}`,
		"",
	}, "\n")
	out := &bytes.Buffer{}
	s := &Server{BuildVersion: "test", Stdin: strings.NewReader(input), Stdout: out, Stderr: &bytes.Buffer{}}
	if code := s.Run(); code != 0 {
		t.Fatalf("Run exit = %d", code)
	}

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 responses, got %d: %s", len(lines), out.String())
	}
	var resp map[string]interface{}
	if err := json.Unmarshal([]byte(lines[1]), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	body, _ := json.Marshal(resp["result"])
	if !strings.Contains(string(body), `"name":"user"`) {
		t.Fatalf("list response missing project: %s", string(body))
	}
}

func TestRPCParamTypeForMethodExpandsImportedDTO(t *testing.T) {
	method := schema.Method{
		Package: "com.example.facade",
		Imports: map[string]string{
			"UserRequest": "com.example.model.UserRequest",
		},
		Parameters: []schema.Parameter{{Name: "request", Type: "UserRequest"}},
	}

	if got := rpcParamTypeForMethod("UserRequest", method); got != "com.example.model.UserRequest" {
		t.Fatalf("rpcParamTypeForMethod imported DTO = %q", got)
	}
	if got := rpcParamTypeForMethod("SamePackageRequest", method); got != "com.example.facade.SamePackageRequest" {
		t.Fatalf("rpcParamTypeForMethod same package DTO = %q", got)
	}
	if got := rpcParamTypeForMethod("Long", method); got != "java.lang.Long" {
		t.Fatalf("rpcParamTypeForMethod Long = %q", got)
	}
	if !sameParamTypes(method, []string{"com.example.model.UserRequest"}) {
		t.Fatalf("sameParamTypes should match FQN parameter")
	}
}

func TestDecodeJSONPreservesLargeNumbers(t *testing.T) {
	var payload struct {
		Arguments map[string]interface{} `json:"arguments"`
	}
	err := decodeJSON([]byte(`{"arguments":{"mpCode":433905635109773312}}`), &payload)
	if err != nil {
		t.Fatalf("decodeJSON: %v", err)
	}
	n, ok := payload.Arguments["mpCode"].(json.Number)
	if !ok {
		t.Fatalf("mpCode type = %T, want json.Number", payload.Arguments["mpCode"])
	}
	if n.String() != "433905635109773312" {
		t.Fatalf("mpCode = %s", n.String())
	}
}
