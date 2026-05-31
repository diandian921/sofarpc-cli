package mcp

import (
	"context"
	"io"

	"github.com/diandian921/sofarpc-cli/internal/mcp/proto"
)

// serverInstructions is the server-level guidance returned at initialize.
const serverInstructions = "Run sofarpc_resolve before sofarpc_invoke. When multiple servers exist, always pass `server`. Use sofarpc_describe with query=... to find a service FQN before invoking, and sofarpc_invoke_plan to validate arguments without sending a request."

// serverInfo identifies this server in the initialize response.
func (s *Server) serverInfo() proto.ServerInfo {
	return proto.ServerInfo{
		Name:        "sofarpc-mcp",
		Title:       "SofaRPC Direct Invoker",
		Version:     s.BuildVersion,
		Description: "Invoke SofaRPC services directly over BOLT/Hessian2 from local Java source schema.",
	}
}

// serverCapabilities declares the capabilities advertised at initialize.
func (s *Server) serverCapabilities() proto.Capabilities {
	return proto.Capabilities{Tools: &proto.ToolsCapability{}, Logging: &struct{}{}}
}

// dispatcher adapts the Server's method handlers to the proto.Session contract.
// The session owns framing, lifecycle, cancellation, and progress/logging; the
// dispatcher owns tools/list and tools/call semantics.
type dispatcher struct {
	server *Server
	stderr io.Writer
}

func (d *dispatcher) Async(req proto.Request) bool {
	if req.IsNotification() || req.Method != "tools/call" {
		return false
	}
	var call struct {
		Name string `json:"name"`
	}
	if err := decodeJSON(req.Params, &call); err != nil {
		return false
	}
	return d.server.toolRegistry().Async(call.Name)
}

func (d *dispatcher) Handle(ctx context.Context, req proto.Request) (proto.Response, bool) {
	return handleWithRecover(req, d.stderr, func() (proto.Response, bool) {
		return d.server.handle(ctx, req)
	})
}
