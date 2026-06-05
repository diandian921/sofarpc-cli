package tools

import (
	"encoding/json"

	"github.com/diandian921/sofarpc-mcp/internal/app"
)

// InvokeArgs are the arguments shared by sofarpc_invoke and sofarpc_invoke_plan.
// paramTypes/orderedArguments are the advertised names; types/args are accepted
// aliases (declared struct fields, so they decode even though the schema does not
// list them).
type InvokeArgs struct {
	Server           string                 `json:"server,omitempty"`
	Project          string                 `json:"project,omitempty"`
	Service          string                 `json:"service"`
	Method           string                 `json:"method"`
	ParamTypes       []string               `json:"paramTypes,omitempty"`
	Types            []string               `json:"types,omitempty"`
	OrderedArguments []interface{}          `json:"orderedArguments,omitempty"`
	Args             []interface{}          `json:"args,omitempty"`
	Arguments        map[string]interface{} `json:"arguments,omitempty"`
	TimeoutMS        int                    `json:"timeoutMs,omitempty"`
	RawResult        bool                   `json:"rawResult,omitempty"`
}

func (a InvokeArgs) toInput() app.InvocationInput {
	paramTypes := a.ParamTypes
	if len(paramTypes) == 0 {
		paramTypes = a.Types
	}
	input := app.InvocationInput{
		Project:    a.Project,
		Server:     a.Server,
		Service:    a.Service,
		Method:     a.Method,
		ParamTypes: paramTypes,
		TimeoutMS:  a.TimeoutMS,
		RawResult:  a.RawResult,
	}
	ordered := a.OrderedArguments
	if ordered == nil {
		ordered = a.Args
	}
	if ordered != nil {
		input.OrderedArguments = ordered
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
