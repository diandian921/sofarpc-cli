package proto

// MCP logging levels (notifications/message), ordered by increasing severity.
const (
	LevelDebug   = "debug"
	LevelInfo    = "info"
	LevelWarning = "warning"
	LevelError   = "error"
)

// logLevelRank orders the syslog-style levels MCP uses; a higher rank is more
// severe. Unknown levels rank 0 (always emitted).
var logLevelRank = map[string]int{
	"debug": 0, "info": 1, "notice": 2, "warning": 3,
	"error": 4, "critical": 5, "alert": 6, "emergency": 7,
}

// handleSetLevel handles logging/setLevel: it records the minimum level the
// client wants and acknowledges. It is gated like other post-handshake requests.
func (s *Session) handleSetLevel(req Request) {
	if s.getState() != StateReady {
		if !req.IsNotification() {
			_ = s.transport.Write(errorResponse(req.ID, CodeServerNotInitialized, "server not initialized"))
		}
		return
	}
	var p struct {
		Level string `json:"level"`
	}
	if err := DecodeParams(req.Params, &p); err != nil {
		if !req.IsNotification() {
			_ = s.transport.Write(errorResponse(req.ID, CodeInvalidParams, "invalid logging/setLevel params"))
		}
		return
	}
	s.logMu.Lock()
	s.logLevel = p.Level
	s.logMu.Unlock()
	if !req.IsNotification() {
		_ = s.transport.Write(Response{JSONRPC: "2.0", ID: req.ID, Result: map[string]interface{}{}})
	}
}

// SendLog emits a notifications/message with the given level and structured data,
// suppressed when below the client-requested minimum level. logger is optional
// and identifies the emitting component.
func (s *Session) SendLog(level, logger string, data interface{}) {
	s.logMu.Lock()
	minLevel := s.logLevel
	s.logMu.Unlock()
	if minLevel != "" && logLevelRank[level] < logLevelRank[minLevel] {
		return
	}
	params := map[string]interface{}{"level": level, "data": data}
	if logger != "" {
		params["logger"] = logger
	}
	_ = s.transport.Write(outNotification{JSONRPC: "2.0", Method: "notifications/message", Params: params})
}
