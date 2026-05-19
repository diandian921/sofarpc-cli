# Pure-Go Runtime

`sofarpc-mcp` and `sofarpc-cli` invoke SofaRPC directly from Go.

## Runtime Shape

- `sofarpc-mcp`: stdio MCP server for agents.
- `sofarpc-cli`: human-facing config, diagnostics, and reproduction tool.
- `internal/app`: application use cases for resolve, invocation planning, and
  execution.
- `internal/javavalue`: Java-aware typed value model passed from planning to the
  Hessian encoder.
- `internal/direct`: BOLT frame codec, Hessian2 reader/writer, request/response
  transport, and raw Java response decoding.
- `internal/presentation`: pure result flattening and assertion evaluation for
  agent-friendly JSON.
- `internal/schema`: local Java source parser for service and DTO discovery.

There is no sidecar runtime, local TCP control plane, token file, or managed process lifecycle.

## Invocation

The direct runtime sends a SofaRPC generic invocation over BOLT:

1. Resolve the configured project, server, endpoint, and method signature.
2. Build an invocation plan with Java-aware typed arguments.
3. Build a `SofaRequest`.
4. Encode request and arguments with Hessian2.
5. Send a BOLT RPC request frame to the configured provider address.
6. Decode `SofaResponse` into a raw Java object tree in `internal/direct`.
7. Flatten DTO-like Hessian objects into JSON-friendly maps in the application
   use case through `internal/presentation`.

`java.math.BigDecimal` values are normalized to JSON numbers. `byte[]` response
values use Go's standard JSON base64 representation. `BigInteger` currently has
limited support; see `compatibility-matrix.md`.
DTO field type metadata is carried in the internal typed value model rather than
in user argument maps.
Set `rawResult=true` on MCP invoke when the decoded Java object shape is needed for troubleshooting.

## Agent Contract

Agents should prefer MCP:

- `sofarpc_config`
- `sofarpc_resolve`
- `sofarpc_probe`
- `sofarpc_describe`
- `sofarpc_invoke`
- `sofarpc_doctor`

`sofarpc_resolve` is read-only and explains which configured project, server,
and endpoint will be used. `sofarpc_probe` is the only reachability check and
does not prove service or method existence.

`sofarpc_describe` accepts either a `query` for source search or a `service`
FQN plus optional `method` for schema description.

`sofarpc_invoke` accepts either:

- `paramTypes + orderedArguments` for exact invocation.
- `arguments` for schema-guided named arguments when local source can resolve the method.
- `dryRun=true` to return the resolved plan without sending a SofaRPC request.

Assertions are not exposed on the MCP invoke tool. They remain available on the `sofarpc-cli invoke --assertions-json` reproduction path, where the caller owns the exact request contract. There is no separate protocol envelope; MCP and CLI emit one shared rendered result contract.

## Known Limits

- Only direct BOLT/Hessian2 is implemented.
- Schema discovery uses local Java source only.
- External jar parents and generated DTO fields are not loaded.
- Request encoding rejects cyclic values and does not preserve shared object references.
- Go request encoding for `java.util.Date`, `BigInteger`, enum payloads without source schema, and provider-specific Hessian extensions require more compatibility work before being treated as broadly supported. Schema-known enum parameters and DTO fields are covered by Hessian oracle tests.
- Flattened map keys are strings; use `rawResult=true` for response-shape diagnosis when key type matters.
- `sofarpc_probe` is a TCP reachability check; it does not prove service or method existence.

## Safety Notes

The MCP server's stdout is the JSON-RPC frame stream; logging must stay on stderr. `sofarpc_probe` accepts explicit diagnostic addresses, so configured servers are the safer default when agent input is not fully trusted.

Stdin JSON-RPC frames are capped at 128 MiB. Oversized frames return a parse error and do not stop the server.
