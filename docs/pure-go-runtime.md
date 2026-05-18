# Pure-Go Runtime

`sofarpc-mcp` and `sofarpc-cli` invoke SofaRPC directly from Go.

## Runtime Shape

- `sofarpc-mcp`: stdio MCP server for agents.
- `sofarpc-cli`: human-facing config, diagnostics, and reproduction tool.
- `internal/direct`: BOLT frame codec, Hessian2 reader/writer, request builder, and response flattener.
- `internal/invoker`: protocol envelope adapter for `invoke` and `ping`.
- `internal/schema`: local Java source parser for service and DTO discovery.

There is no sidecar runtime, local TCP control plane, token file, or managed process lifecycle.

## Invocation

The direct runtime sends a SofaRPC generic invocation over BOLT:

1. Build a `SofaRequest`.
2. Encode request and arguments with Hessian2.
3. Send a BOLT RPC request frame to the configured provider address.
4. Decode `SofaResponse`.
5. Flatten DTO-like Hessian objects into JSON-friendly maps.

`java.math.BigDecimal` and `java.math.BigInteger` values are normalized to JSON numbers.

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

## Known Limits

- Only direct BOLT/Hessian2 is implemented.
- Schema discovery uses local Java source only.
- External jar parents and generated DTO fields are not loaded.
- `sofarpc_probe` is a TCP reachability check; it does not prove service or method existence.
