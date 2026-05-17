package direct

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	"testing"
	"time"
)

func TestBuildRequestContentWrapsTopLevelDTO(t *testing.T) {
	content, target, err := buildRequestContent(Request{
		Service:  "com.example.Facade",
		Method:   "query",
		ArgTypes: []string{"com.example.QueryRequest"},
		Args: []interface{}{
			map[string]interface{}{"mpCode": json.Number("433905635109773312")},
		},
	})
	if err != nil {
		t.Fatalf("buildRequestContent: %v", err)
	}
	if target != "com.example.Facade:1.0" {
		t.Fatalf("target = %q", target)
	}
	r := &reader{data: content}
	if _, err := r.readValue(); err != nil {
		t.Fatalf("read SofaRequest: %v", err)
	}
	arg, err := r.readValue()
	if err != nil {
		t.Fatalf("read arg: %v", err)
	}
	obj := arg.(map[string]interface{})
	if obj["type"] != "com.example.QueryRequest" {
		t.Fatalf("arg type = %#v", obj["type"])
	}
	fields := obj["fields"].(map[string]interface{})
	if fields["mpCode"] != int64(433905635109773312) {
		t.Fatalf("mpCode = %#v", fields["mpCode"])
	}
}

func TestInvokeRoundTripFlattensResponse(t *testing.T) {
	responseContent := successResponse(t, typedObject{
		name: "com.example.OperationResult",
		fields: map[string]interface{}{
			"success": true,
			"code":    int32(0),
			"data": typedObject{
				name: "com.example.Payload",
				fields: map[string]interface{}{
					"mpCode":      int64(433905635109773312),
					"totalAssets": typedObject{name: "java.math.BigDecimal", fields: map[string]interface{}{"value": "113795.2485"}},
				},
			},
		},
	})
	addr, stop := fakeBoltServer(t, responseContent)
	defer stop()

	out, err := Invoke(context.Background(), Request{
		Address:  addr,
		Service:  "com.example.Facade",
		Method:   "query",
		ArgTypes: []string{"com.example.QueryRequest"},
		Args:     []interface{}{map[string]interface{}{"mpCode": int64(433905635109773312)}},
		Timeout:  2 * time.Second,
	})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	result := out.Result.(map[string]interface{})
	if result["success"] != true || result["code"] != int64(0) {
		t.Fatalf("bad result: %#v", result)
	}
	data := result["data"].(map[string]interface{})
	if data["mpCode"] != int64(433905635109773312) {
		t.Fatalf("bad data: %#v", data)
	}
	amount, ok := data["totalAssets"].(json.Number)
	if !ok || amount.String() != "113795.2485" {
		t.Fatalf("totalAssets = %#v", data["totalAssets"])
	}
	body, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("marshal data: %v", err)
	}
	if string(body) != `{"mpCode":433905635109773312,"totalAssets":113795.2485}` {
		t.Fatalf("bad JSON: %s", body)
	}
}

func TestEvaluateAssertions(t *testing.T) {
	result := map[string]interface{}{"status": "INACTIVE", "name": "alice"}
	exists := true
	out, failed := EvaluateAssertions(result, []Assertion{
		{Path: "$.status", Equals: "ACTIVE"},
		{Path: "$.name", Exists: &exists},
	})
	if failed != 1 || len(out) != 2 || out[0].Passed || !out[1].Passed {
		t.Fatalf("unexpected assertions: failed=%d out=%+v", failed, out)
	}
}

func successResponse(t *testing.T, app interface{}) []byte {
	t.Helper()
	w := newWriter()
	if err := w.writeObject(responseClass,
		[]string{"isError", "errorMsg", "appResponse", "responseProps"},
		[]interface{}{false, nil, app, nil}); err != nil {
		t.Fatalf("write response: %v", err)
	}
	return w.bytes()
}

func fakeBoltServer(t *testing.T, response []byte) (string, func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		id, err := readRequestID(conn)
		if err != nil {
			return
		}
		_ = writeTestResponse(conn, id, response)
	}()
	return ln.Addr().String(), func() {
		_ = ln.Close()
		<-done
	}
}

func readRequestID(r io.Reader) (uint32, error) {
	fixed := make([]byte, requestHeaderLen)
	if _, err := io.ReadFull(r, fixed); err != nil {
		return 0, err
	}
	classLen := int(binary.BigEndian.Uint16(fixed[14:16]))
	headerLen := int(binary.BigEndian.Uint16(fixed[16:18]))
	contentLen := int(binary.BigEndian.Uint32(fixed[18:22]))
	if _, err := io.CopyN(io.Discard, r, int64(classLen+headerLen+contentLen)); err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint32(fixed[5:9]), nil
}

func writeTestResponse(w io.Writer, id uint32, content []byte) error {
	classBytes := []byte(responseClass)
	fixed := make([]byte, responseHeaderLen)
	fixed[0] = protocolCodeV1
	fixed[1] = responseType
	binary.BigEndian.PutUint16(fixed[2:4], cmdRPCResponse)
	fixed[4] = cmdVersion
	binary.BigEndian.PutUint32(fixed[5:9], id)
	fixed[9] = codecHessian2
	binary.BigEndian.PutUint16(fixed[12:14], uint16(len(classBytes)))
	binary.BigEndian.PutUint32(fixed[16:20], uint32(len(content)))
	if _, err := w.Write(fixed); err != nil {
		return err
	}
	if _, err := w.Write(classBytes); err != nil {
		return err
	}
	_, err := w.Write(content)
	return err
}
