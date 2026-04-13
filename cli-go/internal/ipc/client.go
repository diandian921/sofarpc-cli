package ipc

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"time"

	"github.com/sofarpc/cli-go/internal/protocol"
)

// Client is a thin one-shot transport: dial, send one request, read one response, close.
//
// V1 deliberately avoids connection pooling because per-call latency is dominated by the
// server-side warm path, not by socket reuse. This keeps the client trivially correct.
type Client struct {
	Addr           string
	DialTimeout    time.Duration
	RequestTimeout time.Duration
}

// Call sends req on a fresh connection and decodes the response.
func (c *Client) Call(req protocol.Request) (*protocol.Response, error) {
	conn, err := net.DialTimeout("tcp", c.Addr, c.DialTimeout)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", c.Addr, err)
	}
	defer conn.Close()

	if c.RequestTimeout > 0 {
		_ = conn.SetDeadline(time.Now().Add(c.RequestTimeout))
	}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}
	if err := WriteFrame(conn, body); err != nil {
		return nil, err
	}
	respBytes, err := ReadFrame(conn)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	var resp protocol.Response
	dec := json.NewDecoder(bytes.NewReader(respBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&resp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}
	return &resp, nil
}
