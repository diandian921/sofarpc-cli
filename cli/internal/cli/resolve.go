package cli

import (
	"github.com/diandian921/sofarpc-cli/cli/internal/alias"
	"github.com/diandian921/sofarpc-cli/cli/internal/appconfig"
)

// resolveAddress turns input into a concrete host:port using the user's
// config server registry. Raw host:port values pass through untouched. New
// MCP-first servers are read from ~/.sofarpc/config.json; the legacy
// ~/.sofarpc/servers.json alias file remains a fallback for old users.
func resolveAddress(input string) (string, error) {
	if alias.IsHostPort(input) {
		return input, nil
	}
	configPath, err := appconfig.DefaultPath()
	if err != nil {
		return "", err
	}
	cfg, err := appconfig.Load(configPath)
	if err != nil {
		return "", err
	}
	if server, ok := cfg.Servers[input]; ok {
		return server.Address, nil
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
