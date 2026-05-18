package invoker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/sofarpc/cli/internal/appconfig"
	"github.com/sofarpc/cli/internal/direct"
	"github.com/sofarpc/cli/internal/protocol"
)

func DirectRequest(req protocol.Request) (*protocol.Response, error) {
	var resp protocol.Response
	switch req.Op {
	case protocol.OpInvoke:
		payload, err := DecodeInvokePayload(req.Payload)
		if err != nil {
			return Failure(req.RequestID, protocol.CodeBadRequest, err.Error(), nil), nil
		}
		resp = DirectPayload(payload)
	case protocol.OpPing:
		payload, err := DecodePingPayload(req.Payload)
		if err != nil {
			return Failure(req.RequestID, protocol.CodeBadRequest, err.Error(), nil), nil
		}
		resp = PingPayload(payload)
	default:
		return Failure(req.RequestID, protocol.CodeBadRequest, "unsupported op: "+req.Op, nil), nil
	}
	resp.RequestID = req.RequestID
	return &resp, nil
}

func DirectPayload(payload protocol.InvokePayload) protocol.Response {
	timeout := time.Duration(payload.RPCTimeoutMS) * time.Millisecond
	out, err := direct.Invoke(context.Background(), direct.Request{
		Address:  payload.Address,
		Service:  payload.Service,
		Method:   payload.Method,
		ArgTypes: payload.ArgTypes,
		Args:     payload.Args,
		Timeout:  timeout,
	})
	if err != nil {
		return *Failure("", ErrorCode(err), err.Error(), map[string]interface{}{
			"address":      payload.Address,
			"service":      payload.Service,
			"method":       payload.Method,
			"rpcTimeoutMs": payload.RPCTimeoutMS,
		})
	}
	data := map[string]interface{}{
		"result":    out.Result,
		"elapsedMs": out.Elapsed.Milliseconds(),
	}
	assertions, failed := direct.EvaluateAssertions(out.Result, Assertions(payload.Assertions))
	if len(assertions) > 0 {
		data["assertions"] = assertions
	}
	body, err := json.Marshal(data)
	if err != nil {
		return *Failure("", protocol.CodeInternalError, err.Error(), nil)
	}
	resp := protocol.Response{
		OK:   failed == 0,
		Code: protocol.CodeSuccess,
		Data: body,
		Meta: map[string]interface{}{"runtime": "go", "transport": "direct-bolt"},
	}
	if failed > 0 {
		resp.Code = protocol.CodeAssertionFailed
		resp.Error = &protocol.ResponseError{Message: fmt.Sprintf("%d of %d assertions failed", failed, len(assertions))}
	}
	return resp
}

func PingPayload(payload protocol.PingPayload) protocol.Response {
	timeout := time.Duration(payload.RPCTimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = time.Duration(appconfig.DefaultServerTimeoutMS) * time.Millisecond
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	start := time.Now()
	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "tcp", payload.Address)
	elapsed := time.Since(start)
	if err != nil {
		return *Failure("", ErrorCode(err), err.Error(), map[string]interface{}{
			"address":      payload.Address,
			"service":      payload.Service,
			"rpcTimeoutMs": payload.RPCTimeoutMS,
		})
	}
	_ = conn.Close()

	body, err := json.Marshal(map[string]interface{}{
		"address":   payload.Address,
		"service":   payload.Service,
		"reachable": true,
		"elapsedMs": elapsed.Milliseconds(),
	})
	if err != nil {
		return *Failure("", protocol.CodeInternalError, err.Error(), nil)
	}
	return protocol.Response{
		OK:   true,
		Code: protocol.CodeSuccess,
		Data: body,
		Meta: map[string]interface{}{"runtime": "go", "transport": "tcp-dial"},
	}
}

func DecodeInvokePayload(raw json.RawMessage) (protocol.InvokePayload, error) {
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
	if payload.RPCTimeoutMS <= 0 {
		payload.RPCTimeoutMS = appconfig.DefaultServerTimeoutMS
	}
	return payload, nil
}

func DecodePingPayload(raw json.RawMessage) (protocol.PingPayload, error) {
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
	if payload.RPCTimeoutMS <= 0 {
		payload.RPCTimeoutMS = appconfig.DefaultServerTimeoutMS
	}
	return payload, nil
}

func Assertions(input []protocol.AssertionSpec) []direct.Assertion {
	out := make([]direct.Assertion, len(input))
	for i, item := range input {
		out[i] = direct.Assertion{Path: item.Path, Equals: item.Equals, Exists: item.Exists}
	}
	return out
}

func ErrorCode(err error) string {
	var remote *direct.RemoteError
	if errors.As(err, &remote) {
		return protocol.CodeInvokeFailed
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return protocol.CodeRPCTimeout
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "deadline exceeded") || strings.Contains(msg, "timeout") {
		return protocol.CodeRPCTimeout
	}
	if strings.Contains(msg, "dial") || strings.Contains(msg, "connection refused") || strings.Contains(msg, "no such host") {
		return protocol.CodeConnectFailed
	}
	return protocol.CodeInvokeFailed
}

func Failure(requestID, code, message string, details map[string]interface{}) *protocol.Response {
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
