package tools

import (
	"context"
	"encoding/json"

	"github.com/diandian921/sofarpc-mcp/internal/app"
	"github.com/diandian921/sofarpc-mcp/internal/appconfig"
	"github.com/diandian921/sofarpc-mcp/internal/mcp/server"
)

// ----- sofarpc_config_list -----

// ConfigListArgs are the arguments for sofarpc_config_list.
type ConfigListArgs struct {
	Project string `json:"project,omitempty"`
}

var configListInputSchema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "project": {"type": "string", "description": "Optional project filter."}
  }
}`)

// ConfigListTool lists projects and servers in ~/.sofarpc/config.json.
func ConfigListTool(writeEnabled bool) server.Tool[ConfigListArgs] {
	return server.Tool[ConfigListArgs]{
		Spec: server.ToolSpec{
			Name:         "sofarpc_config_list",
			Title:        "SofaRPC Config: List",
			Description:  "List configured projects and servers from ~/.sofarpc/config.json.",
			Annotations:  server.Annotations{ReadOnlyHint: true, IdempotentHint: true},
			InputSchema:  configListInputSchema,
			OutputSchema: resultOutputSchema,
		},
		Run: func(_ context.Context, _ server.Runtime, a ConfigListArgs) server.Result {
			cfg, err := loadConfig()
			if err != nil {
				return configFailure(err)
			}
			path, err := appconfig.DefaultPath()
			if err != nil {
				return failure(app.CodeInternalError, err.Error(), nil)
			}
			projects := make([]map[string]interface{}, 0, len(cfg.Projects))
			for _, name := range cfg.ProjectNames() {
				if a.Project != "" && name != a.Project {
					continue
				}
				projects = append(projects, map[string]interface{}{"name": name, "project": cfg.Projects[name]})
			}
			servers := make([]map[string]interface{}, 0, len(cfg.Servers))
			for _, name := range cfg.ServerNames() {
				srv := cfg.Servers[name]
				if a.Project != "" && srv.Project != a.Project {
					continue
				}
				servers = append(servers, map[string]interface{}{"name": name, "server": publicServer(srv)})
			}
			return success("Config loaded.", map[string]interface{}{
				"configPath":    path,
				"writeEnabled":  writeEnabled,
				"projects":      projects,
				"servers":       servers,
				"projectFilter": a.Project,
			})
		},
	}
}

// ----- sofarpc_config_save_project -----

// ConfigSaveProjectArgs are the arguments for sofarpc_config_save_project.
type ConfigSaveProjectArgs struct {
	Name            string   `json:"name"`
	WorkspaceRoot   string   `json:"workspaceRoot"`
	ServicePrefixes []string `json:"servicePrefixes,omitempty"`
	Overwrite       bool     `json:"overwrite,omitempty"`
}

var configSaveProjectInputSchema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["name", "workspaceRoot"],
  "properties": {
    "name": {"type": "string", "description": "Project name."},
    "workspaceRoot": {"type": "string", "description": "Absolute or ~-relative local source root."},
    "servicePrefixes": {"type": "array", "items": {"type": "string"}, "description": "Optional Java service package prefixes."},
    "overwrite": {"type": "boolean", "description": "Allow replacing an existing project."}
  }
}`)

// ConfigSaveProjectTool adds or replaces a project.
func ConfigSaveProjectTool() server.Tool[ConfigSaveProjectArgs] {
	return server.Tool[ConfigSaveProjectArgs]{
		Spec: server.ToolSpec{
			Name:         "sofarpc_config_save_project",
			Title:        "SofaRPC Config: Save Project",
			Description:  "Add or replace a local source project in config.json.",
			Annotations:  server.Annotations{},
			InputSchema:  configSaveProjectInputSchema,
			OutputSchema: resultOutputSchema,
		},
		Run: func(_ context.Context, _ server.Runtime, a ConfigSaveProjectArgs) server.Result {
			if a.Name == "" || a.WorkspaceRoot == "" {
				return failure(app.CodeBadRequest, "name and workspaceRoot are required", nil)
			}
			path, lock, err := configPaths()
			if err != nil {
				return failure(app.CodeInternalError, err.Error(), nil)
			}
			var project appconfig.Project
			if _, err = appconfig.Update(path, lock, func(cfg *appconfig.Config) error {
				var addErr error
				project, addErr = cfg.AddProject(a.Name, a.WorkspaceRoot, a.ServicePrefixes, a.Overwrite)
				return addErr
			}); err != nil {
				return configFailure(err)
			}
			return success("Project saved to config.json.", map[string]interface{}{"name": a.Name, "project": project})
		},
	}
}

// ----- sofarpc_config_save_server -----

// ConfigSaveServerArgs are the arguments for sofarpc_config_save_server.
type ConfigSaveServerArgs struct {
	Name        string            `json:"name"`
	Address     string            `json:"address"`
	Project     string            `json:"project"`
	Protocol    string            `json:"protocol,omitempty"`
	TimeoutMS   int               `json:"timeoutMs,omitempty"`
	AppName     string            `json:"appName,omitempty"`
	Attachments map[string]string `json:"attachments,omitempty"`
	Overwrite   bool              `json:"overwrite,omitempty"`
}

