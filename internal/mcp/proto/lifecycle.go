package proto

import "strings"

// State is the lifecycle position of a session.
type State int

const (
	// StateNew is a freshly spawned session; only initialize is accepted.
	StateNew State = iota
	// StateInitializing has sent the initialize response and awaits
	// notifications/initialized.
	StateInitializing
	// StateReady accepts all methods.
	StateReady
	// StateClosing is draining after stdin EOF.
	StateClosing
)

// SupportedVersions lists the MCP protocol versions this server speaks, newest
// first. NegotiateVersion degrades to SupportedVersions[0] when the client asks
// for something unknown.
var SupportedVersions = []string{"2025-11-25", "2025-06-18", "2025-03-26", "2024-11-05"}

// NegotiateVersion returns the version to advertise back to the client. A
// supported request is echoed; an unsupported one degrades to the latest
// supported version (per the 2025-11-25 lifecycle, where the client then decides
// whether to proceed). A missing protocolVersion is an invalid initialize.
func NegotiateVersion(client string) (string, *Error) {
	client = strings.TrimSpace(client)
	if client == "" {
		return "", &Error{Code: CodeInvalidParams, Message: "initialize requires protocolVersion"}
	}
	for _, v := range SupportedVersions {
		if v == client {
			return v, nil
		}
	}
	return SupportedVersions[0], nil
}

// ServerInfo identifies the server in the initialize response.
type ServerInfo struct {
	Name        string `json:"name"`
	Title       string `json:"title,omitempty"`
	Version     string `json:"version"`
	Description string `json:"description,omitempty"`
}

// ToolsCapability declares tool support. ListChanged is false: the tool set is static.
type ToolsCapability struct {
	ListChanged bool `json:"listChanged"`
}

// Capabilities declares the capabilities advertised at initialize. Only the
// non-nil members are emitted.
type Capabilities struct {
	Tools   *ToolsCapability `json:"tools,omitempty"`
	Logging *struct{}        `json:"logging,omitempty"`
}

// initializeResult is the payload returned from a successful initialize.
type initializeResult struct {
	ProtocolVersion string       `json:"protocolVersion"`
	Capabilities    Capabilities `json:"capabilities"`
	ServerInfo      ServerInfo   `json:"serverInfo"`
	Instructions    string       `json:"instructions,omitempty"`
}
