package mcp

import (
	"context"

	"github.com/diandian921/sofarpc-cli/internal/mcp/proto"
)

// serverInstructions is the server-level guidance returned at initialize.
// Populated with real prompt guidance in a later step.
const serverInstructions = ""

// serverInfo identifies this server in the initialize response.
func (s *Server) serverInfo() proto.ServerInfo {
	return proto.ServerInfo{Name: "sofarpc-mcp", Version: s.BuildVersion}
}

// serverCapabilities declares the capabilities advertised at initialize.
func (s *Server) serverCapabilities() proto.Capabilities {
	return proto.Capabilities{Tools: &proto.ToolsCapability{}, Logging: &struct{}{}}
}

// dispatcher adapts the Server's method handlers to the proto.Session contract.
// The session owns framing, lifecycle, cancellation, and progress/logging; the
// dispatcher owns tools/list and tools/call semantics.
type dispatcher struct{ server *Server }

func (d *dispatcher) Async(req proto.Request) bool { return shouldRunAsync(req) }

func (d *dispatcher) Handle(ctx context.Context, req proto.Request) (proto.Response, bool) {
	return handleWithRecover(req, func() (proto.Response, bool) {
		return d.server.handle(ctx, req)
	})
}