var configSaveServerInputSchema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["name", "address", "project"],
  "properties": {
    "name": {"type": "string", "description": "Server name."},
    "address": {"type": "string", "description": "host:port."},
    "project": {"type": "string", "description": "Bound project name."},
    "protocol": {"type": "string", "description": "Protocol; default bolt."},
    "timeoutMs": {"type": "integer", "description": "Default total timeout in milliseconds."},
    "appName": {"type": "string", "description": "SofaRPC consumer app name."},
    "attachments": {"type": "object", "additionalProperties": {"type": "string"}, "description": "Optional static SofaRPC attachments."},
    "overwrite": {"type": "boolean", "description": "Allow replacing an existing server."}
  }
}`)

// ConfigSaveServerTool adds or replaces a server.
func ConfigSaveServerTool() server.Tool[ConfigSaveServerArgs] {
	return server.Tool[ConfigSaveServerArgs]{
		Spec: server.ToolSpec{
			Name:         "sofarpc_config_save_server",
			Title:        "SofaRPC Config: Save Server",
			Description:  "Add or replace a configured RPC server in config.json.",
			Annotations:  server.Annotations{},
			InputSchema:  configSaveServerInputSchema,
			OutputSchema: resultOutputSchema,
		},
		Run: func(_ context.Context, _ server.Runtime, a ConfigSaveServerArgs) server.Result {
			if a.Name == "" || a.Address == "" || a.Project == "" {
				return failure(app.CodeBadRequest, "name, address and project are required", nil)
			}
			srv := appconfig.Server{
				Address:     a.Address,
				Project:     a.Project,
				Protocol:    valueOr(a.Protocol, appconfig.DefaultServerProtocol),
				TimeoutMS:   intOr(a.TimeoutMS, appconfig.DefaultServerTimeoutMS),
				AppName:     valueOr(a.AppName, appconfig.DefaultServerAppName),
				Attachments: a.Attachments,
			}
			path, lock, err := configPaths()
			if err != nil {
				return failure(app.CodeInternalError, err.Error(), nil)
			}
			var saved appconfig.Server
			if _, err = appconfig.Update(path, lock, func(cfg *appconfig.Config) error {
				var addErr error
				saved, addErr = cfg.AddServer(a.Name, srv, a.Overwrite)
				return addErr
			}); err != nil {
				return configFailure(err)
			}
			return success("Server saved to config.json.", map[string]interface{}{"name": a.Name, "server": publicServer(saved)})
		},
	}
}

// ----- sofarpc_config_remove_project -----

// ConfigRemoveProjectArgs are the arguments for sofarpc_config_remove_project.
type ConfigRemoveProjectArgs struct {
	Name    string `json:"name"`
	Confirm bool   `json:"confirm,omitempty"`
	Cascade bool   `json:"cascade,omitempty"`
}

var configRemoveProjectInputSchema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["name"],
  "properties": {
    "name": {"type": "string", "description": "Project name to remove."},
    "confirm": {"type": "boolean", "description": "Must be true to actually remove."},
    "cascade": {"type": "boolean", "description": "Also remove servers bound to the project."}
  }
}`)

// ConfigRemoveProjectTool removes a project (destructive).
func ConfigRemoveProjectTool() server.Tool[ConfigRemoveProjectArgs] {
	return server.Tool[ConfigRemoveProjectArgs]{
		Spec: server.ToolSpec{
			Name:         "sofarpc_config_remove_project",
			Title:        "SofaRPC Config: Remove Project",
			Description:  "Remove a project from config.json. Requires confirm=true.",
			Annotations:  server.Annotations{DestructiveHint: true},
			InputSchema:  configRemoveProjectInputSchema,
			OutputSchema: resultOutputSchema,
		},
		Run: func(_ context.Context, _ server.Runtime, a ConfigRemoveProjectArgs) server.Result {
			if a.Name == "" {
				return failure(app.CodeBadRequest, "name is required", nil)
			}
			path, lock, err := configPaths()
			if err != nil {
				return failure(app.CodeInternalError, err.Error(), nil)
			}
			if _, err = appconfig.Update(path, lock, func(cfg *appconfig.Config) error {
				return cfg.RemoveProject(a.Name, a.Confirm, a.Cascade)
			}); err != nil {
				return configFailure(err)
			}
			return success("Project removed from config.json.", map[string]interface{}{"removed": a.Name})
		},
	}
}

// ----- sofarpc_config_remove_server -----

// ConfigRemoveServerArgs are the arguments for sofarpc_config_remove_server.
type ConfigRemoveServerArgs struct {
	Name    string `json:"name"`
	Confirm bool   `json:"confirm,omitempty"`
}

var configRemoveServerInputSchema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["name"],
  "properties": {
    "name": {"type": "string", "description": "Server name to remove."},
    "confirm": {"type": "boolean", "description": "Must be true to actually remove."}
  }
}`)

// ConfigRemoveServerTool removes a server (destructive).
func ConfigRemoveServerTool() server.Tool[ConfigRemoveServerArgs] {
	return server.Tool[ConfigRemoveServerArgs]{
		Spec: server.ToolSpec{
			Name:         "sofarpc_config_remove_server",
			Title:        "SofaRPC Config: Remove Server",
			Description:  "Remove a server from config.json. Requires confirm=true.",
			Annotations:  server.Annotations{DestructiveHint: true},
			InputSchema:  configRemoveServerInputSchema,
			OutputSchema: resultOutputSchema,
		},
		Run: func(_ context.Context, _ server.Runtime, a ConfigRemoveServerArgs) server.Result {
			if a.Name == "" {
				return failure(app.CodeBadRequest, "name is required", nil)
			}
			path, lock, err := configPaths()
			if err != nil {
				return failure(app.CodeInternalError, err.Error(), nil)
			}
			if _, err = appconfig.Update(path, lock, func(cfg *appconfig.Config) error {
				return cfg.RemoveServer(a.Name, a.Confirm)
			}); err != nil {
				return configFailure(err)
			}
			return success("Server removed from config.json.", map[string]interface{}{"removed": a.Name})
		},
	}
}

func valueOr(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

func intOr(v, def int) int {
	if v <= 0 {
		return def
	}
	return v
}
