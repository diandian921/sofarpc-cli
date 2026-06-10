package tools

import (
	"encoding/json"

	"github.com/diandian921/sofarpc-mcp/internal/app"
	"github.com/diandian921/sofarpc-mcp/internal/presentation"
)

// InvokeArgs are the arguments shared by sofarpc_invoke and sofarpc_invoke_plan.
// Only the schema-advertised names are accepted: the undocumented types/args aliases
// were removed so the input schema and the handler agree (decodeStrict rejects any
// other key as an unknown field).
type InvokeArgs struct {
	Server           string                   `json:"server,omitempty"`
	Project          string                   `json:"project,omitempty"`
	Service          string                   `json:"service"`
	Method           string                   `json:"method"`
	ParamTypes       []string                 `json:"paramTypes,omitempty"`
	OrderedArguments []interface{}            `json:"orderedArguments,omitempty"`
	Arguments        map[string]interface{}   `json:"arguments,omitempty"`
	TimeoutMS        int                      `json:"timeoutMs,omitempty"`
	RawResult        bool                     `json:"rawResult,omitempty"`
	Assertions       []presentation.Assertion `json:"assertions,omitempty"`
	ResultPath       string                   `json:"resultPath,omitempty"`
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
		Assertions: a.Assertions,
		ResultPath: a.ResultPath,
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
    "rawResult": {"type": "boolean", "description": "When true, include the decoded Java object shape alongside the flattened result."},
    "assertions": {
      "type": "array",
      "description": "Optional response checks. Each runs a $.path lookup on the flattened result; any failure makes the call isError while still returning data.result and data.assertions.",
      "items": {
        "type": "object",
        "additionalProperties": false,
        "required": ["path"],
        "properties": {
          "path": {"type": "string", "description": "JSONPath-lite into the flattened result, e.g. $.status or $.user.name."},
          "equals": {"description": "Expected value at path (any JSON type)."},
          "exists": {"type": "boolean", "description": "Assert the path exists (true) or is absent (false); use instead of equals."}
        }
      }
    },
    "resultPath": {"type": "string", "description": "Optional $.path; when set, data.result is narrowed to just that subtree (rawResult still returns the full tree)."}
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
