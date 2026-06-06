package tools

import "encoding/json"

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

// ----- sofarpc_config_save_project -----

// ConfigSaveProjectArgs are the arguments for sofarpc_config_save_project.
type ConfigSaveProjectArgs struct {
	Name            string   `json:"name"`
	WorkspaceRoot   string   `json:"workspaceRoot"`
	ServicePrefixes []string `json:"servicePrefixes,omitempty"`
	Overwrite       bool     `json:"overwrite,omitempty"`
	DryRun          bool     `json:"dryRun,omitempty"`
}

var configSaveProjectInputSchema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["name", "workspaceRoot"],
  "properties": {
    "name": {"type": "string", "description": "Project name."},
    "workspaceRoot": {"type": "string", "description": "Absolute or ~-relative local source root."},
    "servicePrefixes": {"type": "array", "items": {"type": "string"}, "description": "Optional Java service package prefixes."},
    "overwrite": {"type": "boolean", "description": "Allow replacing an existing project."},
    "dryRun": {"type": "boolean", "description": "Validate and preview the entry without writing config.json."}
  }
}`)

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
	DryRun      bool              `json:"dryRun,omitempty"`
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
    "overwrite": {"type": "boolean", "description": "Allow replacing an existing server."},
    "dryRun": {"type": "boolean", "description": "Validate and preview the entry without writing config.json."}
  }
}`)

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
