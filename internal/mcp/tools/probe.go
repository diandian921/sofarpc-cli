package tools

import "encoding/json"

// Probe tool display text, shared with the SDK-native AddProbe.
const (
	probeTitle       = "SofaRPC Probe"
	probeDescription = "Probe TCP reachability for a configured server or explicit address; this does not prove an interface or method exists."
	probeSummary     = "Probe completed. Success only means the TCP transport path was reachable; it does not prove the remote interface or method exists."
)

// ProbeArgs are the arguments for sofarpc_probe.
type ProbeArgs struct {
	Server    string `json:"server,omitempty"`
	Address   string `json:"address,omitempty"`
	Service   string `json:"service,omitempty"`
	Project   string `json:"project,omitempty"`
	TimeoutMS int    `json:"timeoutMs,omitempty"`
}

var probeInputSchema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "server": {"type": "string", "description": "Optional configured server name."},
    "address": {"type": "string", "description": "Optional explicit host:port. Used when server is omitted."},
    "service": {"type": "string", "description": "Optional service FQN for labeling diagnostics."},
    "project": {"type": "string", "description": "Optional project name used to infer a single bound server when server is omitted."},
    "timeoutMs": {"type": "integer", "description": "Optional total timeout in milliseconds."}
  }
}`)
