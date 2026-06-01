package cli

import (
	"fmt"
	"sort"
	"strings"

	"github.com/diandian921/sofarpc-mcp/internal/appconfig"
)

// resolveAddress turns input into a concrete host:port using the user's config
// server registry (~/.sofarpc/config.json). A raw host:port passes through
// untouched; a configured server name resolves to its address. An unknown name
// returns an error listing the known servers so the agent can correct it.
func resolveAddress(input string) (string, error) {
	if input == "" {
		return "", fmt.Errorf("address is empty")
	}
	if appconfig.IsHostPort(input) {
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
	if len(cfg.Servers) == 0 {
		return "", fmt.Errorf("server %q not found: no servers configured (use `sofarpc server add`)", input)
	}
	names := make([]string, 0, len(cfg.Servers))
	for name := range cfg.Servers {
		names = append(names, name)
	}
	sort.Strings(names)
	return "", fmt.Errorf("server %q not found; known servers: %s", input, strings.Join(names, ", "))
}
