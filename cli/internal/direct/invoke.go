package direct

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"strings"
	"sync/atomic"
	"time"
)

const (
	requestClass  = "com.alipay.sofa.rpc.core.request.SofaRequest"
	responseClass = "com.alipay.sofa.rpc.core.response.SofaResponse"

	defaultVersion = "1.0"
	genericType    = "2"
	invokeTypeSync = "sync"

	protocolCodeV1 byte   = 1
	requestType    byte   = 1
	responseType   byte   = 0
	cmdRPCRequest  uint16 = 1
	cmdRPCResponse uint16 = 2
	cmdVersion     byte   = 1
	codecHessian2  byte   = 1

	requestHeaderLen  = 22
	responseHeaderLen = 20
	maxResponseBytes  = 16 << 20
)

var requestID atomic.Uint32

func init() {
	requestID.Store(uint32(time.Now().UnixNano()))
}

// Request is the pure-Go direct invoke surface.
type Request struct {
	Address  string
	Service  string
	Method   string
	ArgTypes []string
	Args     []interface{}
	Timeout  time.Duration
	Version  string
	UniqueID string
	AppName  string
}

type Outcome struct {
	Result      interface{}
	RawResult   interface{}
	Elapsed     time.Duration
	Diagnostics map[string]interface{}
}

type RemoteError struct {
	Message string
}

func (e *RemoteError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

type ConnectError struct {
	Address string
	Err     error
}

func (e *ConnectError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("dial %s: %v", e.Address, e.Err)
}

func (e *ConnectError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// Invoke sends one SOFARPC generic invocation over direct BOLT + Hessian2.
func Invoke(ctx context.Context, req Request) (Outcome, error) {
	if err := validateRequest(req); err != nil {
		return Outcome{}, err
	}
	if req.Timeout <= 0 {
		req.Timeout = 5 * time.Second
	}
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, req.Timeout)
		defer cancel()
	}

	id := nextRequestID()
	content, targetService, err := buildRequestContent(req)
	if err != nil {
		return Outcome{}, err
	}
	frame, err := encodeBoltRequest(id, req.Timeout, requestHeader(req.Method, targetService, req.AppName), content)
	if err != nil {
		return Outcome{}, err
	}

	addr := normalizeAddress(req.Address)
	start := time.Now()
	resp, err := roundTrip(ctx, addr, frame)
	elapsed := time.Since(start)
	if err != nil {
		return Outcome{Elapsed: elapsed}, err
	}
	decoded, err := decodeSofaResponse(resp.Content)
	if err != nil {
		return Outcome{Elapsed: elapsed, Diagnostics: responseDiagnostics(resp, targetService, id)}, err
	}
	if decoded.IsError || strings.TrimSpace(decoded.ErrorMsg) != "" {
		msg := strings.TrimSpace(decoded.ErrorMsg)
		if msg == "" {
			msg = "remote response flagged isError=true"
		}
		return Outcome{Elapsed: elapsed, Diagnostics: responseDiagnostics(resp, targetService, id)}, &RemoteError{Message: msg}
	}
	return Outcome{
		Result:      flattenValue(decoded.AppResponse),
		RawResult:   decoded.AppResponse,
		Elapsed:     elapsed,
		Diagnostics: responseDiagnostics(resp, targetService, id),
	}, nil
}

func validateRequest(req Request) error {
	if strings.TrimSpace(req.Address) == "" {
		return fmt.Errorf("address is required")
	}
	if strings.TrimSpace(req.Service) == "" {
		return fmt.Errorf("service is required")
	}
	if strings.TrimSpace(req.Method) == "" {
		return fmt.Errorf("method is required")
	}
	if len(req.ArgTypes) != len(req.Args) {
		return fmt.Errorf("argTypes length (%d) does not match args length (%d)", len(req.ArgTypes), len(req.Args))
	}
	return nil
}

func nextRequestID() uint32 {
	for {
		id := requestID.Add(1)
		if id != 0 {
			return id
		}
	}
}

