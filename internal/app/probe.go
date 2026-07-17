package app

import (
	"context"
	"net"
	"time"

	"github.com/diandian921/sofarpc-mcp/internal/appconfig"
)

type ProbeInput struct {
	Project   string
	Profile   string
	Server    string
	Address   string
	Service   string
	TimeoutMS int
}

type ProbeResult struct {
	Project     string                 `json:"project,omitempty"`
	Profile     string                 `json:"profile,omitempty"`
	Server      string                 `json:"server,omitempty"`
	Address     string                 `json:"address"`
	Service     string                 `json:"service,omitempty"`
	Reachable   bool                   `json:"reachable"`
	ElapsedMS   int64                  `json:"elapsedMs"`
	TimeoutMS   int                    `json:"timeoutMs"`
	Diagnostics Diagnostics            `json:"diagnostics,omitempty"`
	Error       *ExecutionError        `json:"error,omitempty"`
	Code        string                 `json:"-"`
	Meta        map[string]interface{} `json:"meta,omitempty"`
}

func (s *Service) ProbeEndpoint(ctx context.Context, input ProbeInput) ProbeResult {
	address := input.Address
	serverName := input.Server
	projectName := input.Project
	profileName := input.Profile
	timeoutMS := input.TimeoutMS
	endpointSource := "explicit-address"
	if address == "" {
		cfg, err := s.loadConfig()
		if err != nil {
			return probeFailure(input, CodeInternalError, err, timeoutMS, endpointSource)
		}
		selection, err := SelectServer(cfg, ServerSelector{
			Project: input.Project, Profile: input.Profile, Server: input.Server, Required: true,
		})
		if err != nil {
			return probeFailure(input, CodeConnectFailed, err, timeoutMS, "configured-server")
		}
		if !selection.Found {
			return probeFailure(input, CodeConnectFailed, errServerRequired(), timeoutMS, "configured-server")
		}
		serverName = selection.Name
		projectName = selection.Server.Project
		profileName = selection.Server.Profile
		address = selection.Server.Address
		timeoutMS = input.TimeoutMS
		if timeoutMS <= 0 {
			timeoutMS = selection.Server.TimeoutMS
		}
		endpointSource = "configured-server"
	}
	if timeoutMS <= 0 {
		timeoutMS = appconfig.DefaultServerTimeoutMS
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeoutMS)*time.Millisecond)
	defer cancel()

	start := time.Now()
	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "tcp", address)
	elapsed := time.Since(start)
	result := ProbeResult{
		Project:   projectName,
		Profile:   profileName,
		Server:    serverName,
		Address:   address,
		Service:   input.Service,
		Reachable: err == nil,
		ElapsedMS: elapsed.Milliseconds(),
		TimeoutMS: timeoutMS,
		Diagnostics: Diagnostics{
			Timing: map[string]int64{"dialMs": elapsed.Milliseconds()},
			Resolution: map[string]interface{}{
				"project":        projectName,
				"profile":        profileName,
				"server":         serverName,
				"service":        input.Service,
				"endpointSource": endpointSource,
				"address":        address,
			},
		},
		Meta: map[string]interface{}{"runtime": "go", "transport": "tcp-dial"},
	}
	if err == nil {
		_ = conn.Close()
		return result
	}
	result.Code = errorCode(err)
	result.Error = &ExecutionError{
		Message: err.Error(),
		Details: map[string]interface{}{
			"address":      address,
			"service":      input.Service,
			"rpcTimeoutMs": timeoutMS,
		},
	}
	return result
}

func errServerRequired() error {
	return &DomainError{Kind: ErrEndpointNotFound, Message: "server or address is required"}
}

func probeFailure(input ProbeInput, code string, err error, timeoutMS int, source string) ProbeResult {
	if timeoutMS <= 0 {
		timeoutMS = appconfig.DefaultServerTimeoutMS
	}
	return ProbeResult{
		Project:   input.Project,
		Profile:   input.Profile,
		Server:    input.Server,
		Address:   input.Address,
		Service:   input.Service,
		Reachable: false,
		TimeoutMS: timeoutMS,
		Diagnostics: Diagnostics{Resolution: map[string]interface{}{
			"project":        input.Project,
			"profile":        input.Profile,
			"server":         input.Server,
			"service":        input.Service,
			"endpointSource": source,
			"address":        input.Address,
		}},
		Error: &ExecutionError{Message: err.Error(), Details: DomainErrorDetails(err)},
		Code:  code,
		Meta:  map[string]interface{}{"runtime": "go"},
	}
}
