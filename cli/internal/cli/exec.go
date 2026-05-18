package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"

	"github.com/sofarpc/cli/internal/invoker"
	"github.com/sofarpc/cli/internal/protocol"
)

// runExec implements `sofarpc exec --stdin`: it reads exactly one envelope from
// stdin, executes it through the pure-Go runtime, and writes one response to stdout.
func runExec(args []string, env Env) int {
	fs := flag.NewFlagSet("exec", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	useStdin := fs.Bool("stdin", false, "read one request envelope from stdin")
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
	if err := resolveEnvelopeAddress(&req); err != nil {
		writeLocalFailure(env.Stdout, req.RequestID, protocol.CodeBadRequest, err.Error())
		return 1
	}

	resp, err := dispatch(req)
	if err != nil {
		writeLocalFailure(env.Stdout, req.RequestID, protocol.CodeInternalError, err.Error())
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

// writeLocalFailure emits a response envelope on stdout when the client fails
// before reaching the runtime. Agents that only parse stdout get a consistent shape.
func writeLocalFailure(w io.Writer, requestID, code, message string) {
	writeLocalFailureWithDetails(w, requestID, code, message, nil)
}

func writeLocalFailureWithDetails(w io.Writer, requestID, code, message string, details map[string]interface{}) {
	resp := &protocol.Response{
		RequestID: requestID,
		OK:        false,
		Code:      code,
		Error: &protocol.ResponseError{
			Message: message,
			Details: details,
		},
	}
	_ = writeResponse(w, resp)
}

func dispatch(req protocol.Request) (*protocol.Response, error) {
	return invoker.DirectRequest(req)
}
