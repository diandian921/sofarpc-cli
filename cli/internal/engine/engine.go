// Package engine implements the Go side of the MCP-first Engine JSON-RPC
// protocol over the existing length-prefixed loopback transport.
package engine

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"sync/atomic"
	"time"

	"github.com/sofarpc/cli/internal/ipc"
)

type Client struct {
	Addr           string
	DialTimeout    time.Duration
	RequestTimeout time.Duration
	Token          string
}

type RPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *RPCError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("engine json-rpc error %d: %s", e.Code, e.Message)
}

type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      string          `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

var nextID uint64

func (c *Client) CallAuthenticated(method string, params interface{}, result interface{}) error {
	conn, err := net.DialTimeout("tcp", c.Addr, c.DialTimeout)
	if err != nil {
		return fmt.Errorf("dial %s: %w", c.Addr, err)
	}
	defer conn.Close()
	if c.RequestTimeout > 0 {
		_ = conn.SetDeadline(time.Now().Add(c.RequestTimeout))
	}
	if err := c.callOnConn(conn, "engine.hello", map[string]interface{}{"token": c.Token}, nil); err != nil {
		return err
	}
	return c.callOnConn(conn, method, params, result)
}

func (c *Client) callOnConn(conn net.Conn, method string, params interface{}, result interface{}) error {
	id := fmt.Sprintf("go-%d", atomic.AddUint64(&nextID, 1))
	req := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
	}
	if params != nil {
		req["params"] = params
	}
	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal json-rpc request: %w", err)
	}
	if err := ipc.WriteFrame(conn, body); err != nil {
		return err
	}
	respBytes, err := ipc.ReadFrame(conn)
	if err != nil {
		return fmt.Errorf("read json-rpc response: %w", err)
	}
	var resp Response
	dec := json.NewDecoder(bytes.NewReader(respBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&resp); err != nil {
		return fmt.Errorf("decode json-rpc response: %w", err)
	}
	if resp.Error != nil {
		return resp.Error
	}
	if result != nil && len(resp.Result) > 0 {
		if err := json.Unmarshal(resp.Result, result); err != nil {
			return fmt.Errorf("decode json-rpc result: %w", err)
		}
	}
	return nil
}
