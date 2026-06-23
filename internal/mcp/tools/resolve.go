package tools

import "encoding/json"

// ResolveArgs are the arguments for sofarpc_resolve.
type ResolveArgs struct {
	Project   string `json:"project,omitempty"`
	Profile   string `json:"profile,omitempty"`
	Server    string `json:"server,omitempty"`
	TimeoutMS int    `json:"timeoutMs,omitempty"`
}

var resolveInputSchema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "project": {"type": "string", "description": "Optional configured project name."},
    "profile": {"type": "string", "description": "Optional project profile name. Requires project when server is omitted."},
    "server": {"type": "string", "description": "Optional configured server name."},
    "timeoutMs": {"type": "integer", "description": "Optional timeout override to show on the resolved endpoint."}
  }
}`)
