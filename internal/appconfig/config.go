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
	"reflect"
	"regexp"
	"sort"
	"strings"
)

const (
	DefaultServerProtocol  = "bolt"
	DefaultServerTimeoutMS = 5000
	DefaultServerAppName   = "sofarpc-agent"
	LegacyConfigVersion    = 1
	CurrentConfigVersion   = 2
	CodeConfigInvalid      = "CONFIG_INVALID"
	CodeConfigUnsupported  = "CONFIG_UNSUPPORTED_VERSION"
)

var (
	namePattern     = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$`)
	hostPortPattern = regexp.MustCompile(`^[^\s]+:\d+$`)
)

type Config struct {
	Version  int                `json:"version"`
	Defaults EndpointDefaults   `json:"defaults"`
	Projects map[string]Project `json:"projects"`
	Servers  map[string]Server  `json:"servers,omitempty"`
}

type Project struct {
	WorkspaceRoot   string             `json:"workspaceRoot"`
	ServicePrefixes []string           `json:"servicePrefixes"`
	ActiveProfile   string             `json:"activeProfile,omitempty"`
	Profiles        map[string]Profile `json:"profiles,omitempty"`
}

type EndpointDefaults struct {
	Protocol    string            `json:"protocol"`
	TimeoutMS   int               `json:"timeoutMs"`
	AppName     string            `json:"appName"`
	Attachments map[string]string `json:"attachments"`
}

type Profile struct {
	Address     string            `json:"address"`
	Protocol    string            `json:"protocol,omitempty"`
	TimeoutMS   int               `json:"timeoutMs,omitempty"`
	AppName     string            `json:"appName,omitempty"`
	Attachments map[string]string `json:"attachments,omitempty"`
}

type Server struct {
	Address     string            `json:"address"`
	Project     string            `json:"project"`
	Profile     string            `json:"profile,omitempty"`
	Protocol    string            `json:"protocol"`
	TimeoutMS   int               `json:"timeoutMs"`
	AppName     string            `json:"appName"`
	Attachments map[string]string `json:"attachments"`
}

// IsHostPort reports whether s is a raw "host:port" endpoint rather than a
// configured server name. The check is intentionally lax — the direct runtime
// validates the final address — it only needs to tell a literal endpoint apart
// from a name to look up in Servers.
func IsHostPort(s string) bool {
	return hostPortPattern.MatchString(s)
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
	root, err := Home()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "config.json"), nil
}

func DefaultLockPath() (string, error) {
	root, err := Home()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "state", "config.lock"), nil
}

func DefaultConfig() Config {
	return Config{
		Version:  CurrentConfigVersion,
		Defaults: DefaultEndpointDefaults(),
		Projects: map[string]Project{},
		Servers:  map[string]Server{},
	}
}

func DefaultEndpointDefaults() EndpointDefaults {
	return EndpointDefaults{
		Protocol:    DefaultServerProtocol,
		TimeoutMS:   DefaultServerTimeoutMS,
		AppName:     DefaultServerAppName,
		Attachments: map[string]string{},
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
		Defaults         EndpointDefaults   `json:"defaults,omitempty"`
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
	} else {
		cfg.Version = LegacyConfigVersion
	}
	if !isZeroEndpointDefaults(disk.Defaults) {
		cfg.Defaults = disk.Defaults
	}
	if disk.Projects != nil {
		cfg.Projects = disk.Projects
	}
	if disk.Servers != nil {
		cfg.Servers = disk.Servers
	}
	applyDefaults(&cfg)
	if err := validateLoadedConfig(cfg); err != nil {
		return cfg, &ConfigError{Code: CodeConfigInvalid, Path: path, Err: err}
	}
	return cfg, nil
}

func Save(path string, cfg Config) error {
	cfg = configForSave(cfg)
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
	if server.Profile == "" {
		server.Profile = InferProfileFromServerName(name, server.Project)
	}
	if server.Profile != "" {
		return c.AddProfile(server.Project, server.Profile, Profile{
			Address:     server.Address,
			Protocol:    server.Protocol,
			TimeoutMS:   server.TimeoutMS,
			AppName:     server.AppName,
			Attachments: server.Attachments,
		}, overwrite)
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
	server, ok := c.Servers[name]
	if !ok {
		return fmt.Errorf("server %q not found", name)
	}
	if server.Profile != "" {
		if project, ok := c.Projects[server.Project]; ok && project.Profiles != nil {
			delete(project.Profiles, server.Profile)
			if project.ActiveProfile == server.Profile {
				project.ActiveProfile = firstProfileName(project.Profiles)
			}
			c.Projects[server.Project] = project
		}
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
	defaults := normalizedEndpointDefaults(c.Defaults)
	if server.Protocol == "" {
		server.Protocol = defaults.Protocol
	}
	if server.TimeoutMS <= 0 {
		server.TimeoutMS = defaults.TimeoutMS
	}
	if server.AppName == "" {
		server.AppName = defaults.AppName
	}
	if server.Attachments == nil {
		server.Attachments = copyStringMap(defaults.Attachments)
	} else {
		server.Attachments = copyStringMap(server.Attachments)
	}
	return server, nil
}

func (c *Config) AddProfile(projectName, profileName string, profile Profile, overwrite bool) (Server, error) {
	if err := validateName("project", projectName); err != nil {
		return Server{}, err
	}
	if err := validateName("profile", profileName); err != nil {
		return Server{}, err
	}
	project, ok := c.Projects[projectName]
	if !ok {
		return Server{}, fmt.Errorf("project %q not found", projectName)
	}
	if project.Profiles == nil {
		project.Profiles = map[string]Profile{}
	}
	if _, exists := project.Profiles[profileName]; exists && !overwrite {
		return Server{}, fmt.Errorf("profile %q already exists for project %q", profileName, projectName)
	}
	normalized, err := c.NormalizeProfile(projectName, profileName, profile)
	if err != nil {
		return Server{}, err
	}
	project.Profiles[profileName] = normalized
	if project.ActiveProfile == "" {
		project.ActiveProfile = profileName
	}
	c.Projects[projectName] = project
	server := c.ServerFromProfile(projectName, profileName, normalized)
	if c.Servers == nil {
		c.Servers = map[string]Server{}
	}
	c.Servers[ServerNameForProfile(projectName, profileName)] = server
	if c.Version <= LegacyConfigVersion {
		c.Version = CurrentConfigVersion
	}
	return server, nil
}

func (c Config) NormalizeProfile(projectName, profileName string, profile Profile) (Profile, error) {
	if profile.Address == "" {
		return Profile{}, fmt.Errorf("profile %q for project %q requires address", profileName, projectName)
	}
	if !hostPortPattern.MatchString(profile.Address) {
		return Profile{}, fmt.Errorf("invalid profile address %q: expected host:port", profile.Address)
	}
	if profile.Attachments != nil {
		profile.Attachments = copyStringMap(profile.Attachments)
	}
	return profile, nil
}

func (c Config) ServerFromProfile(projectName, profileName string, profile Profile) Server {
	defaults := normalizedEndpointDefaults(c.Defaults)
	server := Server{
		Address:     profile.Address,
		Project:     projectName,
		Profile:     profileName,
		Protocol:    profile.Protocol,
		TimeoutMS:   profile.TimeoutMS,
		AppName:     profile.AppName,
		Attachments: profile.Attachments,
	}
	if server.Protocol == "" {
		server.Protocol = defaults.Protocol
	}
	if server.TimeoutMS <= 0 {
		server.TimeoutMS = defaults.TimeoutMS
	}
	if server.AppName == "" {
		server.AppName = defaults.AppName
	}
	if server.Attachments == nil {
		server.Attachments = copyStringMap(defaults.Attachments)
	} else {
		server.Attachments = copyStringMap(server.Attachments)
	}
	return server
}

func ServerNameForProfile(project, profile string) string {
	if project == "" || profile == "" {
		return ""
	}
	return project + "-" + profile
}

func InferProfileFromServerName(serverName, project string) string {
	prefix := project + "-"
	if project == "" || !strings.HasPrefix(serverName, prefix) || len(serverName) == len(prefix) {
		return ""
	}
	return serverName[len(prefix):]
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
	c.Defaults = normalizedEndpointDefaults(c.Defaults)
	if c.Projects == nil {
		c.Projects = map[string]Project{}
	}
	if c.Servers == nil {
		c.Servers = map[string]Server{}
	}
	for name, server := range c.Servers {
		if server.Profile == "" {
			server.Profile = InferProfileFromServerName(name, server.Project)
		}
		if server.Protocol == "" {
			server.Protocol = c.Defaults.Protocol
		}
		if server.TimeoutMS <= 0 {
			server.TimeoutMS = c.Defaults.TimeoutMS
		}
		if server.AppName == "" {
			server.AppName = c.Defaults.AppName
		}
		if server.Attachments == nil {
			server.Attachments = copyStringMap(c.Defaults.Attachments)
		} else {
			server.Attachments = copyStringMap(server.Attachments)
		}
		c.Servers[name] = server
	}
	for name, project := range c.Projects {
		project.ServicePrefixes = NormalizeServicePrefixes(project.ServicePrefixes)
		if project.Profiles == nil {
			project.Profiles = map[string]Profile{}
		}
		for profileName, profile := range project.Profiles {
			if profile.Attachments != nil {
				profile.Attachments = copyStringMap(profile.Attachments)
			}
			project.Profiles[profileName] = profile
			c.Servers[ServerNameForProfile(name, profileName)] = c.ServerFromProfile(name, profileName, profile)
		}
		if project.ActiveProfile == "" {
			project.ActiveProfile = singleProfileName(project.Profiles)
		}
		c.Projects[name] = project
	}
}

func validateLoadedConfig(c Config) error {
	for projectName, project := range c.Projects {
		if err := validateName("project", projectName); err != nil {
			return err
		}
		if project.ActiveProfile != "" {
			if _, ok := project.Profiles[project.ActiveProfile]; !ok {
				return fmt.Errorf("activeProfile %q for project %q does not exist in profiles", project.ActiveProfile, projectName)
			}
		}
		if len(project.Profiles) > 1 && project.ActiveProfile == "" {
			return fmt.Errorf("activeProfile is required when project %q has multiple profiles", projectName)
		}
		for profileName, profile := range project.Profiles {
			if err := validateName("profile", profileName); err != nil {
				return err
			}
			if _, err := c.NormalizeProfile(projectName, profileName, profile); err != nil {
				return err
			}
		}
	}
	for name, server := range c.Servers {
		if err := validateName("server", name); err != nil {
			return err
		}
		if _, err := c.NormalizeServer(server); err != nil {
			return err
		}
	}
	return nil
}

func configForSave(cfg Config) Config {
	if cfg.Version < CurrentConfigVersion {
		cfg.Version = CurrentConfigVersion
	}
	applyDefaults(&cfg)
	if cfg.Version >= CurrentConfigVersion {
		servers := make(map[string]Server, len(cfg.Servers))
		for name, server := range cfg.Servers {
			if isDerivedProfileServer(cfg, name, server) {
				continue
			}
			servers[name] = server
		}
		if len(servers) == 0 {
			cfg.Servers = nil
		} else {
			cfg.Servers = servers
		}
	}
	return cfg
}

func isDerivedProfileServer(cfg Config, name string, server Server) bool {
	if server.Project == "" || server.Profile == "" || name != ServerNameForProfile(server.Project, server.Profile) {
		return false
	}
	project, ok := cfg.Projects[server.Project]
	if !ok {
		return false
	}
	profile, ok := project.Profiles[server.Profile]
	if !ok {
		return false
	}
	return reflect.DeepEqual(server, cfg.ServerFromProfile(server.Project, server.Profile, profile))
}

func normalizedEndpointDefaults(defaults EndpointDefaults) EndpointDefaults {
	if defaults.Protocol == "" {
		defaults.Protocol = DefaultServerProtocol
	}
	if defaults.TimeoutMS <= 0 {
		defaults.TimeoutMS = DefaultServerTimeoutMS
	}
	if defaults.AppName == "" {
		defaults.AppName = DefaultServerAppName
	}
	if defaults.Attachments == nil {
		defaults.Attachments = map[string]string{}
	} else {
		defaults.Attachments = copyStringMap(defaults.Attachments)
	}
	return defaults
}

func isZeroEndpointDefaults(defaults EndpointDefaults) bool {
	return defaults.Protocol == "" && defaults.TimeoutMS == 0 && defaults.AppName == "" && defaults.Attachments == nil
}

func singleProfileName(profiles map[string]Profile) string {
	if len(profiles) != 1 {
		return ""
	}
	return firstProfileName(profiles)
}

func firstProfileName(profiles map[string]Profile) string {
	names := sortedKeys(profiles)
	if len(names) == 0 {
		return ""
	}
	return names[0]
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

func copyStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
