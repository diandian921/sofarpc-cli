package tools

import (
	"context"
	"encoding/json"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/diandian921/sofarpc-mcp/internal/appconfig"
)

// newToolClient spins up a bare SDK server with only the tools the caller
// registers and returns a live in-memory client session, so each test drives one
// Add* registration through the real initialize / tools/call handshake instead of
// invoking handlers directly.
func newToolClient(t *testing.T, register func(srv *mcpsdk.Server)) *mcpsdk.ClientSession {
	t.Helper()
	ctx := context.Background()
	serverT, clientT := mcpsdk.NewInMemoryTransports()
	srv := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "tools-test", Version: "0"}, nil)
	register(srv)
	if _, err := srv.Connect(ctx, serverT, nil); err != nil {
		t.Fatalf("server connect: %v", err)
	}
	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "tools-test-client", Version: "0"}, nil)
	cs, err := client.Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

// seedConfig points SOFARPC_HOME at a fresh temp dir and applies mutate to the
// empty config, mirroring the sdkserver_test seeding pattern. A nil mutate just
// isolates the home directory.
func seedConfig(t *testing.T, mutate func(cfg *appconfig.Config) error) {
	t.Helper()
	t.Setenv("SOFARPC_HOME", t.TempDir())
	if mutate == nil {
		return
	}
	path, err := appconfig.DefaultPath()
	if err != nil {
		t.Fatalf("default path: %v", err)
	}
	lock, err := appconfig.DefaultLockPath()
	if err != nil {
		t.Fatalf("lock path: %v", err)
	}
	if _, err := appconfig.Update(path, lock, mutate); err != nil {
		t.Fatalf("seed config: %v", err)
	}
}

// callTool invokes one tool over the live session and fails the test on a
// protocol-level error (tool failures must stay inside the result envelope).
func callTool(t *testing.T, cs *mcpsdk.ClientSession, name string, args map[string]any) *mcpsdk.CallToolResult {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcpsdk.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: unexpected protocol error: %v", name, err)
	}
	return res
}

// envelopeError is the error sub-object of the decoded app.Result envelope.
type envelopeError struct {
	Message  string         `json:"message"`
	NextTool string         `json:"nextTool"`
	Recovery string         `json:"recovery"`
	Details  map[string]any `json:"details"`
}

// resultEnvelope is the app.Result wire shape decoded from structuredContent.
type resultEnvelope struct {
	OK        bool           `json:"ok"`
	Code      string         `json:"code"`
	RequestID string         `json:"requestId"`
	Data      map[string]any `json:"data"`
	Error     *envelopeError `json:"error"`
}

// decodeEnvelope unmarshals a tool result's structuredContent into the shared
// app.Result envelope shape.
func decodeEnvelope(t *testing.T, res *mcpsdk.CallToolResult) resultEnvelope {
	t.Helper()
	body, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("marshal structuredContent: %v", err)
	}
	var env resultEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("structuredContent is not an app.Result envelope: %v\n%s", err, body)
	}
	return env
}
