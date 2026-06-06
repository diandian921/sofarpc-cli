package tools

import (
	"context"
	"encoding/json"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

const compatibilityResourceURI = "sofarpc://compatibility"

// compatEntry is one row of the Java/Hessian type support summary.
type compatEntry struct {
	Feature string `json:"feature"`
	Status  string `json:"status"`
	Notes   string `json:"notes"`
}

// compatibilitySummary is a curated, machine-readable digest of the pure-Go
// BOLT/Hessian2 type support (the prose source of truth is docs/compatibility-matrix.md).
// It is safe to expose as a resource: no config, no secrets, just the runtime's
// Java/Hessian capability matrix. Built from structs so it is always valid JSON.
var compatibilitySummary = func() json.RawMessage {
	doc := map[string]interface{}{
		"runtime":   "pure Go direct BOLT/Hessian2",
		"reference": "docs/compatibility-matrix.md",
		"types": []compatEntry{
			{"Integer / Long / Double", "supported", "Numeric request encoding uses the declared Java types, so values do not depend on Go JSON number shape."},
			{"String", "supported", "UTF-16 length with Java-compatible CESU-8 bytes; non-BMP characters supported."},
			{"BigDecimal", "supported", "Flattened result is a JSON number."},
			{"BigInteger", "supported", "Encoded as the signum/mag object form; request input is a string or integer JSON number."},
			{"byte[]", "supported", "Request input is an array of bytes in [-128, 255]; response JSON uses base64."},
			{"DTO object / nested DTO", "supported", "Shared object references are not preserved on request encoding."},
			{"List / Set", "supported", "Set is serialized by Java as a typed list and decodes to an ordered list shape."},
			{"Map", "partial", "Flattened results stringify keys; set rawResult=true to inspect raw key types."},
			{"java.time.LocalDate / LocalDateTime / Instant", "supported", "Request args accept ISO strings; these three types only."},
			{"java.util.Date", "partial", "Decodes as epoch millis; Go request encoding as a Date tag is not yet implemented."},
			{"Enum", "partial", "Needs source schema to encode as a Java enum object; explicit-address calls without schema cannot infer enum types."},
			{"Overloaded methods", "supported", "Provide paramTypes when overloads are ambiguous."},
			{"Cyclic request values", "rejected", "Object reference / cycle preservation is not implemented for request encoding."},
			{"Cyclic / shared object responses", "supported", "Hessian back-references resolve into shared objects; presentation cuts cycles with a $circularRef marker."},
		},
		"notes": []string{
			"Schema-known param/DTO types drive numeric and enum encoding.",
			"Set rawResult=true on sofarpc_invoke to inspect the decoded Java object tree.",
		},
	}
	body, err := json.Marshal(doc)
	if err != nil {
		panic(err)
	}
	return body
}()

// AddCompatibilityResource registers the read-only sofarpc://compatibility resource and
// turns on the server's resources capability. The config file is intentionally NOT
// exposed as a resource because it can carry credential-bearing attachments.
func AddCompatibilityResource(srv *mcpsdk.Server) {
	srv.AddResource(&mcpsdk.Resource{
		Name:        "compatibility",
		Title:       "SofaRPC Type Compatibility",
		URI:         compatibilityResourceURI,
		Description: "Read-only Java/Hessian2 type support matrix for the pure-Go BOLT runtime. No config or secrets.",
		MIMEType:    "application/json",
	}, func(_ context.Context, _ *mcpsdk.ReadResourceRequest) (*mcpsdk.ReadResourceResult, error) {
		return &mcpsdk.ReadResourceResult{
			Contents: []*mcpsdk.ResourceContents{
				{URI: compatibilityResourceURI, MIMEType: "application/json", Text: string(compatibilitySummary)},
			},
		}, nil
	})
}
