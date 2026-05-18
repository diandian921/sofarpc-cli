# Architecture Abstraction Review

This document summarizes the abstraction shape for the pure-Go SofaRPC agent
runtime. It is a guardrail, not a mandate to keep adding layers. The current
highest-risk area is SofaRPC/Hessian compatibility against real Java endpoints;
structural cleanup is valuable only when it makes that risk easier to test,
debug, or contain.

## Current Assessment

The project is already MCP-first at the product boundary: agents call
`sofarpc-mcp`, and the runtime invokes SofaRPC directly from Go.

The internal abstractions now have a single application stack for invoke and
probe. The useful architecture work already landed:

- MCP and CLI invoke/probe paths converge through `internal/app`.
- `InvocationPlan` is the planning boundary for execution.
- `TypedValue` is the only internal representation for Java-aware arguments.
- MCP session concerns are separated from tool behavior.

Remaining gaps are narrower:

- MCP still owns some non-invoke workflow details such as config and doctor
  response shaping.
- `direct.Invoke` still returns both flattened and raw result shapes for
  compatibility, but flattening itself now lives in `internal/presentation`.
- Error and diagnostic information exists in a minimal form, but recovery hints
  are not yet complete.

The current code is practical and deliverable. The main improvement area is no
longer "add more abstraction"; it is "make the protocol surface executable and
trustworthy."

## Priority Reset

The product succeeds or fails on two hard surfaces:

1. Pure-Go Hessian/SofaRPC wire compatibility with real Java implementations.
2. Local Java source parsing accuracy for the interfaces agents ask to test.

The remaining engineering investment should prioritize executable contract
tests and visible compatibility status over additional interface seams.

Concrete priority:

1. Keep the abstraction stop line explicit.
2. Done: `exec --stdin` and `internal/protocol` were removed (no real
   consumer). MCP and CLI now emit one shared rendered contract, `app.Result`,
   built by `app.RenderExecution`/`RenderProbe`/`RenderFailure`.
3. Done: flattening and assertion evaluation were extracted into
   `internal/presentation`, so the stable contract is
   `decode -> decoded tree -> presentation JSON`.
4. Expand the codec compatibility harness with the hybrid model: default CI
   runs signed golden bytes; optional JVM oracle tests generate and validate the
   corpus.
5. Add a parallel parser golden corpus for real Java facade/DTO snippets.

Do not introduce `Codec`, `EndpointResolver`, `Policy`, or similar interfaces
just because the conceptual boundary exists. Add an interface only when a
second implementation or a concrete test fake is actually needed.

## Implementation Status

This document is both a design target and an implementation guide. The current
codebase status is:

| Area | Status | Notes |
| --- | --- | --- |
| Application layer | Implemented | MCP invoke/resolve/probe and CLI invoke/ping now route through `internal/app`. |
| `InvocationPlan` | Implemented | Invocation entry points converge on `PlanInvocation` before execution. |
| `TypedValue` | Implemented | Java type metadata is carried by `internal/javavalue`; user argument maps and the Hessian writer no longer use `@type`, `__type`, or `__fieldTypes` as an encoding path. |
| MCP session/dispatcher | Implemented | JSON-RPC read/write, async dispatch, cancellation, and stdout locking live in `internal/mcp/session.go`. |
| Source index port | Implemented | `internal/app.SourceIndex` hides local Java source indexing behind an interface. |
| Probe use case | Implemented | `ProbeEndpoint` is an app use case; MCP and CLI both use it. |
| CLI migration | Implemented | `sofarpc-cli invoke`/`ping` route through app use cases and emit the shared `app.Result` contract. |
| Legacy invoker package | Removed | `internal/invoker` was deleted after CLI and MCP moved to app use cases. |
| `exec --stdin` + `internal/protocol` | Removed | No real consumer existed. The stdin envelope, the `internal/protocol` package, and the root `protocol/` schema/fixtures were deleted. Single rendered contract is `app.Result`. |
| Endpoint resolution | Implemented enough | Explicit address and configured server resolution are centralized in app code. Do not add an `EndpointResolver` interface until there is a second resolver implementation or a real test need. |
| Domain error model | Partial | Minimal `DomainError{Kind, Message, Details}` exists for planning errors; recovery hints are not complete. |
| Presentation renderer | Implemented | `internal/presentation` owns result flattening and assertion evaluation as pure functions. No renderer strategy interface exists. |
| Codec interface | Deferred | Hessian encode/decode still lives in `internal/direct`. The next step is Java compatibility tests, not a `Codec` port. |
| Compatibility matrix | Partial | `docs/compatibility-matrix.md` is backed by signed Java Hessian golden bytes, presentation JSON assertions, and optional JVM oracle tests under the `hessian_oracle` build tag. It now covers nested DTO/list/map/null response shapes; expand parser and provider samples before claiming more codec surface. |
| Parser golden corpus | Partial | Golden fixtures under `internal/schema/testdata/golden` now cover the sales facade shape plus a modern Java fixture with method annotations, parameter annotations, Lombok-style DTO fields, records, nested generics, and overloaded facade methods. Expand with more real facade samples before claiming broad parser compatibility. |
| Performance budget | Not implemented | Plan/probe diagnostics expose timings, but no thresholds or benchmark gate exist yet. |
| Dependency rule enforcement | Not implemented | Boundaries are cleaner, but no automated package-boundary test exists yet. |

