package server

import (
	"context"

	"github.com/diandian921/sofarpc-mcp/internal/mcp/proto"
)

// SessionRuntime forwards progress and logging to the live proto.Session bound
// to the request context. Tools depend only on the Runtime interface, so they
// never import proto.
type SessionRuntime struct{}

var _ Runtime = SessionRuntime{}

// Progress emits notifications/progress when the client supplied a progress
// token; otherwise it is a no-op.
func (SessionRuntime) Progress(ctx context.Context, message string, percent float64) {
	s, ok := proto.SessionFromContext(ctx)
	if !ok {
		return
	}
	token, ok := proto.ProgressTokenFromContext(ctx)
	if !ok {
		return
	}
	s.SendProgress(token, percent, message)
}

// Log emits a notifications/message at the given level.
func (SessionRuntime) Log(ctx context.Context, level, message string) {
	s, ok := proto.SessionFromContext(ctx)
	if !ok {
		return
	}
	s.SendLog(level, "sofarpc", message)
}
