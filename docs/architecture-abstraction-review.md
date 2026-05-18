# Architecture Abstraction Review

This document summarizes the desired abstraction shape for the pure-Go
SofaRPC agent runtime. It focuses on long-term code structure and product
clarity, not on immediate feature scope.

## Current Assessment

The project is already MCP-first at the product boundary: agents call
`sofarpc-mcp`, and the runtime invokes SofaRPC directly from Go.

The internal abstractions now have a single application stack for invoke and
probe. Remaining gaps are narrower:

- MCP still owns some non-invoke workflow details such as config and doctor
  response shaping.
- Result flattening still lives in `internal/direct` rather than a renderer
  layer.
- Hessian encode/decode is still concrete direct-runtime code rather than a
  formal codec port.
- Error and diagnostic information exists in a minimal form, but recovery hints
  are not yet complete.

The current code is practical and deliverable. The main improvement area is to
introduce a clearer application core so MCP, CLI, schema, codec, and transport
do not leak into each other.

## Implementation Status

This document is both a design target and an implementation guide. The current
codebase status is:

| Area | Status | Notes |
| --- | --- | --- |
| Application layer | Implemented | MCP invoke/resolve/probe and CLI invoke/ping/exec now route through `internal/app`. |
| `InvocationPlan` | Implemented | Invocation entry points converge on `PlanInvocation` before execution. |
| `TypedValue` | Implemented | Java type metadata is carried by `internal/javavalue`; user argument maps and the Hessian writer no longer use `@type`, `__type`, or `__fieldTypes` as an encoding path. |
| MCP session/dispatcher | Implemented | JSON-RPC read/write, async dispatch, cancellation, and stdout locking live in `internal/mcp/session.go`. |
| Source index port | Implemented | `internal/app.SourceIndex` hides local Java source indexing behind an interface. |
| Probe use case | Implemented | `ProbeEndpoint` is an app use case; MCP and CLI both use it. |
| CLI/exec migration | Implemented | `sofarpc-cli invoke`, `ping`, and `exec --stdin` no longer use a separate invoker stack. |
| Legacy invoker package | Removed | `internal/invoker` was deleted after CLI and MCP moved to app use cases. |
| Endpoint resolver port | Partial | Explicit address and configured server resolution are centralized in app code, but not yet exposed as a separate `EndpointResolver` interface. |
| Domain error model | Partial | Minimal `DomainError{Kind, Message, Details}` exists for planning errors; recovery hints are not complete. |
| Renderer layer | Not implemented | Result flattening still lives in `internal/direct`; MCP/CLI protocol rendering is adapter code. |
| Codec port | Not implemented | Hessian encode/decode still lives in `internal/direct`; no `Codec` interface yet. |
| Compatibility matrix | Not implemented | Compatibility samples exist as tests, but no visible matrix document is maintained yet. |
| Performance budget | Not implemented | Plan/probe diagnostics expose timings, but no thresholds or benchmark gate exist yet. |
| Dependency rule enforcement | Not implemented | Boundaries are cleaner, but no automated package-boundary test exists yet. |

## Desired Layering

The ideal dependency direction is:

```text
Inbound Adapter -> Application Use Case -> Plan -> Policy -> Codec -> Transport -> Renderer
```

Concrete package shape can evolve toward:

```text
internal/mcp
  session.go       JSON-RPC read/write, concurrency, cancellation
  tools.go         MCP tool definitions
  adapter.go       Tool call -> application command

internal/app
  invoke.go        PlanInvocation and ExecuteInvocation use cases
  resolve.go       ResolveService and ResolveMethod use cases
  describe.go      Schema inspection use cases
  diagnostics.go   Timing, warnings, and structured troubleshooting output

internal/schema
  index.go         SourceIndex and SchemaSnapshot
  parser.go        Java source parser implementation

internal/direct
  client.go        SofaRPC client over BOLT
  transport.go     BOLT request/response transport

internal/codec
  hessian.go       Hessian encode/decode implementation
  values.go        Java-aware value model

internal/config
  store.go         ConfigStore port and file-backed implementation
```

