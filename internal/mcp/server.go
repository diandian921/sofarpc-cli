package mcp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/diandian921/sofarpc-mcp/internal/app"
	"github.com/diandian921/sofarpc-mcp/internal/appconfig"
	"github.com/diandian921/sofarpc-mcp/internal/schema"
)

// Server is the stdio MCP server facade. Its public surface (fields, Run, SelfTest)
// is unchanged so cli/mcp.go and callers do not churn; internally it is backed by the
// official modelcontextprotocol go-sdk (see newSDKServer).
type Server struct {
	BuildVersion       string
	Stdin              io.Reader
	Stdout             io.Writer
	Stderr             io.Writer
	DisableConfigWrite bool
	App                *app.Service
}

// Run serves the MCP protocol over the configured streams until the client
// disconnects. It uses an IOTransport (not StdioTransport) so injected Stdin/Stdout
// are honored — production passes os streams, tests pass buffers.
//
// Only a failure to bring up the session (Connect) is a fatal exit 1. Once serving,
// the session ending — the host closing stdin, EOF, a peer disconnect — is the
// normal terminal condition for a stdio server, so it exits 0 (logging any non-EOF
// reason to stderr for diagnostics).
func (s *Server) Run() int {
	_ = schema.CleanupUnused(7 * 24 * time.Hour)

	in := s.Stdin
	if in == nil {
		in = strings.NewReader("")
	}
	out := s.Stdout
	if out == nil {
		out = io.Discard
	}
	stderr := s.Stderr
	if stderr == nil {
		stderr = io.Discard
	}

	srv := newSDKServer(s.application(), s.BuildVersion, !s.DisableConfigWrite, stderr)
	transport := &mcpsdk.IOTransport{Reader: io.NopCloser(in), Writer: nopWriteCloser{out}}
	session, err := srv.Connect(context.Background(), transport, nil)
	if err != nil {
		fmt.Fprintln(stderr, "mcp: connect failed:", err)
		return 1
	}
	if err := session.Wait(); err != nil && !errors.Is(err, io.EOF) {
		fmt.Fprintln(stderr, "mcp: session ended:", err)
	}
	return 0
}

// nopWriteCloser adapts an io.Writer to io.WriteCloser for IOTransport; the
// underlying stream (os.Stdout in production) is owned by the caller, so Close is
// a no-op.
type nopWriteCloser struct{ io.Writer }

func (nopWriteCloser) Close() error { return nil }

// SelfTest brings up the server machinery and drives a real initialize → tools/list
// → tools/call(sofarpc_config_list) handshake over in-memory transports, so a broken
// config or registration fails here instead of at first agent use.
func (s *Server) SelfTest() error {
	if _, err := appconfig.DefaultPath(); err != nil {
		return fmt.Errorf("config path: %w", err)
	}
	if s.application() == nil {
		return errors.New("app service is nil")
	}
	stderr := s.Stderr
	if stderr == nil {
		stderr = io.Discard
	}

	ctx := context.Background()
	serverTransport, clientTransport := mcpsdk.NewInMemoryTransports()
	srv := newSDKServer(s.application(), s.BuildVersion, !s.DisableConfigWrite, stderr)
	if _, err := srv.Connect(ctx, serverTransport, nil); err != nil {
		return fmt.Errorf("server connect: %w", err)
	}

	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "sofarpc-selftest", Version: s.BuildVersion}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		return fmt.Errorf("initialize handshake: %w", err)
	}
	defer func() { _ = session.Close() }()

	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		return fmt.Errorf("tools/list: %w", err)
	}
	if len(tools.Tools) == 0 {
		return errors.New("no tools registered")
	}

	res, err := session.CallTool(ctx, &mcpsdk.CallToolParams{Name: "sofarpc_config_list"})
	if err != nil {
		return fmt.Errorf("tools/call sofarpc_config_list: %w", err)
	}
	if res.IsError {
		return errors.New("sofarpc_config_list returned an error result")
	}
	return nil
}

func (s *Server) application() *app.Service {
	if s.App != nil {
		return s.App
	}
	return app.New(nil)
}
