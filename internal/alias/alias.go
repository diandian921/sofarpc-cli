// Package alias implements a small local registry that maps short names
// (e.g. "user-test") to RPC endpoints (e.g. "10.74.194.40:12200"). The
// registry lives at ~/.sofarpc/servers.json and is read/written by the
// Go client only; addresses are resolved client-side before invocation.
package alias

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/diandian921/sofarpc-mcp/internal/appconfig"
)

// Server is one registry entry.
type Server struct {
	Address     string `json:"address"`
	Description string `json:"description,omitempty"`
}

// Registry is the whole on-disk document.
type Registry struct {
	Servers map[string]Server `json:"servers"`
}

// aliasPattern restricts alias keys to a safe, terminal-friendly charset.
// Lowercase letters, digits, hyphen, underscore, dot; 1..64 chars.
var aliasPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

// hostPortPattern is the minimal shape we treat as a literal address.
// It is intentionally lax; the direct runtime validates the final request.
var hostPortPattern = regexp.MustCompile(`^[^\s]+:\d+$`)

// DefaultPath returns <SOFARPC_HOME>/servers.json, defaulting to
// ~/.sofarpc/servers.json.
func DefaultPath() (string, error) {
	home, err := appconfig.Home()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "servers.json"), nil
}

// Load reads the registry from path. A missing file returns an empty registry,
// not an error — first-run users have nothing saved yet.
func Load(path string) (*Registry, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Registry{Servers: map[string]Server{}}, nil
		}
		return nil, err
	}
	defer f.Close()
	reg := &Registry{}
	dec := json.NewDecoder(f)
	dec.DisallowUnknownFields()
	if err := dec.Decode(reg); err != nil && err != io.EOF {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if reg.Servers == nil {
		reg.Servers = map[string]Server{}
	}
	return reg, nil
}

// Save writes the registry atomically: tmp file + rename.
func Save(path string, reg *Registry) error {
	if reg == nil {
		reg = &Registry{Servers: map[string]Server{}}
	}
	if reg.Servers == nil {
		reg.Servers = map[string]Server{}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	body, err := json.MarshalIndent(reg, "", "  ")
	if err != nil {
		return err
	}
	body = append(body, '\n')
	tmp, err := os.CreateTemp(filepath.Dir(path), ".servers-*.json")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(body); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, path)
}

// IsHostPort reports whether s looks like a raw bolt endpoint.
func IsHostPort(s string) bool {
	return hostPortPattern.MatchString(s)
}

// Resolve turns input into a concrete host:port. If input contains a colon
// followed by digits it is treated as literal; otherwise it is looked up in
// the registry. A miss produces an error that lists the known aliases so
// the agent can offer a correction.
func (r *Registry) Resolve(input string) (string, error) {
	if input == "" {
		return "", fmt.Errorf("address is empty")
	}
	if IsHostPort(input) {
		return input, nil
	}
	if r == nil || len(r.Servers) == 0 {
		return "", fmt.Errorf("alias %q not found: no aliases registered (use `sofarpc server add`)", input)
	}
	if s, ok := r.Servers[input]; ok {
		return s.Address, nil
	}
	return "", fmt.Errorf("alias %q not found; known aliases: %s", input, strings.Join(r.Names(), ", "))
}

// Add inserts or overwrites an alias. It validates key and address shape.
func (r *Registry) Add(name, address, description string) error {
	if !aliasPattern.MatchString(name) {
		return fmt.Errorf("invalid alias name %q: must match %s", name, aliasPattern.String())
	}
	if !IsHostPort(address) {
		return fmt.Errorf("invalid address %q: expected host:port", address)
	}
	if r.Servers == nil {
		r.Servers = map[string]Server{}
	}
	r.Servers[name] = Server{Address: address, Description: description}
	return nil
}

// Remove deletes an alias. Missing key is an error so callers can surface it.
func (r *Registry) Remove(name string) error {
	if _, ok := r.Servers[name]; !ok {
		return fmt.Errorf("alias %q not found", name)
	}
	delete(r.Servers, name)
	return nil
}

// Names returns alias keys sorted alphabetically.
func (r *Registry) Names() []string {
	if r == nil {
		return nil
	}
	out := make([]string, 0, len(r.Servers))
	for k := range r.Servers {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