The exact package names are less important than the boundary: adapters translate
requests, the application layer decides what should happen, and direct/codec
layers only execute already-planned work.

## Core Domain Vocabulary

The project should stabilize around a small set of domain names:

- `Project`: a configured local source project.
- `Service`: a Java service interface identified by FQN.
- `MethodSignature`: method name plus exact Java parameter types.
- `Endpoint`: provider address and related routing metadata.
- `Invocation`: the user or agent request to call a method.
- `InvocationPlan`: the fully resolved, executable call plan.
- `TypedValue`: a Java-aware value tree used for encoding and diagnostics.
- `SourceIndex`: an index over local Java source.
- `SchemaSnapshot`: a consistent schema view for one project at one point in time.
- `Diagnostics`: structured timings, warnings, resolution details, and recovery hints.

Keeping code centered on these names will make the project easier to reason
about than a mix of `target`, `payload`, `resolve`, `description`, and ad hoc
JSON maps.

## Key Abstractions

### Application Layer

MCP and CLI should call application use cases instead of composing the whole
workflow themselves.

Suggested use cases:

- `ResolveService`
- `DescribeService`
- `PlanInvocation`
- `ExecuteInvocation`
- `ProbeEndpoint`
- `ReadConfig`
- `WriteConfig`

MCP tools should be thin adapters over these use cases. Tool names and MCP
schemas should not define the internal architecture.

### InvocationPlan

All entry points should converge on `InvocationPlan` before execution.

```go
type InvocationPlan struct {
    Project    ProjectRef
    Service    string
    Method     MethodSignature
    Endpoint   Endpoint
    Arguments  []TypedValue
    Timeout    time.Duration
    Policy     ExecutionPolicy
    Warnings   []PlanWarning
    Diagnostics Diagnostics
}
```

`dryRun` should become the public expression of `PlanInvocation`, not a special
mode inside invoke. `ExecuteInvocation` should only consume a plan.

### TypedValue

Java type metadata should not be injected into user argument maps.

Prefer an internal value model:

```go
type TypedValue struct {
    JavaType string
    Kind     ValueKind
    Scalar   any
    Fields   map[string]TypedValue
    Items    []TypedValue
    Entries  []MapEntry
}
```

This avoids collisions with real fields named `@type`, `__type`, or
`__fieldTypes`, and gives the codec a clear contract.

### SourceIndex and SchemaSnapshot

Schema should provide facts, not make invocation policy decisions.

```go
type SourceIndex interface {
    Snapshot(project ProjectRef) (*SchemaSnapshot, error)
}
```

The snapshot answers questions such as:

- Which services exist?
- Which methods and overloads exist?
- What are the DTO fields and Java types?

Planner code should decide how to handle ambiguity, missing schema, fallback,
and warnings.

### EndpointResolver

Source resolution and endpoint resolution are separate concerns.

```go
type EndpointResolver interface {
    ResolveEndpoint(ctx context.Context, req EndpointRequest) (Endpoint, error)
}
```

This keeps future sources such as config files, explicit address, local project
settings, or registry integration from being mixed into schema parsing.

### Codec Port

Hessian should be modeled as an implementation of a codec contract.

```go
type Codec interface {
    EncodeInvocation(plan InvocationPlan) ([]byte, error)
    DecodeResponse(data []byte, expected JavaType) (TypedValue, error)
}
```

This isolates Hessian compatibility work from MCP and application logic.

### Renderer

Decoding and display should be separate.

```text
Codec result -> TypedValue -> Renderer -> MCP/CLI JSON output
```

`rawResult`, flattening, preserved `@type`, and map-key rendering should be
renderer strategies, not codec behavior.

### Policy

Execution policy should be explicit instead of scattered through handlers.

Examples:

