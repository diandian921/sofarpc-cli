# SofaRPC Compatibility Matrix

This matrix tracks the pure-Go direct BOLT/Hessian2 runtime against real Java
Hessian behavior. Default tests use oracle-bound Java-generated golden bytes
and do not require a JVM. The optional oracle test,
`go test -tags hessian_oracle ./internal/direct`, compiles and runs a local
Java Hessian helper when `javac`, `java`, and a local Hessian jar are available.
That oracle test verifies the checked-in golden hex matches Java helper output
byte-for-byte.

Decode coverage means Java Hessian bytes are frozen as golden input and decoded
by Go in CI. Encode coverage means the optional JVM oracle has verified that Go
bytes are readable by Java hessian-lite. Default golden tests also assert the
JSON presentation shape that MCP and CLI return to agents.

| Feature | Status | Coverage | Known Limits |
| --- | --- | --- | --- |
| `Integer`, `Long`, `Double` | Supported | Golden decode + optional encode oracle | Numeric request encoding depends on declared Java types. |
| `String` | Supported | Golden decode + optional encode oracle, including non-BMP characters | Hessian strings use UTF-16 code unit length and Java-compatible CESU-8 bytes. |
| `BigDecimal` | Supported | Golden decode + optional encode oracle | Flattened result is a JSON number. |
| `BigInteger` | Limited | Java -> Go known-gap test | Go request encoding is explicitly rejected because Java Hessian reads the old value-field shape as `0`; Java responses decode to raw internal fields rather than a clean value. |
| `java.util.Date` | Partial | Golden decode | Java wire date decodes as epoch millis. Go request encoding for Date is not yet implemented as a Date tag. |
| `byte[]` | Supported | Golden decode + presentation JSON + optional encode oracle | JSON input should be an array of byte numbers in `[-128, 255]`. Response JSON uses base64 for raw byte slices. |
| DTO object | Supported | Golden decode + presentation JSON + optional encode oracle | Shared object references are not preserved on request encoding. |
| Nested DTO with list/map/null fields | Supported | Golden decode + presentation JSON + optional encode oracle | Covered as a common DTO response shape, not as full object graph reference preservation. |
| List with null elements | Supported | Golden decode + presentation JSON + optional encode oracle | Set-specific behavior is not separately covered yet. |
| Map | Partial | Golden decode + presentation JSON + optional encode oracle with non-string key | Normal flattened results stringify map keys. Use `rawResult=true` for diagnosis. |
| Cyclic request values | Rejected | Go unit test via depth guard | Object reference table and cycle preservation are not implemented for request encoding. |
| Overloaded methods | Supported at planner level | App/MCP tests | Caller must provide `paramTypes` when overloads are ambiguous. |
| `java.time.*` | Not supported | None | Needs real Java contract samples before claiming support. |
| Enum | Not supported | None | Some enum-like responses may flatten by `name`, but wire compatibility is not yet contracted. |
| Remote exception payloads | Not fully contracted | Basic RPC error path tests | Provider-specific exception fields need real SofaRPC samples. |
| Provider-specific Hessian extensions | Not supported | None | Treat as unsupported until captured in contract tests. |

When a row moves from limited or unsupported to supported, add a real Java
contract sample first, then update this matrix.
