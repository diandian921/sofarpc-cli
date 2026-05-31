package proto

// MCP logging levels (notifications/message).
const (
	LevelDebug   = "debug"
	LevelInfo    = "info"
	LevelWarning = "warning"
	LevelError   = "error"
)

// SendLog emits a notifications/message with the given level and structured data.
// logger is optional and identifies the emitting component.
func (s *Session) SendLog(level, logger string, data interface{}) {
	params := map[string]interface{}{"level": level, "data": data}
	if logger != "" {
		params["logger"] = logger
	}
	_ = s.transport.Write(outNotification{JSONRPC: "2.0", Method: "notifications/message", Params: params})
}
