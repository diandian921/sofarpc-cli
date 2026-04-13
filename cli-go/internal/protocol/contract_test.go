package protocol

import (
	"bytes"
	"encoding/json"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// strictDecode rejects unknown top-level envelope fields. The Java daemon does the same via
// Jackson FAIL_ON_UNKNOWN_PROPERTIES — together they pin additionalProperties:false in both
// stacks. Extension must flow through meta (free-form map) or payload (op-specific JsonNode).
func strictDecode(raw []byte, out interface{}) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	return dec.Decode(out)
}

// TestRequestFixturesDecodeCleanly is the Go half of the protocol contract: every shared
// request fixture under protocol/fixtures must decode into a Request with the required
// envelope fields populated. Java does the same in EnvelopeFixturesTest.java.
func TestRequestFixturesDecodeCleanly(t *testing.T) {
	for _, path := range collectByPrefix(t, "request") {
		t.Run(relName(t, path), func(t *testing.T) {
			var req Request
			if err := strictDecode(readFile(t, path), &req); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if req.RequestID == "" {
				t.Fatalf("empty requestId")
			}
			if req.Op == "" {
				t.Fatalf("empty op")
			}
			if len(req.Payload) == 0 {
				t.Fatalf("empty payload")
			}
			assertOpKnown(t, req.Op)
		})
	}
}

// TestResponseFixturesDecodeCleanly mirrors the above for responses: every response fixture
// must carry requestId, ok, and a code from the known set, with ok=true iff code=SUCCESS.
func TestResponseFixturesDecodeCleanly(t *testing.T) {
	for _, path := range collectByPrefix(t, "response") {
		t.Run(relName(t, path), func(t *testing.T) {
			var resp Response
			if err := strictDecode(readFile(t, path), &resp); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if resp.RequestID == "" {
				t.Fatalf("empty requestId")
			}
			if resp.Code == "" {
				t.Fatalf("empty code")
			}
			assertCodeKnown(t, resp.Code)
			if resp.OK && resp.Code != CodeSuccess {
				t.Fatalf("ok=true with non-SUCCESS code %q", resp.Code)
			}
			if !resp.OK && resp.Code == CodeSuccess {
				t.Fatalf("ok=false with SUCCESS code")
			}
		})
	}
}

// TestKnownCodesCovered guarantees every error code the design calls out has at least one
// fixture, so a code silently going missing breaks this test rather than production.
func TestKnownCodesCovered(t *testing.T) {
	seen := map[string]bool{}
	for _, path := range collectByPrefix(t, "response") {
		var resp Response
		if err := strictDecode(readFile(t, path), &resp); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		seen[resp.Code] = true
	}
	required := []string{
		CodeSuccess,
		CodeBadRequest,
		CodeConnectFailed,
		CodeRPCTimeout,
		CodeInvokeFailed,
		CodeAssertionFailed,
		CodeDaemonUnavailable,
		CodeInternalError,
	}
	for _, code := range required {
		if !seen[code] {
			t.Errorf("no fixture covers code %s", code)
		}
	}
}

func assertOpKnown(t *testing.T, op string) {
	t.Helper()
	switch op {
	case OpInvoke, OpPing, OpHealth, OpShutdown:
	default:
		t.Fatalf("unknown op %q", op)
	}
}

func assertCodeKnown(t *testing.T, code string) {
	t.Helper()
	switch code {
	case CodeSuccess, CodeBadRequest, CodeConnectFailed, CodeRPCTimeout,
		CodeInvokeFailed, CodeAssertionFailed, CodeDaemonUnavailable, CodeInternalError:
	default:
		t.Fatalf("unknown code %q", code)
	}
}

func collectByPrefix(t *testing.T, prefix string) []string {
	t.Helper()
	root := fixturesRoot(t)
	var out []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		base := filepath.Base(path)
		if strings.HasPrefix(base, prefix+".") && strings.HasSuffix(base, ".json") {
			out = append(out, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if len(out) == 0 {
		t.Fatalf("no %s.* fixtures under %s", prefix, root)
	}
	return out
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := ioutil.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return raw
}

func relName(t *testing.T, path string) string {
	t.Helper()
	return strings.TrimPrefix(path, fixturesRoot(t)+string(os.PathSeparator))
}

func fixturesRoot(t *testing.T) string {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	candidates := []string{
		filepath.Join(cwd, "..", "..", "..", "protocol", "fixtures"),
		filepath.Join(cwd, "..", "..", "protocol", "fixtures"),
	}
	for _, c := range candidates {
		if info, err := os.Stat(c); err == nil && info.IsDir() {
			abs, _ := filepath.Abs(c)
			return abs
		}
	}
	t.Fatalf("fixtures directory not found (tried %v)", candidates)
	return ""
}
