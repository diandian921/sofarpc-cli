package cli

import (
	"encoding/json"
	"fmt"

	"github.com/sofarpc/cli-go/internal/alias"
	"github.com/sofarpc/cli-go/internal/protocol"
)

// resolveAddress turns input into a concrete host:port using the user's
// alias registry. Raw host:port values pass through untouched; aliases are
// looked up in ~/.sofarpc/servers.json. A nil/missing registry is tolerated
// for raw addresses so alias resolution is a pure add-on — agents that
// always pass literals never touch the file.
func resolveAddress(input string) (string, error) {
	if alias.IsHostPort(input) {
		return input, nil
	}
	path, err := alias.DefaultPath()
	if err != nil {
		return "", err
	}
	reg, err := alias.Load(path)
	if err != nil {
		return "", err
	}
	return reg.Resolve(input)
}

// resolveEnvelopeAddress rewrites the `address` field inside the payload of
// an exec-stdin envelope when it looks like an alias. Only ops that take an
// address (invoke, ping) are touched; others pass through unchanged.
func resolveEnvelopeAddress(req *protocol.Request) error {
	switch req.Op {
	case protocol.OpInvoke, protocol.OpPing:
	default:
		return nil
	}
	if len(req.Payload) == 0 {
		return nil
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(req.Payload, &raw); err != nil {
		return nil
	}
	addrRaw, ok := raw["address"]
	if !ok {
		return nil
	}
	var addr string
	if err := json.Unmarshal(addrRaw, &addr); err != nil {
		return nil
	}
	if alias.IsHostPort(addr) {
		return nil
	}
	resolved, err := resolveAddress(addr)
	if err != nil {
		return fmt.Errorf("resolve address: %w", err)
	}
	b, err := json.Marshal(resolved)
	if err != nil {
		return err
	}
	raw["address"] = b
	body, err := json.Marshal(raw)
	if err != nil {
		return err
	}
	req.Payload = body
	return nil
}
