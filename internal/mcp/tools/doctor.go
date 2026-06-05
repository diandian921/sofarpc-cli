package tools

import "encoding/json"

// DoctorArgs are the arguments for sofarpc_doctor.
type DoctorArgs struct {
	Project string `json:"project,omitempty"`
	Server  string `json:"server,omitempty"`
	Service string `json:"service,omitempty"`
	Method  string `json:"method,omitempty"`
}

var doctorInputSchema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "project": {"type": "string", "description": "Optional project name."},
    "server": {"type": "string", "description": "Optional server name."},
    "service": {"type": "string", "description": "Optional service interface FQN."},
    "method": {"type": "string", "description": "Optional method filter."}
  }
}`)
