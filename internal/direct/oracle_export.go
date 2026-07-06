//go:build bolt_oracle

package direct

// Oracle bridge: re-exports codec internals for the oracletest module, which
// verifies the hand-written BOLT framing against the official sofa-bolt-go
// implementation. Compiled only under the bolt_oracle tag so none of this
// surface exists in normal builds.

import (
	"io"
	"time"
)

const (
	OracleRequestClass  = requestClass
	OracleResponseClass = responseClass
	OracleCmdVersion    = cmdVersion
	OracleCodecHessian2 = codecHessian2
)

// OracleBoltResponse aliases the internal response frame so the oracletest
// module can inspect decoded fields.
type OracleBoltResponse = boltResponse

func OracleTargetServiceName(service, version, uniqueID string) string {
	return targetServiceName(service, version, uniqueID)
}

func OracleRequestHeader(method, targetService, appName string) map[string]string {
	return requestHeader(method, targetService, appName)
}

func OracleEncodeBoltRequest(id uint32, timeout time.Duration, headers map[string]string, content []byte) ([]byte, error) {
	return encodeBoltRequest(id, timeout, headers, content)
}

func OracleReadBoltResponse(r io.Reader) (OracleBoltResponse, error) {
	return readBoltResponse(r)
}

func OracleEncodeSimpleMap(values map[string]string) []byte {
	return encodeSimpleMap(values)
}

func OracleDecodeSimpleMap(data []byte) map[string]string {
	return decodeSimpleMap(data)
}
