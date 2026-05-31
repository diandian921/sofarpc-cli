package proto

import (
	"context"
	"encoding/json"
	"testing"
)

func TestInFlightCancelByID(t *testing.T) {
	f := newInFlight()
	_, cancel := context.WithCancel(context.Background())
	id := json.RawMessage(`"req-1"`)
	f.register(id, "tools/call", cancel)
	if f.wasCancelled(id, "tools/call") {
		t.Fatal("should not be cancelled before cancel")
	}
	f.cancel(id)
	if !f.wasCancelled(id, "tools/call") {
		t.Fatal("must be cancelled after cancel")
	}
}

func TestInFlightDuplicateIDKeepsBothEntries(t *testing.T) {
	f := newInFlight()
	id := json.RawMessage(`"dup"`)
	_, c1 := context.WithCancel(context.Background())
	_, c2 := context.WithCancel(context.Background())
	f.register(id, "tools/call", c1)
	f.register(id, "tools/other", c2)
	// Cancel-by-id marks every entry sharing the id; neither clobbered the other.
	f.cancel(id)
	if !f.wasCancelled(id, "tools/call") || !f.wasCancelled(id, "tools/other") {
		t.Fatal("dup-id entries must both be cancellable")
	}
}