func buildRequestContent(req Request) ([]byte, string, error) {
	targetService := targetServiceName(req.Service, req.Version, req.UniqueID)
	props := map[string]interface{}{
		"sofa_head_generic_type": genericType,
		"type":                   invokeTypeSync,
		"generic.revise":         "true",
	}
	args, err := normalizeArgs(req.ArgTypes, req.Args)
	if err != nil {
		return nil, "", err
	}

	w := newWriter()
	if err := w.writeObject(requestClass,
		[]string{"targetAppName", "methodName", "targetServiceUniqueName", "requestProps", "methodArgSigs"},
		[]interface{}{nil, req.Method, targetService, props, append([]string(nil), req.ArgTypes...)}); err != nil {
		return nil, "", err
	}
	for i, arg := range args {
		argType := ""
		if i < len(req.ArgTypes) {
			argType = req.ArgTypes[i]
		}
		if err := w.writeValueWithType(argType, arg); err != nil {
			return nil, "", fmt.Errorf("encode arg %d: %w", i, err)
		}
	}
	return w.bytes(), targetService, nil
}

func targetServiceName(service, version, uniqueID string) string {
	version = strings.TrimSpace(version)
	if version == "" {
		version = defaultVersion
	}
	out := strings.TrimSpace(service) + ":" + version
	if strings.TrimSpace(uniqueID) != "" {
		out += ":" + strings.TrimSpace(uniqueID)
	}
	return out
}

func requestHeader(method, targetService, appName string) map[string]string {
	h := map[string]string{
		"service":                  targetService,
		"sofa_head_method_name":    method,
		"sofa_head_target_service": targetService,
		"sofa_head_generic_type":   genericType,
		"type":                     invokeTypeSync,
		"generic.revise":           "true",
	}
	if strings.TrimSpace(appName) != "" {
		h["sofa_head_target_app"] = strings.TrimSpace(appName)
	}
	return h
}

func encodeBoltRequest(id uint32, timeout time.Duration, headers map[string]string, content []byte) ([]byte, error) {
	headerBytes := encodeSimpleMap(headers)
	classBytes := []byte(requestClass)
	frame := make([]byte, requestHeaderLen+len(classBytes)+len(headerBytes)+len(content))
	frame[0] = protocolCodeV1
	frame[1] = requestType
	binary.BigEndian.PutUint16(frame[2:4], cmdRPCRequest)
	frame[4] = cmdVersion
	binary.BigEndian.PutUint32(frame[5:9], id)
	frame[9] = codecHessian2
	binary.BigEndian.PutUint32(frame[10:14], uint32(timeout/time.Millisecond))
	binary.BigEndian.PutUint16(frame[14:16], uint16(len(classBytes)))
	binary.BigEndian.PutUint16(frame[16:18], uint16(len(headerBytes)))
	binary.BigEndian.PutUint32(frame[18:22], uint32(len(content)))
	offset := requestHeaderLen
	copy(frame[offset:], classBytes)
	offset += len(classBytes)
	copy(frame[offset:], headerBytes)
	offset += len(headerBytes)
	copy(frame[offset:], content)
	return frame, nil
}

func roundTrip(ctx context.Context, addr string, frame []byte) (boltResponse, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return boltResponse{}, &ConnectError{Address: addr, Err: err}
	}
	defer conn.Close()
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-done:
		}
	}()
	defer close(done)
	if deadline, ok := ctx.Deadline(); ok {
		if err := conn.SetDeadline(deadline); err != nil {
			return boltResponse{}, err
		}
	}
	if _, err := conn.Write(frame); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return boltResponse{}, ctxErr
		}
		return boltResponse{}, fmt.Errorf("write request: %w", err)
	}
	resp, err := readBoltResponse(conn)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return boltResponse{}, ctxErr
		}
	}
	return resp, err
}

type boltResponse struct {
	RequestID uint32
	Status    uint16
	Class     string
	Headers   map[string]string
	Content   []byte
	Codec     byte
}

