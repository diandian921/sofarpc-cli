package proto

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func responseByID(t *testing.T, out string, id interface{}) map[string]interface{} {
	t.Helper()
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var resp map[string]interface{}
		if err := json.Unmarshal([]byte(line), &resp); err != nil {
			continue
		}
		if fmt.Sprint(resp["id"]) == fmt.Sprint(id) {
			return resp
		}
	}
	return nil
}

func runFrames(t *testing.T, frames string) string {
	t.Helper()
	out := &bytes.Buffer{}
	if code := newTestSession(strings.NewReader(frames), out).Run(); code != 0 {
		t.Fatalf("Run = %d", code)
	}
	return out.String()
}

func assertPingOK(t *testing.T, out string, id int) {
	t.Helper()
	resp := responseByID(t, out, id)
	if resp == nil {
		t.Fatalf("no ping response for id %d: %s", id, out)
	}
	if resp["error"] != nil {
		t.Fatalf("ping must not error (id %d): %s", id, out)
	}
	result, ok := resp["result"].(map[string]interface{})
	if !ok || len(result) != 0 {
		t.Fatalf("ping result must be an empty object (id %d): %s", id, out)
	}
}

// TestPingAnsweredInEveryState pins that ping is answered with {} regardless of
// lifecycle state and never gates on initialization.
func TestPingAnsweredInEveryState(t *testing.T) {
	ping := `{"jsonrpc":"2.0","id":7,"method":"ping"}` + "\n"

	t.Run("StateNew", func(t *testing.T) {
		assertPingOK(t, runFrames(t, ping), 7)
	})
	t.Run("StateInitializing", func(t *testing.T) {
		frames := `{"jsonrpc":"2.0","id":0,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}` + "\n" + ping
		assertPingOK(t, runFrames(t, frames), 7)
	})
	t.Run("StateReady", func(t *testing.T) {
		assertPingOK(t, runFrames(t, handshakeFrames()+ping), 7)
	})
}

// TestSecondInitializeIsRejectedWithoutRollback pins that a duplicate initialize
// is rejected with -32600 and does not roll the session back out of Ready.
func TestSecondInitializeIsRejectedWithoutRollback(t *testing.T) {
	frames := handshakeFrames() +
		`{"jsonrpc":"2.0","id":9,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}` + "\n" +
		`{"jsonrpc":"2.0","id":10,"method":"tools/list","params":{}}` + "\n"
	out := runFrames(t, frames)

	dup := responseByID(t, out, 9)
	if dup == nil || dup["error"] == nil {
		t.Fatalf("second initialize must error: %s", out)
	}
	if errObj, _ := dup["error"].(map[string]interface{}); errObj == nil || fmt.Sprint(errObj["code"]) != "-32600" {
		t.Fatalf("second initialize must be -32600: %s", out)
	}
	after := responseByID(t, out, 10)
	if after == nil || after["result"] == nil {
		t.Fatalf("session must stay Ready after a rejected re-initialize: %s", out)
	}
}
