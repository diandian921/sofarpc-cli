package direct

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/diandian921/sofarpc-cli/internal/javavalue"
)

func TestBuildRequestContentWrapsTopLevelDTO(t *testing.T) {
	content, target, err := buildRequestContent(Request{
		Service:  "com.example.Facade",
		Method:   "query",
		ArgTypes: []string{"com.example.QueryRequest"},
		Args: []interface{}{
			javavalue.Object("com.example.QueryRequest", map[string]javavalue.TypedValue{
				"mpCode": javavalue.Scalar("java.lang.Long", json.Number("433905635109773312")),
			}),
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

func TestBuildRequestContentIncludesAttachmentsInRequestProps(t *testing.T) {
	content, _, err := buildRequestContent(Request{
		Service:  "com.example.Facade",
		Method:   "query",
		ArgTypes: []string{"java.lang.String"},
		Args:     []interface{}{javavalue.Scalar("java.lang.String", "u001")},
		Attachments: map[string]string{
			"tenant":         "blue",
			"trace-context":  "abc",
			"generic.revise": "false",
		},
	})
	if err != nil {
		t.Fatalf("buildRequestContent: %v", err)
	}
	req := readSofaRequest(t, content)
	props := req["requestProps"].(map[string]interface{})
	if props["tenant"] != "blue" || props["trace-context"] != "abc" {
		t.Fatalf("attachments missing from requestProps: %#v", props)
	}
	if props["generic.revise"] != "true" || props["sofa_head_generic_type"] != genericType || props["type"] != invokeTypeSync {
		t.Fatalf("runtime requestProps were not preserved: %#v", props)
	}
}

func TestDeclaredNumericTypesChooseHessianTags(t *testing.T) {
	cases := []struct {
		name     string
		javaType string
		value    interface{}
		wantTag  byte
	}{
		{name: "integer", javaType: "java.lang.Integer", value: json.Number("5"), wantTag: 'I'},
		{name: "long", javaType: "java.lang.Long", value: json.Number("5"), wantTag: 'L'},
		{name: "double", javaType: "java.lang.Double", value: json.Number("2.0"), wantTag: 'D'},
		{name: "primitive double", javaType: "double", value: float64(2), wantTag: 'D'},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := newWriter()
			if err := w.writeValueWithType(tc.javaType, tc.value); err != nil {
				t.Fatalf("writeValueWithType: %v", err)
			}
			got := w.bytes()[0]
			if got != tc.wantTag {
				t.Fatalf("tag = %q, want %q; bytes=%x", got, tc.wantTag, w.bytes())
			}
		})
	}
}

func readSofaRequest(t *testing.T, content []byte) map[string]interface{} {
	t.Helper()
	r := &reader{data: content}
	root, err := r.readValue()
	if err != nil {
		t.Fatalf("read SofaRequest: %v", err)
	}
	obj := root.(map[string]interface{})
	if obj["type"] != requestClass {
		t.Fatalf("request type = %#v", obj["type"])
	}
	return obj["fields"].(map[string]interface{})
}

func TestTypedValueDTOFieldTypesDriveNumericEncoding(t *testing.T) {
	content, _, err := buildRequestContent(Request{
		Service:  "com.example.Facade",
		Method:   "query",
		ArgTypes: []string{"com.example.QueryRequest"},
		Args: []interface{}{
			javavalue.Object("com.example.QueryRequest", map[string]javavalue.TypedValue{
				"ratio": javavalue.Scalar("java.lang.Double", json.Number("2.0")),
			}),
		},
	})
	if err != nil {
		t.Fatalf("buildRequestContent: %v", err)
	}
	r := &reader{data: content}
	if _, err := r.readValue(); err != nil {
		t.Fatalf("read SofaRequest: %v", err)
	}
	arg, err := r.readValue()
	if err != nil {
		t.Fatalf("read arg: %v", err)
	}
	fields := arg.(map[string]interface{})["fields"].(map[string]interface{})
	if got, ok := fields["ratio"].(float64); !ok || got != 2 {
		t.Fatalf("ratio = %#v, want float64(2)", fields["ratio"])
	}
	if _, exists := fields["__fieldTypes"]; exists {
		t.Fatalf("__fieldTypes leaked into hessian fields: %#v", fields)
	}
}

func TestDeclaredBigDecimalEncodesTypedValue(t *testing.T) {
	w := newWriter()
	if err := w.writeValueWithType("java.math.BigDecimal", "1000.50"); err != nil {
		t.Fatalf("writeValueWithType: %v", err)
	}
	r := &reader{data: w.bytes()}
	got, err := r.readValue()
	if err != nil {
		t.Fatalf("readValue: %v", err)
	}
	obj := got.(map[string]interface{})
	if obj["type"] != "java.math.BigDecimal" {
		t.Fatalf("type = %#v", obj["type"])
	}
	fields := obj["fields"].(map[string]interface{})
	if fields["value"] != "1000.50" {
		t.Fatalf("value = %#v", fields["value"])
	}
}

func TestListPreservesNullElements(t *testing.T) {
	w := newWriter()
	if err := w.writeValue(javavalue.List("java.util.ArrayList", []javavalue.TypedValue{
		javavalue.Scalar("", nil),
		javavalue.Scalar("", json.Number("1")),
	})); err != nil {
		t.Fatalf("writeValue: %v", err)
	}
	r := &reader{data: w.bytes()}
	got, err := r.readValue()
	if err != nil {
		t.Fatalf("readValue: %v", err)
	}
	items := got.(map[string]interface{})["items"].([]interface{})
	if len(items) != 2 || items[0] != nil || items[1] != int64(1) {
		t.Fatalf("items = %#v", items)
	}
}

func TestLongBoundaryEncoding(t *testing.T) {
	w := newWriter()
	if err := w.writeValueWithType("java.lang.Long", json.Number("9223372036854775807")); err != nil {
		t.Fatalf("writeValueWithType: %v", err)
	}
	r := &reader{data: w.bytes()}
	got, err := r.readValue()
	if err != nil {
		t.Fatalf("readValue: %v", err)
	}
	if got != int64(9223372036854775807) {
		t.Fatalf("long = %#v", got)
	}
}

func TestHessianStringLengthsUseUTF16Units(t *testing.T) {
	w := newWriter()
	if err := w.writeString("a🙂b"); err != nil {
		t.Fatalf("writeString: %v", err)
	}
	if got := w.bytes()[0]; got != 4 {
		t.Fatalf("short string length tag = %d, want 4 UTF-16 units", got)
	}
	r := &reader{data: w.bytes()}
	got, err := r.readValue()
	if err != nil {
		t.Fatalf("readValue: %v", err)
	}
	if got != "a🙂b" {
		t.Fatalf("string = %q", got)
	}
}

func TestBuildRequestContentRejectsDeepTypedArguments(t *testing.T) {
	arg := javavalue.Scalar("java.lang.String", "leaf")
	for i := 0; i < maxHessianDepth+16; i++ {
		arg = javavalue.List("java.util.ArrayList", []javavalue.TypedValue{arg})
	}
	_, _, err := buildRequestContent(Request{
		Service:  "com.example.Facade",
		Method:   "query",
		ArgTypes: []string{"java.util.List"},
		Args:     []interface{}{arg},
	})
	if err == nil || !strings.Contains(err.Error(), "nesting too deep") {
		t.Fatalf("err = %v, want nesting error", err)
	}
}

func TestInvokeRoundTripReturnsDecodedAppResponse(t *testing.T) {
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
		Args: []interface{}{javavalue.Object("com.example.QueryRequest", map[string]javavalue.TypedValue{
			"mpCode": javavalue.Scalar("java.lang.Long", int64(433905635109773312)),
		})},
		Timeout: 2 * time.Second,
	})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	appResponse := out.AppResponse.(map[string]interface{})
	if appResponse["type"] != "com.example.OperationResult" {
		t.Fatalf("app response = %#v", appResponse)
	}
	fields := appResponse["fields"].(map[string]interface{})
	if fields["success"] != true || fields["code"] != int64(0) {
		t.Fatalf("bad fields: %#v", fields)
	}
	data := fields["data"].(map[string]interface{})["fields"].(map[string]interface{})
	if data["mpCode"] != int64(433905635109773312) {
		t.Fatalf("bad data: %#v", data)
	}
	amount := data["totalAssets"].(map[string]interface{})["fields"].(map[string]interface{})
	if amount["value"] != "113795.2485" {
		t.Fatalf("totalAssets = %#v", data["totalAssets"])
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