- Whether explicit diagnostic addresses are allowed.
- Whether config writes are allowed.
- Default timeout.
- Whether unresolved schema may still invoke.
- Whether fallback from named arguments to ordered arguments is allowed.

This can start as an `ExecutionPolicy` struct passed into application use cases.

### Diagnostics

Diagnostics should be structured output for agents, not just logs.

Example shape:

```json
{
  "timing": {
    "schemaResolveMs": 12,
    "planMs": 2,
    "encodeMs": 1,
    "rpcMs": 43,
    "decodeMs": 2,
    "renderMs": 1
  },
  "warnings": [],
  "resolution": {
    "project": "salesfundmp",
    "service": "com.example.PortfolioFacade",
    "method": "queryPortfolioLatestAsset",
    "endpointSource": "configured-server"
  }
}
```

For agents, structured diagnostics are part of the product. They let the agent
explain failures, retry correctly, and avoid guessing.

## Error Model

Errors should be domain errors with stable kinds and machine-readable context.

Suggested kinds:

- `PROJECT_NOT_FOUND`
- `SERVICE_NOT_FOUND`
- `SERVICE_AMBIGUOUS`
- `METHOD_NOT_FOUND`
- `METHOD_AMBIGUOUS`
- `SCHEMA_UNAVAILABLE`
- `ARGUMENT_TYPE_MISMATCH`
- `ENDPOINT_NOT_FOUND`
- `RPC_TIMEOUT`
- `RPC_CONNECTION_FAILED`
- `REMOTE_ERROR`
- `CODEC_UNSUPPORTED_VALUE`

Agent-facing errors should include recovery hints where possible:

- `METHOD_AMBIGUOUS`: return candidate signatures.
- `SCHEMA_UNAVAILABLE`: return candidate projects or source roots.
- `ARGUMENT_TYPE_MISMATCH`: return field path, expected Java type, and actual value.
- `RPC_TIMEOUT`: return endpoint, timeout, and elapsed time.

## Compatibility Contract

Pure-Go SofaRPC compatibility is a core product risk. It should be represented
as a contract test matrix, not only ordinary unit tests.

Important samples:

- primitives and boxed primitives
- `String`
- `BigDecimal` and `BigInteger`
- `java.util.Date`
- `java.time.*`
- enum
- `byte[]`
- nested DTO
- list, set, map
- null elements in collections
- overloaded methods
- remote exceptions
- long/int/double numeric boundaries
- shared references and cyclic input rejection

These tests define the supported protocol surface.

## Current Design Smells To Retire

- Magic metadata keys inside user values.
- MCP handlers that contain application workflow logic.
- Direct invocation paths that know protocol envelope details.
- Schema parser output consumed directly as execution policy.
- Result flattening mixed with response decoding.
- String-matched error classification.
- Repeated dynamic `interface{}` normalization across layers.

None of these are blockers for the current delivery, but each one increases the
cost of future features.

## Coverage Map

This review folds the 22 abstraction points from the discussion into the
following sections:

1. Application layer: covered by `Desired Layering` and `Application Layer`.
2. First-class `InvocationPlan`: covered by `InvocationPlan`.
3. Type metadata outside user values: covered by `TypedValue`.
4. Schema provides facts, not policy: covered by `SourceIndex and SchemaSnapshot`.
5. MCP server as a thin shell: covered by `Desired Layering` and `Application Layer`.
6. Direct runtime as a narrow client: covered by `Desired Layering`, `Codec Port`,
   and `Current Design Smells To Retire`.
7. Port/adapter boundary: covered by `Desired Layering`.
8. Stable domain vocabulary: covered by `Core Domain Vocabulary`.
9. Plan and execute separation: covered by `InvocationPlan`.
10. Config not read directly by every layer: covered by `Desired Layering` through
    the `internal/config` boundary and the application use cases.
11. Domain error model: covered by `Error Model`.
12. Return value as a domain model, not only JSON: covered by `TypedValue` and
    `Renderer`.