## Boundary Model

The useful dependency direction is:

```text
Inbound Adapter -> Application Use Case -> Direct Runtime
                                  \-> Presentation
```

`internal/direct` returns decoded Java object trees. It does not flatten,
render, or choose agent-facing JSON shapes. The application use case owns the
decision to pass decoded values through `internal/presentation` before MCP or
CLI adapters render the shared `app.Result` contract.

Conceptual package shape:

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
  index.go         source indexing and SchemaSnapshot
  parser.go        Java source parser implementation

internal/direct
  client.go        SofaRPC client over BOLT
  transport.go     BOLT request/response transport
  hessian_*.go     Hessian encode/decode implementation

internal/presentation
  result.go        Flatten decoded Java object trees into agent-friendly JSON
  assertions       Assertion evaluation for CLI reproduction

internal/config
  store.go         file-backed configuration
```

The exact package names are less important than the boundary: adapters translate
requests, the application layer decides what should happen, and direct/codec
layers only execute already-planned work. This model is not permission to add
interfaces before they have a second consumer.

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

### Endpoint Resolution

Source resolution and endpoint resolution are separate concerns.

For now this should remain concrete app code. If a second resolver source
appears, such as registry integration, then an `EndpointResolver` interface may
be worth introducing. Before that point, a helper function is simpler and more
idiomatic Go.

### Hessian Codec

Hessian encode/decode is the core correctness risk. The immediate need is not a
`Codec` interface; it is executable compatibility coverage against real Java
Hessian/SofaRPC implementations.

After a compatibility harness exists, a codec boundary may be considered if it
reduces testing friction or if a second codec appears. Until then, keep the code
concrete and heavily tested.

### Renderer

Decoding and display should be separate.

```text
Codec result -> TypedValue -> Renderer -> MCP/CLI JSON output
```

`rawResult`, flattening, preserved `@type`, and map-key rendering should be
presentation behavior, not transport behavior. The first extraction should be a
small pure function package plus contract tests, not a pluggable renderer
strategy.

### Policy

Execution policy should be explicit instead of scattered through handlers.

Examples:

- Whether explicit diagnostic addresses are allowed.
- Whether config writes are allowed.
- Default timeout.
- Whether unresolved schema may still invoke.
- Whether fallback from named arguments to ordered arguments is allowed.

This can start as concrete fields passed into application use cases. Do not add
a policy interface until there is a second policy source or a test that cannot
be expressed cleanly without one.

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

The harness has two modes:

- Default CI: Go tests decode signed Java-generated golden bytes and assert the
  frozen decode and presentation contracts. No JVM is required.
- Optional oracle: `go test -tags hessian_oracle ./internal/direct` compiles a
  local Java Hessian helper and verifies both directions against hessian-lite.
  This is the path for refreshing or adding golden samples.

Direction matters:

- Decode direction: Java Hessian bytes -> Go decode can be covered by signed
  golden bytes.
- Encode direction: Go bytes -> Java Hessian decode cannot be proven by golden
  bytes alone. It needs the optional JVM oracle when creating or refreshing
  samples.

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

The current default corpus covers boxed numeric values, non-BMP strings,
`BigDecimal`, Java Date wire values, `byte[]`, map/list/null samples, DTOs, and
a nested DTO response with list/map/null fields. The optional oracle also
verifies Go-encoded nested DTO values against Java hessian-lite.

## Parser Contract

Codec compatibility is only half the correctness surface. If source parsing
resolves the wrong method signature or DTO fields, the runtime can encode the
wrong call correctly.

The parser should maintain a parallel golden corpus:

```text
realistic Java facade/DTO snippet -> expected MethodSignature and DTO schema
```

High-priority parser samples:

- annotated facade parameters
- Chinese Javadoc recall
- nested generic return and parameter types
- DTO fields with `byte[]`, `BigDecimal`, and collections
- Lombok-style DTOs
- Java records
- nested interfaces
- overloaded methods

Current coverage includes Chinese Javadocs, nested generics, DTO collection
fields, Lombok-style DTO fields, Java records, nested interfaces, overloaded
facade methods, and method and parameter annotations. More real project facade
samples remain the highest-value next parser work.

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

1. Application layer: covered by `Boundary Model` and `Application Layer`.
2. First-class `InvocationPlan`: covered by `InvocationPlan`.
3. Type metadata outside user values: covered by `TypedValue`.
4. Schema provides facts, not policy: covered by `SourceIndex and SchemaSnapshot`.
5. MCP server as a thin shell: covered by `Boundary Model` and `Application Layer`.
6. Direct runtime as a narrow client: covered by `Boundary Model`, `Hessian Codec`,
   and `Current Design Smells To Retire`.
7. Port/adapter boundary: covered by `Boundary Model`.
8. Stable domain vocabulary: covered by `Core Domain Vocabulary`.
9. Plan and execute separation: covered by `InvocationPlan`.
10. Config not read directly by every layer: covered by `Boundary Model` through
    the config boundary and the application use cases.
11. Domain error model: covered by `Error Model`.
12. Return value as a domain model, not only JSON: covered by `TypedValue` and
    `Renderer`.
13. Schema index as a snapshot: covered by `SourceIndex and SchemaSnapshot`.
14. Tool definitions decoupled from use cases: covered by `Application Layer`.
15. Invocation lifecycle: covered by `Diagnostics` and should become the standard
    execution timeline from receive to render.
16. Independent endpoint resolution: covered by `Endpoint Resolution`.
17. Codec correctness: covered by `Hessian Codec` and `Compatibility Contract`.
18. Normalizer and renderer separation: covered by `TypedValue`, `Hessian Codec`,
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
  dispatcher concerns from `Boundary Model`.
- Duplicate `tools/call` decoding for async scheduling: resolved by making MCP
  a thin adapter that decodes once and passes a typed command to application use
  cases.
- Schema parser decisions leaking into invocation: reduced by `SourceIndex`,
  `SchemaSnapshot`, and planner-owned policy decisions.
- Hessian writer accumulating dynamic switches: reduced by `TypedValue` and
  should now be controlled by real compatibility contract tests.
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
cmd -> inbound adapters -> application -> concrete implementations
```

