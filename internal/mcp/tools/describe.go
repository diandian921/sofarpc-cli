package tools

import "encoding/json"

// DescribeArgs are the arguments for sofarpc_describe.
type DescribeArgs struct {
	Project            string `json:"project,omitempty"`
	Server             string `json:"server,omitempty"`
	Query              string `json:"query,omitempty"`
	Service            string `json:"service,omitempty"`
	Method             string `json:"method,omitempty"`
	Limit              int    `json:"limit,omitempty"`
	IncludeOutOfPrefix bool   `json:"includeOutOfPrefix,omitempty"`
}

var describeInputSchema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "project": {"type": "string", "description": "Optional project name. Required when multiple projects are configured and server is omitted."},
    "server": {"type": "string", "description": "Optional server name used to infer the bound project."},
    "query": {"type": "string", "description": "Natural language or identifier query for search mode."},
    "service": {"type": "string", "description": "Service interface FQN for describe mode."},
    "method": {"type": "string", "description": "Optional method filter for describe mode."},
    "limit": {"type": "integer", "description": "Max search candidates; default 5, max 20."},
    "includeOutOfPrefix": {"type": "boolean", "description": "Include services outside configured servicePrefixes."}
  }
}`)