13. Schema index as a snapshot: covered by `SourceIndex and SchemaSnapshot`.
14. Tool definitions decoupled from use cases: covered by `Application Layer`.
15. Invocation lifecycle: covered by `Diagnostics` and should become the standard
    execution timeline from receive to render.
16. Independent endpoint resolution: covered by `EndpointResolver`.
17. Codec as a first-class port: covered by `Codec Port`.
18. Normalizer and renderer separation: covered by `TypedValue`, `Codec Port`,
    and `Renderer`.
19. Policy layer: covered by `Policy`.
20. Diagnostics as product output: covered by `Diagnostics`.
21. Compatibility contract: covered by `Compatibility Contract`.
22. Error recovery strategy: covered by `Error Model`.

## Code Elegance Coverage

The earlier code-level review is covered by this abstraction direction as
follows:

- Metadata injection with `__fieldTypes`: resolved by `TypedValue`.
- Overloaded MCP `Run()` orchestration: resolved by extracting MCP session and
  dispatcher concerns from `Desired Layering`.
- Duplicate `tools/call` decoding for async scheduling: resolved by making MCP
  a thin adapter that decodes once and passes a typed command to application use
  cases.
- Schema parser decisions leaking into invocation: reduced by `SourceIndex`,
  `SchemaSnapshot`, and planner-owned policy decisions.
- Hessian writer accumulating dynamic switches: reduced by `TypedValue` and
  `Codec Port`.
- Silent schema annotation fallback: addressed by `Diagnostics`, `Warnings`,
  and the `Error Model`.

Small local cleanups, such as simplifying redundant conditions or renaming
private helpers, are intentionally not architecture items. They should be fixed
opportunistically when touching the related files.

## Global Engineering Concerns

The abstraction model above should be backed by explicit engineering constraints.
These are the concerns that make the runtime maintainable, extensible, and
testable over time.

### Dependency Rules

Package dependency direction should be intentional and enforceable:

```text
cmd -> inbound adapters -> application -> outbound ports -> implementations
```

Expected direction:

```text
internal/mcp      -> internal/app
internal/cli      -> internal/app
internal/app      -> schema/direct/codec/config ports
internal/direct   -> internal/codec and transport internals
internal/schema   -> parser internals
```

Reverse dependencies should be avoided:

- `internal/app` should not import `internal/mcp` or CLI packages.
- `internal/direct` should not know MCP tool schemas or JSON-RPC envelopes.
- `internal/schema` should not decide invocation fallback policy.
- `internal/codec` should not render MCP or CLI output.

Eventually this should be checked by a lightweight package-boundary test or
`go list` script, so the architecture is executable instead of only documented.

### Composition Root

Dependency construction should happen in one small composition root per binary.
Handlers should not casually create config stores, schema indexes, codecs, and
clients.

Preferred shape:

```text
cmd/sofarpc-mcp
  load config
  construct ConfigStore
  construct SourceIndex
  construct EndpointResolver
  construct Codec
  construct RpcClient
  construct Application
  construct MCP server

cmd/sofarpc-cli
  construct the same Application
  expose human-facing commands
```

This keeps MCP and CLI behavior aligned and makes use cases easy to test with
fake ports.

### Test Strategy

Testing should be layered by responsibility:

```text
unit
  planner, typed values, domain errors, policy, endpoint resolution

contract
  Hessian encode/decode samples and SofaRPC wire compatibility

integration
  MCP JSON-RPC session, cancellation, stdout framing, config store behavior

real environment
  smoke tests against a real SofaRPC provider

performance
  cold start, tools/list, resolve cold/warm, first invoke, warm invoke

cross-platform
  darwin, linux, windows build and packaging checks
```

Real-provider validation is important, but it should complement deterministic
unit, contract, and integration tests. It should not be the only safety net.

### Performance Budget

Startup and first-call latency should be treated as a product contract because
this tool is used inside agent sessions.

Suggested tracked stages:

