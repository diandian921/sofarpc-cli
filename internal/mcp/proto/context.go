package proto

import (
	"context"
	"encoding/json"
)

type ctxKey int

const (
	ctxKeyProgressToken ctxKey = iota
	ctxKeySession
)

// WithProgressToken attaches the request's progress token to ctx so a Runtime
// can emit notifications/progress against it.
func WithProgressToken(ctx context.Context, token json.RawMessage) context.Context {
	return context.WithValue(ctx, ctxKeyProgressToken, token)
}

// ProgressTokenFromContext returns the progress token, if the client supplied one.
func ProgressTokenFromContext(ctx context.Context) (json.RawMessage, bool) {
	v, ok := ctx.Value(ctxKeyProgressToken).(json.RawMessage)
	return v, ok && len(v) > 0
}

// withSession attaches the live session so layer 2's Runtime can reach
// SendProgress / SendLog without importing the dispatch wiring.
func withSession(ctx context.Context, s *Session) context.Context {
	return context.WithValue(ctx, ctxKeySession, s)
}

// SessionFromContext returns the live session bound to ctx.
func SessionFromContext(ctx context.Context) (*Session, bool) {
	s, ok := ctx.Value(ctxKeySession).(*Session)
	return s, ok
}
