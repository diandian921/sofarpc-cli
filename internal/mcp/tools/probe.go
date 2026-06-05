package tools

import (
	"context"
	"encoding/json"

	"github.com/diandian921/sofarpc-mcp/internal/app"
	"github.com/diandian921/sofarpc-mcp/internal/mcp/server"
)

// Probe tool display text, shared by the legacy ProbeTool and the SDK-native
// AddProbe so the two paths cannot drift during the go-sdk migration.
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

// ProbeTool probes TCP reachability. Reaching the host does not prove the remote
// interface or method exists.
func ProbeTool(appSvc *app.Service) server.Tool[ProbeArgs] {
	return server.Tool[ProbeArgs]{
		Spec: server.ToolSpec{
			Name:         "sofarpc_probe",
			Title:        probeTitle,
			Description:  probeDescription,
			Annotations:  server.Annotations{ReadOnlyHint: true, IdempotentHint: true, OpenWorldHint: true},
			InputSchema:  probeInputSchema,
			OutputSchema: resultOutputSchema,
			Async:        true,
		},
		Run: func(ctx context.Context, _ server.Runtime, a ProbeArgs) server.Result {
			probe := appSvc.ProbeEndpoint(ctx, app.ProbeInput{
				Project:   a.Project,
				Server:    a.Server,
				Address:   a.Address,
				Service:   a.Service,
				TimeoutMS: a.TimeoutMS,
			})
			result := app.RenderProbe(probe)
			result.RequestID = app.NewRequestID("ping")
			return rendered(result, probeSummary)
		},
	}
}
