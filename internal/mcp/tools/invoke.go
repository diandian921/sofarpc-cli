package tools

import (
	"encoding/json"

	"github.com/diandian921/sofarpc-mcp/internal/app"
)

// InvokeArgs are the arguments shared by sofarpc_invoke and sofarpc_invoke_plan.
// Only the schema-advertised names are accepted: the undocumented types/args aliases
// were removed so the input schema and the handler agree (decodeStrict rejects any
// other key as an unknown field).
type InvokeArgs struct {
	Server           string                 `json:"server,omitempty"`
	Project          string                 `json:"project,omitempty"`
	Service          string                 `json:"service"`
	Method           string                 `json:"method"`
	ParamTypes       []string               `json:"paramTypes,omitempty"`
	OrderedArguments []interface{}          `json:"orderedArguments,omitempty"`
	Arguments        map[string]interface{} `json:"arguments,omitempty"`
	TimeoutMS        int                    `json:"timeoutMs,omitempty"`
	RawResult        bool                   `json:"rawResult,omitempty"`
}

func (a InvokeArgs) toInput() app.InvocationInput {
	input := app.InvocationInput{
		Project:    a.Project,
		Server:     a.Server,
		Service:    a.Service,
		Method:     a.Method,
		ParamTypes: a.ParamTypes,
		TimeoutMS:  a.TimeoutMS,
		RawResult:  a.RawResult,
	}
	if a.OrderedArguments != nil {
		input.OrderedArguments = a.OrderedArguments
		input.HasOrderedArguments = true
		return input
	}
	if a.Arguments != nil {
		input.NamedArguments = a.Arguments
	}
	return input
}

var invokeInputSchema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["service", "method"],
  "properties": {
    "server": {"type": "string", "description": "Configured server name. Optional only when exactly one matching server can be inferred."},
    "project": {"type": "string", "description": "Optional project name used to infer a single bound server."},
    "service": {"type": "string", "description": "Service interface FQN."},
    "method": {"type": "string", "description": "Method name."},
    "paramTypes": {"type": "array", "items": {"type": "string"}, "description": "Optional Java parameter type FQNs for overload disambiguation."},
    "orderedArguments": {"type": "array", "description": "Arguments in method parameter order."},
    "arguments": {"type": "object", "additionalProperties": true, "description": "Named arguments keyed by Java parameter name, or a single DTO object when the method has one parameter."},
    "timeoutMs": {"type": "integer", "description": "Optional total timeout in milliseconds."},
    "rawResult": {"type": "boolean", "description": "When true, include the decoded Java object shape alongside the flattened result."}
  }
}`)

var invokePlanInputSchema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["service", "method"],
  "properties": {
    "server": {"type": "string", "description": "Configured server name. Optional only when exactly one matching server can be inferred."},
    "project": {"type": "string", "description": "Optional project name used to infer a single bound server."},
    "service": {"type": "string", "description": "Service interface FQN."},
    "method": {"type": "string", "description": "Method name."},
    "paramTypes": {"type": "array", "items": {"type": "string"}, "description": "Optional Java parameter type FQNs for overload disambiguation."},
    "orderedArguments": {"type": "array", "description": "Arguments in method parameter order."},
    "arguments": {"type": "object", "additionalProperties": true, "description": "Named arguments keyed by Java parameter name, or a single DTO object when the method has one parameter."},
    "timeoutMs": {"type": "integer", "description": "Optional total timeout in milliseconds."}
  }
}`)
