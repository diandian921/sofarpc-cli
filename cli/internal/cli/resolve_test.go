package cli

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sofarpc/cli/internal/alias"
	"github.com/sofarpc/cli/internal/protocol"
)

func TestResolveEnvelopeAddressLiteralPassthrough(t *testing.T) {
	_, cleanup := tempHome(t)
	defer cleanup()

	req := protocol.Request{
		Op:      protocol.OpPing,
		Payload: json.RawMessage(`{"address":"10.0.0.1:12200"}`),
	}
	if err := resolveEnvelopeAddress(&req); err != nil {
		t.Fatalf("resolveEnvelopeAddress: %v", err)
	}
	if !strings.Contains(string(req.Payload), "10.0.0.1:12200") {
		t.Fatalf("literal address got rewritten: %s", string(req.Payload))
	}
}

func TestResolveEnvelopeAddressAliasLookup(t *testing.T) {
	base, cleanup := tempHome(t)
	defer cleanup()

	path := filepath.Join(base, "servers.json")
	reg := &alias.Registry{Servers: map[string]alias.Server{}}
	if err := reg.Add("user-test", "10.74.194.40:12200", "desc"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := alias.Save(path, reg); err != nil {
		t.Fatalf("save: %v", err)
	}

	req := protocol.Request{
		Op:      protocol.OpInvoke,
		Payload: json.RawMessage(`{"address":"user-test","service":"S","method":"m","argTypes":[],"args":[]}`),
	}
	if err := resolveEnvelopeAddress(&req); err != nil {
		t.Fatalf("resolveEnvelopeAddress: %v", err)
	}

	var got map[string]interface{}
	if err := json.Unmarshal(req.Payload, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got["address"] != "10.74.194.40:12200" {
		t.Fatalf("address not resolved: %v", got["address"])
	}
}

func TestResolveEnvelopeAddressMissingAlias(t *testing.T) {
	_, cleanup := tempHome(t)
	defer cleanup()

	req := protocol.Request{
		Op:      protocol.OpPing,
		Payload: json.RawMessage(`{"address":"nope"}`),
	}
	err := resolveEnvelopeAddress(&req)
	if err == nil {
		t.Fatal("expected error on missing alias")
	}
}

func TestResolveEnvelopeAddressNonAddressableOp(t *testing.T) {
	_, cleanup := tempHome(t)
	defer cleanup()

	req := protocol.Request{
		Op:      protocol.OpHealth,
		Payload: json.RawMessage(`{}`),
	}
	if err := resolveEnvelopeAddress(&req); err != nil {
		t.Fatalf("health should not need resolution: %v", err)
	}
}

func TestServerAddListRemove(t *testing.T) {
	base, cleanup := tempHome(t)
	defer cleanup()

	env := Env{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}
	if code := runProject([]string{"add", "user", filepath.Dir(base)}, env); code != 0 {
		t.Fatalf("project add exit=%d stderr=%s", code, env.Stderr.(*bytes.Buffer).String())
	}
	if code := runServer([]string{"add", "user-test", "10.74.194.40:12200", "--project", "user"}, env); code != 0 {
		t.Fatalf("add exit=%d stderr=%s", code, env.Stderr.(*bytes.Buffer).String())
	}

	listOut := &bytes.Buffer{}
	listEnv := Env{Stdout: listOut, Stderr: &bytes.Buffer{}}
	if code := runServer([]string{"list", "--json"}, listEnv); code != 0 {
		t.Fatalf("list exit=%d", code)
	}
	if !strings.Contains(listOut.String(), `"user-test"`) {
		t.Fatalf("list missing alias: %s", listOut.String())
	}

	rmEnv := Env{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}
	if code := runServer([]string{"remove", "user-test", "--confirm"}, rmEnv); code != 0 {
		t.Fatalf("remove exit=%d stderr=%s", code, rmEnv.Stderr.(*bytes.Buffer).String())
	}

	rmMissingEnv := Env{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}
	if code := runServer([]string{"remove", "user-test", "--confirm"}, rmMissingEnv); code == 0 {
		t.Fatal("expected non-zero exit when removing missing alias")
	}
}
