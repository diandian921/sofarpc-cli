package mcp

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/sofarpc/cli/internal/schema"
)

func TestToolsListRegistersWorkflowTools(t *testing.T) {
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
	var resp struct {
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(out.Bytes()), &resp); err != nil {
		t.Fatalf("decode tools/list: %v", err)
	}
	names := make([]string, 0, len(resp.Result.Tools))
	for _, tool := range resp.Result.Tools {
		names = append(names, tool.Name)
	}
	sort.Strings(names)
	want := []string{
		"sofarpc_config",
		"sofarpc_describe",
		"sofarpc_doctor",
		"sofarpc_invoke",
		"sofarpc_probe",
		"sofarpc_resolve",
	}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("tools = %v, want %v", names, want)
	}
	for _, legacy := range []string{"add" + "_project", "list" + "_projects", "invoke" + "_method", "ping" + "_service"} {
		if strings.Contains(out.String(), legacy) {
			t.Fatalf("legacy tool %q should not be listed: %s", legacy, out.String())
		}
	}
}

func TestConfigWriteCanBeDisabled(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	workspace := filepath.Join(home, "workspace")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}

	input := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"sofarpc_config","arguments":{"action":"save_project","name":"user","workspaceRoot":"` + workspace + `"}}}` + "\n"
	out := &bytes.Buffer{}
	s := &Server{BuildVersion: "test", Stdin: strings.NewReader(input), Stdout: out, Stderr: &bytes.Buffer{}, DisableConfigWrite: true}
	if code := s.Run(); code != 0 {
		t.Fatalf("Run exit = %d", code)
	}
	if !strings.Contains(out.String(), "config write tools are disabled") {
		t.Fatalf("expected disabled write error: %s", out.String())
	}
}

func TestConfigSaveAndListProjectTool(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	workspace := filepath.Join(home, "workspace")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}

	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"sofarpc_config","arguments":{"action":"save_project","name":"user","workspaceRoot":"` + workspace + `","servicePrefixes":["com.example"]}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"sofarpc_config","arguments":{"action":"list"}}}`,
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

func TestResolveAndInvokeDryRunUseWorkflowTools(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	workspace := filepath.Join(home, "workspace")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}

	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"sofarpc_config","arguments":{"action":"save_project","name":"user","workspaceRoot":"` + workspace + `"}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"sofarpc_config","arguments":{"action":"save_server","name":"user-test","address":"127.0.0.1:12200","project":"user"}}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"sofarpc_resolve","arguments":{"server":"user-test"}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"sofarpc_invoke","arguments":{"server":"user-test","service":"com.example.UserService","method":"getUser","paramTypes":["java.lang.String"],"args":["u001"],"dryRun":true}}}`,
		"",
	}, "\n")
	out := &bytes.Buffer{}
	s := &Server{BuildVersion: "test", Stdin: strings.NewReader(input), Stdout: out, Stderr: &bytes.Buffer{}}
	if code := s.Run(); code != 0 {
		t.Fatalf("Run exit = %d", code)
	}

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 4 {
		t.Fatalf("expected 4 responses, got %d: %s", len(lines), out.String())
	}
	if !strings.Contains(lines[2], `"endpoint"`) || !strings.Contains(lines[2], `"user-test"`) {
		t.Fatalf("resolve response missing endpoint: %s", lines[2])
	}
	if !strings.Contains(lines[3], `"dryRun":true`) || !strings.Contains(lines[3], `"argTypes":["java.lang.String"]`) {
		t.Fatalf("dry run response missing plan: %s", lines[3])
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
