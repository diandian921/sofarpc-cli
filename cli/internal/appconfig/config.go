// Package appconfig owns ~/.sofarpc/config.json, the MCP-first user-editable
// project/server configuration contract.
package appconfig

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const (
	DefaultServerProtocol  = "bolt"
	DefaultServerTimeoutMS = 5000
	DefaultServerAppName   = "sofarpc-agent"
	CurrentConfigVersion   = 1
	CodeConfigInvalid      = "CONFIG_INVALID"
	CodeConfigUnsupported  = "CONFIG_UNSUPPORTED_VERSION"
)

var (
	namePattern     = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$`)
	hostPortPattern = regexp.MustCompile(`^[^\s]+:\d+$`)
)

type Config struct {
	Version  int                `json:"version"`
	Projects map[string]Project `json:"projects"`
	Servers  map[string]Server  `json:"servers"`
}

type Project struct {
	WorkspaceRoot   string   `json:"workspaceRoot"`
	ServicePrefixes []string `json:"servicePrefixes"`
}

type Server struct {
	Address     string            `json:"address"`
	Project     string            `json:"project"`
	Protocol    string            `json:"protocol"`
	TimeoutMS   int               `json:"timeoutMs"`
	AppName     string            `json:"appName"`
	Attachments map[string]string `json:"attachments"`
}

type ConfigError struct {
	Code string
	Path string
	Err  error
}

func (e *ConfigError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("%s: %s: %v", e.Code, e.Path, e.Err)
}

func (e *ConfigError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func DefaultPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".sofarpc", "config.json"), nil
}

func DefaultLockPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".sofarpc", "state", "config.lock"), nil
}

func DefaultConfig() Config {
	return Config{
		Version:  CurrentConfigVersion,
		Projects: map[string]Project{},
		Servers:  map[string]Server{},
	}
}

func Load(path string) (Config, error) {
	cfg := DefaultConfig()
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cfg, nil
		}
		return cfg, err
	}
	defer f.Close()

	var disk struct {
		Version          int                `json:"version,omitempty"`
		Projects         map[string]Project `json:"projects"`
		Servers          map[string]Server  `json:"servers"`
		DeprecatedEngine json.RawMessage    `json:"engine,omitempty"`
	}
	dec := json.NewDecoder(f)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&disk); err != nil && !errors.Is(err, io.EOF) {
		return cfg, &ConfigError{Code: CodeConfigInvalid, Path: path, Err: err}
	}
	if disk.Version > CurrentConfigVersion {
		return cfg, &ConfigError{Code: CodeConfigUnsupported, Path: path, Err: fmt.Errorf("config version %d is newer than supported version %d", disk.Version, CurrentConfigVersion)}
	}
	if disk.Version > 0 {
		cfg.Version = disk.Version
	}
	if disk.Projects != nil {
		cfg.Projects = disk.Projects
	}
	if disk.Servers != nil {
		cfg.Servers = disk.Servers
	}
	applyDefaults(&cfg)
	return cfg, nil
}

func Save(path string, cfg Config) error {
	applyDefaults(&cfg)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	body, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	body = append(body, '\n')
	tmp, err := os.CreateTemp(filepath.Dir(path), ".config-*.json")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(body); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, path)
}

func Update(path, lockPath string, mutate func(*Config) error) (Config, error) {
	lock, err := lockConfig(lockPath)
	if err != nil {
		return Config{}, err
	}
	defer lock()

	cfg, err := Load(path)
	if err != nil {
		return Config{}, err
	}
	if err := mutate(&cfg); err != nil {
		return Config{}, err
	}
	if err := Save(path, cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c *Config) AddProject(name, workspaceRoot string, prefixes []string, overwrite bool) (Project, error) {
	if err := validateName("project", name); err != nil {
		return Project{}, err
	}
	if c.Projects == nil {
		c.Projects = map[string]Project{}
	}
	if _, exists := c.Projects[name]; exists && !overwrite {
		return Project{}, fmt.Errorf("project %q already exists", name)
	}
	root, err := CanonicalWorkspaceRoot(workspaceRoot)
	if err != nil {
		return Project{}, err
	}
	project := Project{
		WorkspaceRoot:   root,
		ServicePrefixes: NormalizeServicePrefixes(prefixes),
	}
	c.Projects[name] = project
	return project, nil
}

func (c *Config) RemoveProject(name string, confirm bool, cascade bool) error {
	if !confirm {
		return fmt.Errorf("confirm=true is required to remove project %q", name)
	}
	if _, ok := c.Projects[name]; !ok {
		return fmt.Errorf("project %q not found", name)
	}
	var refs []string
	for serverName, server := range c.Servers {
		if server.Project == name {
			refs = append(refs, serverName)
		}
	}
	sort.Strings(refs)
	if len(refs) > 0 && !cascade {
		return fmt.Errorf("project %q is still referenced by servers: %s", name, strings.Join(refs, ", "))
	}
	if cascade {
		for _, serverName := range refs {
			delete(c.Servers, serverName)
		}
	}
	delete(c.Projects, name)
	return nil
}

func (c *Config) AddServer(name string, server Server, overwrite bool) (Server, error) {
	if err := validateName("server", name); err != nil {
		return Server{}, err
	}
	if c.Servers == nil {
		c.Servers = map[string]Server{}
	}
	if _, exists := c.Servers[name]; exists && !overwrite {
		return Server{}, fmt.Errorf("server %q already exists", name)
	}
	normalized, err := c.NormalizeServer(server)
	if err != nil {
		return Server{}, err
	}
	c.Servers[name] = normalized
	return normalized, nil
}

func (c *Config) RemoveServer(name string, confirm bool) error {
	if !confirm {
		return fmt.Errorf("confirm=true is required to remove server %q", name)
	}
	if _, ok := c.Servers[name]; !ok {
		return fmt.Errorf("server %q not found", name)
	}
	delete(c.Servers, name)
	return nil
}

func (c Config) NormalizeServer(server Server) (Server, error) {
	if !hostPortPattern.MatchString(server.Address) {
		return Server{}, fmt.Errorf("invalid server address %q: expected host:port", server.Address)
	}
	if server.Project == "" {
		return Server{}, fmt.Errorf("server project is required")
	}
	if _, ok := c.Projects[server.Project]; !ok {
		return Server{}, fmt.Errorf("project %q not found", server.Project)
	}
	if server.Protocol == "" {
		server.Protocol = DefaultServerProtocol
	}
	if server.TimeoutMS <= 0 {
		server.TimeoutMS = DefaultServerTimeoutMS
	}
	if server.AppName == "" {
		server.AppName = DefaultServerAppName
	}
	if server.Attachments == nil {
		server.Attachments = map[string]string{}
	}
	return server, nil
}

func (c Config) ProjectNames() []string {
	return sortedKeys(c.Projects)
}

func (c Config) ServerNames() []string {
	return sortedKeys(c.Servers)
}

func CanonicalWorkspaceRoot(root string) (string, error) {
	if root == "" {
		return "", fmt.Errorf("workspaceRoot is required")
	}
	if strings.HasPrefix(root, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		root = filepath.Join(home, root[2:])
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("workspaceRoot %q is not an existing directory: %w", root, err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("workspaceRoot %q is not an existing directory: %w", root, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("workspaceRoot %q is not a directory", root)
	}
	return resolved, nil
}

func NormalizeServicePrefixes(prefixes []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, prefix := range prefixes {
		p := strings.TrimSpace(prefix)
		if p == "" {
			continue
		}
		p = strings.TrimRight(p, ".") + "."
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out
}

func applyDefaults(c *Config) {
	if c.Version <= 0 {
		c.Version = CurrentConfigVersion
	}
	if c.Projects == nil {
		c.Projects = map[string]Project{}
	}
	if c.Servers == nil {
		c.Servers = map[string]Server{}
	}
	for name, server := range c.Servers {
		if server.Protocol == "" {
			server.Protocol = DefaultServerProtocol
		}
		if server.TimeoutMS <= 0 {
			server.TimeoutMS = DefaultServerTimeoutMS
		}
		if server.AppName == "" {
			server.AppName = DefaultServerAppName
		}
		if server.Attachments == nil {
			server.Attachments = map[string]string{}
		}
		c.Servers[name] = server
	}
	for name, project := range c.Projects {
		project.ServicePrefixes = NormalizeServicePrefixes(project.ServicePrefixes)
		c.Projects[name] = project
	}
}

func validateName(kind, name string) error {
	if !namePattern.MatchString(name) {
		return fmt.Errorf("invalid %s name %q: must match %s", kind, name, namePattern.String())
	}
	return nil
}

func sortedKeys[T any](m map[string]T) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
