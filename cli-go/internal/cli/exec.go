package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"

	"github.com/sofarpc/cli-go/internal/launcher"
	"github.com/sofarpc/cli-go/internal/protocol"
)

// runExec implements `sofarpc exec --stdin`: the agent-first entrypoint. It reads exactly one
// envelope from stdin, hands it to a warm daemon (spawning one if needed), and writes the
// envelope returned by the daemon to stdout. One request, one response, one line of JSON each.
func runExec(args []string, env Env) int {
	fs := flag.NewFlagSet("exec", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	useStdin := fs.Bool("stdin", false, "read one request envelope from stdin")
	noSpawn := fs.Bool("no-spawn", false, "fail instead of spawning the daemon")
	jar := fs.String("jar", "", "path to sofarpcd.jar (overrides autodiscovery)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if !*useStdin {
		fmt.Fprintln(env.Stderr, "exec: only --stdin is supported in V1")
		return 2
	}

	req, err := readRequest(env.Stdin)
	if err != nil {
		writeLocalFailure(env.Stdout, "", protocol.CodeBadRequest, "read stdin request: "+err.Error())
		return 1
	}

	resp, err := dispatch(req, execConfig(env, *noSpawn, *jar))
	if err != nil {
		writeLocalFailure(env.Stdout, req.RequestID, protocol.CodeDaemonUnavailable, err.Error())
		return 1
	}
	if err := writeResponse(env.Stdout, resp); err != nil {
		fmt.Fprintln(env.Stderr, "write response:", err)
		return 1
	}
	if !resp.OK {
		return 1
	}
	return 0
}

func readRequest(r io.Reader) (protocol.Request, error) {
	var req protocol.Request
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		return req, err
	}
	if req.Op == "" {
		return req, fmt.Errorf("missing op")
	}
	if req.RequestID == "" {
		req.RequestID = protocol.NewRequestID(req.Op)
	}
	if len(req.Payload) == 0 {
		req.Payload = json.RawMessage(`{}`)
	}
	return req, nil
}

func writeResponse(w io.Writer, resp *protocol.Response) error {
	body, err := json.Marshal(resp)
	if err != nil {
		return err
	}
	body = append(body, '\n')
	_, err = w.Write(body)
	return err
}

// writeLocalFailure emits a daemon-shaped error envelope on stdout when the client fails
// before ever reaching the daemon. Agents that only parse stdout get a consistent shape.
func writeLocalFailure(w io.Writer, requestID, code, message string) {
	resp := &protocol.Response{
		RequestID: requestID,
		OK:        false,
		Code:      code,
		Error:     &protocol.ResponseError{Message: message},
	}
	_ = writeResponse(w, resp)
}

func dispatch(req protocol.Request, cfg launcher.Config) (*protocol.Response, error) {
	conn, err := launcher.Connect(cfg)
	if err != nil {
		return nil, err
	}
	return conn.Client.Call(req)
}

func execConfig(env Env, noSpawn bool, jar string) launcher.Config {
	cfg, err := launcher.DefaultConfig(env.BuildVersion)
	if err != nil {
		return launcher.Config{NoSpawn: noSpawn, JarPath: jar, BuildVersion: env.BuildVersion}
	}
	cfg.NoSpawn = noSpawn
	if jar != "" {
		cfg.JarPath = jar
	}
	return cfg
}

