//go:build bolt_oracle

package direct

import (
	"bytes"
	"reflect"
	"testing"
	"time"

	"github.com/sofastack/sofa-bolt-go/sofabolt"
)

func TestBoltOracleDecodesRequestFrame(t *testing.T) {
	content := []byte{0x48, 0x01, 0x02, 0x03}
	targetService := targetServiceName("com.example.Facade", "1.0", "")
	headers := requestHeader("query", targetService, "agent-app")
	frame, err := encodeBoltRequest(42, 1500*time.Millisecond, headers, content)
	if err != nil {
		t.Fatalf("encodeBoltRequest: %v", err)
	}

	req := sofabolt.AcquireRequest()
	defer sofabolt.ReleaseRequest(req)
	n, err := req.Read(sofabolt.NewReadOption(), bytes.NewReader(frame))
	if err != nil {
		t.Fatalf("sofa-bolt-go read request: %v", err)
	}
	if n != len(frame) {
		t.Fatalf("read bytes = %d, want %d", n, len(frame))
	}
	if req.GetProto() != sofabolt.ProtoBOLTV1 {
		t.Fatalf("proto = %v, want BOLTV1", req.GetProto())
	}
	if req.GetType() != sofabolt.TypeBOLTRequest {
		t.Fatalf("type = %v, want request", req.GetType())
	}
	if req.GetCMDCode() != sofabolt.CMDCodeBOLTRequest {
		t.Fatalf("cmd = %v, want request", req.GetCMDCode())
	}
	if req.GetRequestID() != 42 {
		t.Fatalf("request id = %d, want 42", req.GetRequestID())
	}
	if req.GetCodec() != sofabolt.CodecHessian2 {
		t.Fatalf("codec = %v, want hessian2", req.GetCodec())
	}
	if req.GetTimeout() != 1500 {
		t.Fatalf("timeout = %d, want 1500", req.GetTimeout())
	}
	if got := string(req.GetClass()); got != requestClass {
		t.Fatalf("class = %q, want %q", got, requestClass)
	}
	if !bytes.Equal(req.GetContent(), content) {
		t.Fatalf("content = %x, want %x", req.GetContent(), content)
	}
	for k, want := range headers {
		if got := req.GetHeaders().Get(k); got != want {
			t.Fatalf("header %q = %q, want %q", k, got, want)
		}
	}
}

func TestBoltOracleGeneratedResponseReadsInDirect(t *testing.T) {
	content := []byte{0x48, 0x04, 0x05, 0x06}
	headers := map[string]string{
		"service":        "com.example.Facade:1.0",
		"trace-id":       "trace-123",
		"generic.revise": "true",
	}

	res := sofabolt.AcquireResponse()
	defer sofabolt.ReleaseResponse(res)
	res.SetRequestID(42).
		SetVer2(cmdVersion).
		SetStatus(sofabolt.StatusSuccess).
		SetClassString(responseClass).
		SetContent(content)
	for k, v := range headers {
		res.GetHeaders().Set(k, v)
	}
	frame, err := res.Write(sofabolt.NewWriteOption(), nil)
	if err != nil {
		t.Fatalf("sofa-bolt-go write response: %v", err)
	}

	got, err := readBoltResponse(bytes.NewReader(frame))
	if err != nil {
		t.Fatalf("readBoltResponse: %v", err)
	}
	if got.RequestID != 42 {
		t.Fatalf("request id = %d, want 42", got.RequestID)
	}
	if got.Status != uint16(sofabolt.StatusSuccess) {
		t.Fatalf("status = %d, want %d", got.Status, sofabolt.StatusSuccess)
	}
	if got.Codec != codecHessian2 {
		t.Fatalf("codec = %d, want %d", got.Codec, codecHessian2)
	}
	if got.Class != responseClass {
		t.Fatalf("class = %q, want %q", got.Class, responseClass)
	}
	if !bytes.Equal(got.Content, content) {
		t.Fatalf("content = %x, want %x", got.Content, content)
	}
	if !reflect.DeepEqual(got.Headers, headers) {
		t.Fatalf("headers = %#v, want %#v", got.Headers, headers)
	}
}

func TestBoltOracleSimpleMapParity(t *testing.T) {
	headers := map[string]string{
		"service":        "com.example.Facade:1.0",
		"host":           "127.0.0.1",
		"generic.revise": "true",
		"custom":         "value with spaces",
	}

	var oracle sofabolt.SimpleMap
	if err := oracle.Decode(encodeSimpleMap(headers)); err != nil {
		t.Fatalf("sofa-bolt-go decode simplemap: %v", err)
	}
	for k, want := range headers {
		if got := oracle.Get(k); got != want {
			t.Fatalf("oracle header %q = %q, want %q", k, got, want)
		}
	}

	var fromOracle sofabolt.SimpleMap
	for k, v := range headers {
		fromOracle.Set(k, v)
	}
	buf := make([]byte, fromOracle.GetEncodeSize())
	n := fromOracle.Encode(buf)
	got := decodeSimpleMap(buf[:n])
	if !reflect.DeepEqual(got, headers) {
		t.Fatalf("decoded headers = %#v, want %#v", got, headers)
	}
}
