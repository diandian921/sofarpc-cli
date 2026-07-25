package mcp

import (
	"encoding/json"
	"io"
	"sort"
	"strings"
	"testing"
	"time"
)

// This file pins the protocol-lifecycle claims the READMEs make in their MCP
// Compliance section. The typed SDK client always performs a correct handshake, so
// the in-memory client tests can never observe what a non-conforming client — or a
// client speaking a revision this build predates — actually gets back. These tests
// drive raw JSON-RPC frames through the real Run() path instead.

// rpcResponse is the subset of a JSON-RPC response these tests assert on.
type rpcResponse struct {
	Result json.RawMessage `json:"result"`
	Error  *rpcError       `json:"error"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// stdioResponses drives Run() over injected streams (the production transport),
// writes each frame, and returns the first wantResponses newline-delimited
// responses. Notifications produce no response, so wantResponses counts only frames
// that carry an id.
func stdioResponses(t *testing.T, wantResponses int, frames ...string) []rpcResponse {
	t.Helper()
	t.Setenv("SOFARPC_HOME", t.TempDir())

	inR, inW := io.Pipe()
	out := &syncBuf{}
	s := &Server{
		BuildVersion:       "test",
		Stdin:              inR,
		Stdout:             out,
		Stderr:             io.Discard,
		DisableConfigWrite: true,
	}
	done := make(chan int, 1)
	go func() { done <- s.Run() }()

	for _, frame := range frames {
		if _, err := io.WriteString(inW, frame+"\n"); err != nil {
			t.Fatalf("write frame: %v", err)
		}
	}

	// Wait for the responses before closing stdin, so an async handler is not raced
	// against EOF.
	var lines []string
	deadline := time.Now().Add(5 * time.Second)
	for {
		lines = completeLines(out.String())
		if len(lines) >= wantResponses {
			break
		}
		if time.Now().After(deadline) {
			_ = inW.Close()
			t.Fatalf("timed out waiting for %d response(s); got:\n%s", wantResponses, out.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
	_ = inW.Close()
	if code := <-done; code != 0 {
		t.Fatalf("Run exit = %d", code)
	}

	responses := make([]rpcResponse, 0, len(lines))
	for _, line := range lines {
		var r rpcResponse
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			t.Fatalf("response %q: %v", line, err)
		}
		responses = append(responses, r)
	}
	return responses
}

// completeLines returns only the newline-terminated lines of raw, so a response
// still being written is never parsed as truncated JSON.
func completeLines(raw string) []string {
	var out []string
	for {
		i := strings.IndexByte(raw, '\n')
		if i < 0 {
			return out
		}
		if line := strings.TrimSpace(raw[:i]); line != "" {
			out = append(out, line)
		}
		raw = raw[i+1:]
	}
}

// initializeFrame builds an initialize request asking for a specific protocol version.
func initializeFrame(version string) string {
	return `{"jsonrpc":"2.0","id":0,"method":"initialize","params":{"protocolVersion":"` + version +
		`","capabilities":{},"clientInfo":{"name":"compliance-test","version":"0"}}}`
}

// initializeResult decodes the fields of an initialize result these tests assert on.
func initializeResult(t *testing.T, res rpcResponse) struct {
	ProtocolVersion string                     `json:"protocolVersion"`
	Capabilities    map[string]json.RawMessage `json:"capabilities"`
} {
	t.Helper()
	var out struct {
		ProtocolVersion string                     `json:"protocolVersion"`
		Capabilities    map[string]json.RawMessage `json:"capabilities"`
	}
	if res.Error != nil {
		t.Fatalf("initialize failed: %+v", *res.Error)
	}
	if err := json.Unmarshal(res.Result, &out); err != nil {
		t.Fatalf("decode initialize result: %v", err)
	}
	return out
}

// TestPreHandshakeRequestIsRefused pins what a client sees when it skips the
// initialize / notifications/initialized handshake: a JSON-RPC error, before any tool
// handler runs. The READMEs documented this refusal but nothing tested it, and the
// documented error code (-32002) was never the code the server sends — -32002 is the
// SDK's resource-not-found code, unrelated to the lifecycle.
//
// The assertion deliberately does not pin a numeric code. The SDK sends 0 here, which
// is not a value any MCP revision assigns to this condition, so pinning it would
// enshrine an implementation accident as a contract. What the READMEs now claim, and
// what this test guards, is that the request is refused rather than served.
func TestPreHandshakeRequestIsRefused(t *testing.T) {
	res := stdioResponses(t, 1, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	got := res[0]
	if got.Error == nil {
		t.Fatalf("tools/list before the handshake must be refused, got result: %s", got.Result)
	}
	if len(got.Result) > 0 {
		t.Errorf("a refused request must not also carry a result: %s", got.Result)
	}
	if !strings.Contains(got.Error.Message, "initialization") {
		t.Errorf("refusal should name the lifecycle stage it happened in, got %q", got.Error.Message)
	}
}

// TestStatelessRequestIsRefused is the 2026-07-28 tripwire.
//
// That revision removes the initialize handshake entirely: a conforming client sends
// no handshake at all and instead carries its protocol version and capabilities in
// _meta on every request. The pinned go-sdk predates it, so such a client is refused
// exactly like a malformed one — this server is unusable for it, and no amount of
// client-side retry helps.
//
// Pinning the gap keeps it visible and documented instead of leaving it to be found
// by a user. When the dependency moves to an SDK that speaks 2026-07-28, this test
// fails; that failure is the signal to flip it to assert a served tools/list and to
// update the support matrix in both READMEs.
func TestStatelessRequestIsRefused(t *testing.T) {
	const statelessListTools = `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{"_meta":{` +
		`"io.modelcontextprotocol/protocolVersion":"2026-07-28",` +
		`"io.modelcontextprotocol/clientCapabilities":{}}}}`
	res := stdioResponses(t, 1, statelessListTools)
	if res[0].Error == nil {
		t.Fatalf("the pinned go-sdk cannot serve a handshake-free 2026-07-28 client; a served "+
			"result means the SDK now speaks it — flip this test and update the README "+
			"support matrix: %s", res[0].Result)
	}
}

// TestInitializeNegotiatesAdvertisedVersions pins the version-negotiation table the
// READMEs publish: every advertised revision is echoed back unchanged, and anything
// outside the list — older, newer, or nonsense — degrades to the newest supported
// revision rather than failing the handshake.
//
// The 2026-07-28 row is the one that matters going forward: a client asking for it
// over initialize is silently served an older revision, which is why a stateless
// client (which never sends initialize at all) is a separate case entirely — see
// TestStatelessRequestIsRefused.
func TestInitializeNegotiatesAdvertisedVersions(t *testing.T) {
	const newestSupported = "2025-11-25"
	cases := []struct{ requested, want string }{
		{"2025-11-25", "2025-11-25"},
		{"2025-06-18", "2025-06-18"},
		{"2025-03-26", "2025-03-26"},
		{"2024-11-05", "2024-11-05"},
		{"2026-07-28", newestSupported}, // newer than this build supports
		{"2024-01-01", newestSupported}, // older than the oldest advertised
		{"not-a-version", newestSupported},
	}
	for _, c := range cases {
		t.Run(c.requested, func(t *testing.T) {
			res := stdioResponses(t, 1, initializeFrame(c.requested))
			if got := initializeResult(t, res[0]).ProtocolVersion; got != c.want {
				t.Errorf("requested %q: negotiated %q, want %q", c.requested, got, c.want)
			}
		})
	}
}

// TestInitializeDeclaresDocumentedCapabilities pins the exact capability set the
// READMEs list, so a capability can never appear or vanish silently — the SDK infers
// tools/prompts/resources from what newSDKServer registers, and a registration change
// would otherwise move the wire contract without a doc change.
//
// `logging` is in the set only because it is go-sdk's default when
// ServerOptions.Capabilities is nil; this server never sends notifications/message.
// SEP-2577 deprecates logging as of 2026-07-28, so this test is also the anchor for
// eventually dropping it deliberately (by passing a non-nil Capabilities) rather than
// by accident.
func TestInitializeDeclaresDocumentedCapabilities(t *testing.T) {
	res := stdioResponses(t, 1, initializeFrame("2025-11-25"))
	caps := initializeResult(t, res[0]).Capabilities

	got := make([]string, 0, len(caps))
	for name := range caps {
		got = append(got, name)
	}
	sort.Strings(got)

	want := []string{"logging", "prompts", "resources", "tools"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("declared capabilities = %v, want %v (update both READMEs with the change)", got, want)
	}
}