- `mcpStartMs`: process start until initialize is handled.
- `toolsListMs`: `tools/list` response latency.
- `resolveColdMs`: first schema resolve for a project.
- `resolveWarmMs`: cached schema resolve.
- `planMs`: argument and method planning.
- `encodeMs`: Hessian request encoding.
- `rpcMs`: provider round trip.
- `decodeMs`: Hessian response decoding.
- `renderMs`: MCP/CLI result rendering.

The exact thresholds can be set after repeated measurements. The important
point is to keep the stages stable so regressions are visible.

### MCP Tool Contract Versioning

MCP tool names, parameter schemas, and response shapes are a public API for
agents.

Compatibility rules:

- Adding optional fields is acceptable.
- Removing fields, renaming fields, or changing response meaning should be
  treated as a breaking change.
- Deprecated fields should remain readable for at least one compatibility
  window.
- Tool response errors should use stable `ErrorKind` values where possible.

The application layer should not depend on MCP tool names. MCP should adapt to
application use cases, not the reverse.

### Config Schema Versioning

The configuration file should have an explicit schema version.

Expected behavior:

- Read older supported versions.
- Migrate in memory before use.
- Write only the current version.
- Preserve unknown fields only when the format intentionally supports extension.
- Return a structured error for unsupported future versions.

This keeps user-managed config editable while still allowing the tool to evolve.

### Observability

Diagnostics and logs serve different audiences.

Agent-facing diagnostics should be structured and returned with results or
errors. Local logs should go to stderr and help humans debug the tool itself.

Useful observability fields:

- trace id or request id
- project, service, method, and endpoint source
- phase timings
- cache hit or miss
- selected method signature
- fallback decisions
- error kind and recovery hint

This is especially important for separating schema, encode, network, provider,
decode, and rendering failures.

### Compatibility Matrix

The project should maintain a visible compatibility matrix for Java and SofaRPC
features.

Each row should track:

```text
feature / support status / test coverage / known limits
```

Examples:

- primitive and boxed numeric types
- `String`
- `BigDecimal`
- `BigInteger`
- `Date`
- `java.time.*`
- enum
- `byte[]`
- DTO
- nested DTO
- list and set
- map
- overloaded methods
- remote exceptions

This matrix prevents accidental overclaiming and helps prioritize protocol work.

### Security Boundary

Agent-facing tools should make safety policy explicit.

Policy-sensitive actions include:

- probing arbitrary explicit addresses
- invoking arbitrary explicit addresses
- writing config
- reading local source roots
- following future remote schema sources

The default posture should prefer configured projects and configured endpoints.
Explicit addresses and write operations should be governed by policy rather than
being incidental handler behavior.

### Reproducible Releases

Release artifacts should be reproducible and inspectable.

Recommended release metadata:

- version
- commit SHA
- target OS and architecture
- build flags
- checksums
- package contents
- install script version

The packaging flow should produce the same binary names and archive layout for
all supported platforms.

## Recommended Refactor Order

1. Introduce `InvocationPlan` and move `dryRun` to `PlanInvocation`.
2. Introduce `TypedValue` and remove `__fieldTypes` from user-shaped maps.
3. Extract MCP session/dispatcher from the current server loop.
4. Split `EndpointResolver` from source/schema resolution.
5. Move flatten/raw output into a renderer layer.
6. Replace string-matched errors with domain error kinds.
7. Move Hessian encode/decode behind a codec contract.
8. Formalize the compatibility test matrix.

The first two steps provide the biggest architecture payoff. Once
`InvocationPlan` and `TypedValue` are stable, MCP, CLI, schema, and codec can
evolve independently.

## Non-Goals

This review does not require:

- Reintroducing a daemon or sidecar process.
- Adding an HTTP control plane.
- Building a full Java parser immediately.
- Managing saved test cases.
- Adding assertions to MCP invoke.

The goal is a smaller and cleaner agent runtime, not a broader product surface.