func readBoltResponse(r io.Reader) (boltResponse, error) {
	fixed := make([]byte, responseHeaderLen)
	if _, err := io.ReadFull(r, fixed); err != nil {
		return boltResponse{}, fmt.Errorf("read response header: %w", err)
	}
	if fixed[0] != protocolCodeV1 || fixed[1] != responseType {
		return boltResponse{}, fmt.Errorf("unexpected BOLT response header")
	}
	if binary.BigEndian.Uint16(fixed[2:4]) != cmdRPCResponse || fixed[4] != cmdVersion {
		return boltResponse{}, fmt.Errorf("unexpected BOLT response command")
	}
	if fixed[9] != codecHessian2 {
		return boltResponse{}, fmt.Errorf("unsupported BOLT response codec %d", fixed[9])
	}
	classLen := int(binary.BigEndian.Uint16(fixed[12:14]))
	headerLen := int(binary.BigEndian.Uint16(fixed[14:16]))
	contentLen := int(binary.BigEndian.Uint32(fixed[16:20]))
	total := classLen + headerLen + contentLen
	if total > maxResponseBytes {
		return boltResponse{}, fmt.Errorf("response body length %d exceeds limit %d", total, maxResponseBytes)
	}
	body := make([]byte, total)
	if _, err := io.ReadFull(r, body); err != nil {
		return boltResponse{}, fmt.Errorf("read response body: %w", err)
	}
	headerStart := classLen
	contentStart := classLen + headerLen
	return boltResponse{
		RequestID: binary.BigEndian.Uint32(fixed[5:9]),
		Status:    binary.BigEndian.Uint16(fixed[10:12]),
		Class:     string(body[:classLen]),
		Headers:   decodeSimpleMap(body[headerStart:contentStart]),
		Content:   append([]byte(nil), body[contentStart:]...),
		Codec:     fixed[9],
	}, nil
}

func encodeSimpleMap(values map[string]string) []byte {
	if len(values) == 0 {
		return nil
	}
	keys := orderedHeaderKeys(values)
	out := make([]byte, 0, len(values)*24)
	for _, k := range keys {
		out = appendSizedString(out, k)
		out = appendSizedString(out, values[k])
	}
	return out
}

func decodeSimpleMap(data []byte) map[string]string {
	out := map[string]string{}
	for offset := 0; offset+4 <= len(data); {
		k, next, ok := readSizedString(data, offset)
		if !ok {
			return out
		}
		v, after, ok := readSizedString(data, next)
		if !ok {
			return out
		}
		out[k] = v
		offset = after
	}
	return out
}

func appendSizedString(dst []byte, s string) []byte {
	var b [4]byte
	binary.BigEndian.PutUint32(b[:], uint32(len(s)))
	dst = append(dst, b[:]...)
	dst = append(dst, s...)
	return dst
}

func readSizedString(data []byte, offset int) (string, int, bool) {
	if len(data[offset:]) < 4 {
		return "", offset, false
	}
	size := int(binary.BigEndian.Uint32(data[offset : offset+4]))
	offset += 4
	if size < 0 || len(data[offset:]) < size {
		return "", offset, false
	}
	return string(data[offset : offset+size]), offset + size, true
}

func orderedHeaderKeys(values map[string]string) []string {
	preferred := []string{
		"service",
		"sofa_head_method_name",
		"sofa_head_target_service",
		"sofa_head_generic_type",
		"type",
		"generic.revise",
		"sofa_head_target_app",
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, k := range preferred {
		if _, ok := values[k]; ok {
			out = append(out, k)
			seen[k] = true
		}
	}
	for k := range values {
		if !seen[k] {
			out = append(out, k)
		}
	}
	return out
}

func normalizeAddress(address string) string {
	address = strings.TrimSpace(address)
	address = strings.TrimPrefix(address, "bolt://")
	return address
}

func responseDiagnostics(resp boltResponse, targetService string, id uint32) map[string]interface{} {
	return map[string]interface{}{
		"transport":               "go-direct-bolt",
		"requestId":               id,
		"responseRequestId":       resp.RequestID,
		"targetServiceUniqueName": targetService,
		"responseStatus":          resp.Status,
		"responseClass":           resp.Class,
		"responseCodec":           resp.Codec,
		"responseContentLength":   len(resp.Content),
	}
}
