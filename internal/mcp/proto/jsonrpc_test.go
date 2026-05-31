package proto

import (
	"encoding/json"
	"testing"
)

func TestDecodeRejectsBadFrames(t *testing.T) {
	cases := []struct {
		name string
		line string
		code int
	}{
		{"wrong-jsonrpc", `{"jsonrpc":"1.0","id":1,"method":"x"}`, CodeInvalidRequest},
		{"empty-method", `{"jsonrpc":"2.0","id":1,"method":""}`, CodeInvalidRequest},
		{"missing-method", `{"jsonrpc":"2.0","id":1}`, CodeInvalidRequest},
		{"trailing-data", `{"jsonrpc":"2.0","id":1,"method":"x"} {}`, CodeParseError},
		{"not-json", `not json`, CodeParseError},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := Decode([]byte(c.line))
			if err == nil || err.Code != c.code {
				t.Fatalf("Decode(%s) err = %v, want code %d", c.line, err, c.code)
			}
		})
	}
}

func TestDecodeAcceptsValidRequest(t *testing.T) {
	req, err := Decode([]byte(`{"jsonrpc":"2.0","id":7,"method":"tools/list","params":{}}`))
	if err != nil {
		t.Fatalf("Decode err = %v", err)
	}
	if req.Method != "tools/list" || req.IsNotification() {
		t.Fatalf("unexpected req: %+v", req)
	}
}

func TestDecodeParamsPreservesLargeNumbers(t *testing.T) {
	var payload struct {
		Arguments map[string]interface{} `json:"arguments"`
	}
	if err := DecodeParams([]byte(`{"arguments":{"mpCode":433905635109773312}}`), &payload); err != nil {
		t.Fatalf("DecodeParams: %v", err)
	}
	n, ok := payload.Arguments["mpCode"].(json.Number)
	if !ok {
		t.Fatalf("mpCode type = %T, want json.Number", payload.Arguments["mpCode"])
	}
	if n.String() != "433905635109773312" {
		t.Fatalf("mpCode = %s", n.String())
	}
}
