package app

import (
	"context"
	"net"
	"time"

	"github.com/sofarpc/cli/internal/appconfig"
)

type ProbeInput struct {
	Project   string
	Server    string
	Address   string
	Service   string
	TimeoutMS int
}

type ProbeResult struct {
	Project     string                 `json:"project,omitempty"`
	Server      string                 `json:"server,omitempty"`
	Address     string                 `json:"address"`
	Service     string                 `json:"service,omitempty"`
	Reachable   bool                   `json:"reachable"`
	ElapsedMS   int64                  `json:"elapsedMs"`
	TimeoutMS   int                    `json:"timeoutMs"`
	Diagnostics Diagnostics            `json:"diagnostics,omitempty"`
	Error       *ExecutionError        `json:"error,omitempty"`
	Meta        map[string]interface{} `json:"meta,omitempty"`
}

func (s *Service) ProbeEndpoint(ctx context.Context, input ProbeInput) ProbeResult {
	address := input.Address
	serverName := input.Server
	projectName := input.Project
	timeoutMS := input.TimeoutMS
	endpointSource := "explicit-address"
	if address == "" {
		cfg, err := s.loadConfig()
		if err != nil {
			return probeFailure(input, CodeInternalError, err, timeoutMS, endpointSource)
		}
		name, server, hasServer, err := resolveServer(cfg, input.Project, input.Server, true)
		if err != nil {
			return probeFailure(input, CodeConnectFailed, err, timeoutMS, "configured-server")
		}
		if !hasServer {
			return probeFailure(input, CodeConnectFailed, errServerRequired(), timeoutMS, "configured-server")
		}
		serverName = name
		projectName = server.Project
		address = server.Address
		timeoutMS = input.TimeoutMS
		if timeoutMS <= 0 {
			timeoutMS = server.TimeoutMS
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
		Server:    input.Server,
		Address:   input.Address,
		Service:   input.Service,
		Reachable: false,
		TimeoutMS: timeoutMS,
		Diagnostics: Diagnostics{Resolution: map[string]interface{}{
			"project":        input.Project,
			"server":         input.Server,
			"service":        input.Service,
			"endpointSource": source,
			"address":        input.Address,
		}},
		Error: &ExecutionError{Message: err.Error()},
		Meta:  map[string]interface{}{"runtime": "go", "code": code},
	}
}