Expected direction:

```text
internal/mcp      -> internal/app
internal/cli      -> internal/app
internal/app      -> schema/direct/presentation/config helpers
internal/direct   -> transport and hessian internals
internal/schema   -> parser internals
```

Reverse dependencies should be avoided:

- `internal/app` should not import `internal/mcp` or CLI packages.
- `internal/direct` should not know MCP tool schemas or JSON-RPC envelopes.
- `internal/direct` should not import `internal/presentation`; it returns raw
  decoded Java values only.
- `internal/schema` should not decide invocation fallback policy.
- Hessian encode/decode should not render MCP or CLI output.

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
  construct direct runtime client
  construct application services
  construct MCP server

cmd/sofarpc-cli
  construct the same application services
  expose human-facing commands
```

This keeps MCP and CLI behavior aligned and makes use cases easy to test with
small fakes where they are actually useful.

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

Done:

1. Introduce `InvocationPlan` and move `dryRun` to planning.
2. Introduce `TypedValue` and remove magic metadata keys from user-shaped maps.
3. Extract MCP session/dispatcher from the server loop.
4. Route MCP and CLI through the same app stack and remove the legacy invoker.
5. Replace string-matched planning errors with stable domain error kinds.
6. Remove `exec --stdin` and `internal/protocol`; MCP and CLI emit one shared
   `app.Result` contract.
7. Extract result flattening and assertion evaluation into
   `internal/presentation`.

Next:

1. Expand codec golden coverage with the hybrid model: signed Java golden bytes
   in default CI, optional JVM oracle under `hessian_oracle` for generation and
   bidirectional verification.
2. Expand parser golden coverage for real facade/DTO syntax.
3. Move final rendering ownership fully out of `direct.Invoke` once app callers
   can consume raw decoded results directly.
4. Add package-boundary and performance checks once the compatibility and parser
   harnesses are stable.

Deferred unless a second implementation appears:

- `Codec` interface.
- `EndpointResolver` interface.
- Pluggable renderer strategy.
- Policy interface.

## Non-Goals

This review does not require:

- Reintroducing a daemon or sidecar process.
- Adding an HTTP control plane.
- Building a full Java parser immediately.
- Managing saved test cases.
- Adding assertions to MCP invoke.
- Adding `Codec`, `EndpointResolver`, `Policy`, or renderer interfaces before a
  second implementation or concrete testing need exists.

The goal is a smaller and cleaner agent runtime, not a broader product surface.
