package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/sofarpc/cli/internal/app"
	"github.com/sofarpc/cli/internal/direct"
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
	switch req.Op {
	case protocol.OpInvoke:
		payload, err := decodeInvokePayload(req.Payload)
		if err != nil {
			return failure(req.RequestID, protocol.CodeBadRequest, err.Error(), nil), nil
		}
		resp := executeInvokePayload(payload)
		resp.RequestID = req.RequestID
		return resp, nil
	case protocol.OpPing:
		payload, err := decodePingPayload(req.Payload)
		if err != nil {
			return failure(req.RequestID, protocol.CodeBadRequest, err.Error(), nil), nil
		}
		probe := app.New(nil).ProbeEndpoint(context.Background(), app.ProbeInput{
			Address:   payload.Address,
			Service:   payload.Service,
			TimeoutMS: payload.RPCTimeoutMS,
		})
		resp := protocolResponseFromProbe(probe)
		resp.RequestID = req.RequestID
		return &resp, nil
	default:
		return failure(req.RequestID, protocol.CodeBadRequest, "unsupported op: "+req.Op, nil), nil
	}
}

func executeInvokePayload(payload protocol.InvokePayload) *protocol.Response {
	service := app.New(nil)
	plan, err := service.PlanInvocation(context.Background(), app.InvocationInput{
		Address:             payload.Address,
		Service:             payload.Service,
		Method:              payload.Method,
		ParamTypes:          payload.ArgTypes,
		OrderedArguments:    payload.Args,
		HasOrderedArguments: true,
		TimeoutMS:           payload.RPCTimeoutMS,
		RawResult:           payload.RawResult,
	})
	if err != nil {
		return failure("", protocol.CodeBadRequest, err.Error(), app.DomainErrorDetails(err))
	}
	exec := service.ExecuteInvocation(context.Background(), plan)
	if exec.OK {
		applyAssertions(&exec, payload.Assertions)
	}
	return protocolResponseFromExecution(exec)
}

func applyAssertions(exec *app.InvocationExecution, specs []protocol.AssertionSpec) {
	if exec == nil || len(specs) == 0 {
		return
	}
	assertions := make([]direct.Assertion, len(specs))
	for i, item := range specs {
		assertions[i] = direct.Assertion{Path: item.Path, Equals: item.Equals, Exists: item.Exists}
	}
	out, failed := direct.EvaluateAssertions(exec.Data["result"], assertions)
	exec.Data["assertions"] = out
	if failed > 0 {
		exec.OK = false
		exec.Code = protocol.CodeAssertionFailed
		exec.Error = &app.ExecutionError{Message: fmt.Sprintf("%d of %d assertions failed", failed, len(out))}
	}
}

func decodeInvokePayload(raw json.RawMessage) (protocol.InvokePayload, error) {
	var payload protocol.InvokePayload
	if len(raw) == 0 {
		return payload, fmt.Errorf("payload must be an object")
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.UseNumber()
	if err := dec.Decode(&payload); err != nil {
		return payload, err
	}
	if payload.Address == "" || payload.Service == "" || payload.Method == "" {
		return payload, fmt.Errorf("address, service and method are required")
	}
	if len(payload.ArgTypes) != len(payload.Args) {
		return payload, fmt.Errorf("argTypes length (%d) does not match args length (%d)", len(payload.ArgTypes), len(payload.Args))
	}
	return payload, nil
}

func decodePingPayload(raw json.RawMessage) (protocol.PingPayload, error) {
	var payload protocol.PingPayload
	if len(raw) == 0 {
		return payload, fmt.Errorf("payload must be an object")
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.UseNumber()
	if err := dec.Decode(&payload); err != nil {
		return payload, err
	}
	if strings.TrimSpace(payload.Address) == "" {
		return payload, fmt.Errorf("address is required")
	}
	return payload, nil
}

func protocolResponseFromExecution(exec app.InvocationExecution) *protocol.Response {
	resp := &protocol.Response{
		OK:   exec.OK,
		Code: exec.Code,
		Meta: exec.Meta,
	}
	if exec.OK {
		body, err := json.Marshal(exec.Data)
		if err != nil {
			return failure("", protocol.CodeInternalError, err.Error(), nil)
		}
		resp.Data = body
		return resp
	}
	if exec.Error != nil {
		resp.Error = &protocol.ResponseError{
			Message: exec.Error.Message,
			Cause:   exec.Error.Cause,
			Details: exec.Error.Details,
		}
	}
	return resp
}

func protocolResponseFromProbe(probe app.ProbeResult) protocol.Response {
	data := map[string]interface{}{
		"address":     probe.Address,
		"service":     probe.Service,
		"reachable":   probe.Reachable,
		"elapsedMs":   probe.ElapsedMS,
		"diagnostics": probe.Diagnostics,
	}
	if probe.Error != nil {
		return protocol.Response{
			OK:   false,
			Code: app.CodeConnectFailed,
			Error: &protocol.ResponseError{
				Message: probe.Error.Message,
				Cause:   probe.Error.Cause,
				Details: probe.Error.Details,
			},
			Meta: probe.Meta,
		}
	}
	body, err := json.Marshal(data)
	if err != nil {
		return *failure("", protocol.CodeInternalError, err.Error(), nil)
	}
	return protocol.Response{OK: true, Code: app.CodeSuccess, Data: body, Meta: probe.Meta}
}

func failure(requestID, code, message string, details map[string]interface{}) *protocol.Response {
	return &protocol.Response{
		RequestID: requestID,
		OK:        false,
		Code:      code,
		Error: &protocol.ResponseError{
			Message: message,
			Details: details,
		},
		Meta: map[string]interface{}{"runtime": "go"},
	}
}
