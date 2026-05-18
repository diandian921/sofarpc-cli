package cli

import (
	"bytes"
	"encoding/json"
	"strconv"
	"testing"

	"github.com/sofarpc/cli/internal/protocol"
)

func TestInvokeSubcommandReturnsDirectFailureEnvelope(t *testing.T) {
	_, cleanup := tempHome(t)
	defer cleanup()

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	code := runInvoke([]string{
		"--address", "127.0.0.1:1",
		"--service", "com.example.UserService",
		"--method", "getUser",
		"--arg-types", "java.lang.String",
		"--args-json", `["u001"]`,
	}, Env{BuildVersion: "test", Stdout: stdout, Stderr: stderr})

	if code == 0 {
		t.Fatalf("expected non-zero exit; stdout=%s stderr=%s", stdout.String(), stderr.String())
	}
	var resp protocol.Response
	if err := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &resp); err != nil {
		t.Fatalf("decode: %v, out=%s", err, stdout.String())
	}
	if resp.OK || resp.Code != protocol.CodeConnectFailed {
		t.Fatalf("bad resp: %+v", resp)
	}
}

func TestInvokeRejectsArgTypeMismatch(t *testing.T) {
	_, cleanup := tempHome(t)
	defer cleanup()

	stderr := &bytes.Buffer{}
	code := runInvoke([]string{
		"--address", "127.0.0.1:12200",
		"--service", "com.example.UserService",
		"--method", "getUser",
		"--arg-types", "java.lang.String",
		"--args-json", `["u001", 42]`,
	}, Env{BuildVersion: "test", Stdout: &bytes.Buffer{}, Stderr: stderr})

	if code != 2 {
		t.Fatalf("expected exit 2, got %d; stderr=%s", code, stderr.String())
	}
}

func TestBuildInvokePayloadPreservesLargeJSONNumbers(t *testing.T) {
	payload, err := buildInvokePayload(
		"127.0.0.1:12200",
		"com.example.UserService",
		"getUser",
		"com.example.QueryRequest",
		`[{"mpCode":433905635109773312}]`,
		"",
		0,
	)
	if err != nil {
		t.Fatalf("buildInvokePayload: %v", err)
	}
	arg := payload.Args[0].(map[string]interface{})
	number, ok := arg["mpCode"].(json.Number)
	if !ok {
		t.Fatalf("mpCode type = %T", arg["mpCode"])
	}
	if number.String() != strconv.FormatInt(433905635109773312, 10) {
		t.Fatalf("mpCode = %s", number.String())
	}
}

func TestPingSubcommandSendsEnvelope(t *testing.T) {
	_, cleanup := tempHome(t)
	defer cleanup()
	ln := startTCPListener(t)
	defer ln.Close()

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	code := runPing([]string{ln.Addr().String()},
		Env{BuildVersion: "test", Stdout: stdout, Stderr: stderr})
	if code != 0 {
		t.Fatalf("exit = %d, stderr=%s", code, stderr.String())
	}
	var resp protocol.Response
	if err := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.OK {
		t.Fatalf("bad resp: %+v", resp)
	}
}

func TestPingAcceptsFlagsAfterPositional(t *testing.T) {
	_, cleanup := tempHome(t)
	defer cleanup()
	ln := startTCPListener(t)
	defer ln.Close()

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	code := runPing([]string{ln.Addr().String(), "--service", "com.x.Foo"},
		Env{BuildVersion: "test", Stdout: stdout, Stderr: stderr})
	if code != 0 {
		t.Fatalf("exit = %d, stderr=%s", code, stderr.String())
	}
}

func TestServerAddAcceptsFlagAfterPositionals(t *testing.T) {
	dir, cleanup := tempHome(t)
	defer cleanup()

	projectOut := &bytes.Buffer{}
	projectErr := &bytes.Buffer{}
	projectCode := runProject([]string{"add", "user", dir, "--prefix", "com.example"},
		Env{BuildVersion: "test", Stdout: projectOut, Stderr: projectErr})
	if projectCode != 0 {
		t.Fatalf("project add exit = %d; stderr=%s", projectCode, projectErr.String())
	}

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	code := runServerAdd([]string{"user-test", "10.0.0.1:12200", "--project", "user", "--timeout-ms", "3000"},
		Env{BuildVersion: "test", Stdout: stdout, Stderr: stderr})
	if code != 0 {
		t.Fatalf("exit = %d, stderr=%s", code, stderr.String())
	}
	var out map[string]interface{}
	if err := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out["name"] != "user-test" {
		t.Fatalf("unexpected output: %+v", out)
	}
}
