package proto

import (
	"bytes"
	"context"
	"encoding/json"
	"sync"
)

// inFlight tracks running async requests. The map is keyed by (id, method) so a
// client that reuses an id across methods cannot clobber another request's
// cancel function. Cancel-by-id marks every entry sharing that id.
type inFlight struct {
	mu      sync.Mutex
	entries map[string]*inFlightEntry
}

type inFlightEntry struct {
	id        string
	cancel    context.CancelFunc
	cancelled bool
}

func newInFlight() *inFlight {
	return &inFlight{entries: map[string]*inFlightEntry{}}
}

// idKey normalizes a raw JSON id to a stable string ("" for absent ids).
func idKey(id json.RawMessage) string {
	id = bytes.TrimSpace(id)
	if len(id) == 0 {
		return ""
	}
	return string(id)
}

func compositeKey(id json.RawMessage, method string) string {
	return idKey(id) + "\x00" + method
}

// register records a running request and its cancel func. Absent-id requests are
// not tracked (they cannot be cancelled).
func (f *inFlight) register(id json.RawMessage, method string, cancel context.CancelFunc) {
	key := idKey(id)
	if key == "" {
		return
	}
	f.mu.Lock()
	f.entries[compositeKey(id, method)] = &inFlightEntry{id: key, cancel: cancel}
	f.mu.Unlock()
}

func (f *inFlight) unregister(id json.RawMessage, method string) {
	if idKey(id) == "" {
		return
	}
	f.mu.Lock()
	delete(f.entries, compositeKey(id, method))
	f.mu.Unlock()
}

// cancel marks every entry whose id matches and invokes its cancel func. A
// cancelled request's eventual response is dropped (see wasCancelled).
func (f *inFlight) cancel(id json.RawMessage) {
	key := idKey(id)
	if key == "" {
		return
	}
	f.mu.Lock()
	var cancels []context.CancelFunc
	for _, e := range f.entries {
		if e.id == key {
			e.cancelled = true
			cancels = append(cancels, e.cancel)
		}
	}
	f.mu.Unlock()
	for _, c := range cancels {
		c()
	}
}

// wasCancelled reports whether the (id, method) request was cancelled, so the
// dispatcher can drop the final response (MCP: a cancelled request SHOULD NOT
// receive a response).
func (f *inFlight) wasCancelled(id json.RawMessage, method string) bool {
	if idKey(id) == "" {
		return false
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	e, ok := f.entries[compositeKey(id, method)]
	return ok && e.cancelled
}
