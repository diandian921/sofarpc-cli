# MCP Layer Refactor — Implementation Plan

> **历史实施计划。** 三层重构已落地;后续以 `docs/mcp-review-followups.md` 为准(第二/三轮 review 跟进项)。本文保留 PR 分段与设计推导,供回溯,**不再是当前工作准绳**。

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

Date: 2026-05-31
Source of truth (do not re-litigate): `docs/mcp-spec-compliance-review.md`, `docs/mcp-architecture-blueprint.md` §16「已裁决」.

**Goal:** Split the single `internal/mcp` package into a three-layer `proto / server / tools` architecture, land all P0/P1 MCP-compliance fixes, delete `sofarpc invoke` from the CLI, and keep `go test ./...` green at every PR.

**Architecture:** Bottom-up. Build `internal/mcp/proto` (pure JSON-RPC + lifecycle + cancellation + progress/logging, stdlib only) first; keep `internal/mcp` as a facade so `internal/cli/mcp.go` never changes; then build `internal/mcp/server` (registry/dispatch/annotations/strict-decode, no SofaRPC); then move each tool into `internal/mcp/tools` (business only, never imports `proto`). Files move last.

**Tech Stack:** Go 1.19 (generics available), stdlib `encoding/json` (UseNumber + DisallowUnknownFields), `go list -deps` arch tests, hand-written JSON Schema (no reflection).

---

## Decisions already locked (inputs, not open questions)

1. **Path A — three packages** `proto / server / tools`. (codex twice preferred single-package B; overruled for long-term cleanliness.)
2. **`sofarpc invoke` CLI is deleted**; capability lives only as the `sofarpc_invoke` MCP tool.
3. **Refactor guardrail: freeze `internal/app` public types during the refactor.** (Originally a second guardrail — a cross-render CLI/MCP contract test — was specced; **dropped 2026-05-31**: this is a self-use tool with no external contract window, CLI and MCP share the *same* `app.Render*` functions verbatim with no independent rendering to drift, and render semantics are already covered by `internal/app/render_test.go`. The freeze test is the only anti-drift guard kept.)
4. **schema is hand-written, never reflected.** The typed registry generic decodes arguments only; each tool ships its own `inputSchema` / `outputSchema` literal.
5. **Discipline: < 1 week or roll back.** Point of no return is PR5 (old handlers deleted). Keep the whole epic on one feature branch; merge to main only after PR5 is green and validated.
6. **Dropped findings:** original N20 (config.json as MCP resource — leaks attachments/credentials) and original N21 (standalone openWorldHint — folded into the annotations matrix).

---

## Layer contract (the invariants every PR must preserve)

```
cli/mcp facade  →  tools  →  server  →  proto
                                         (stdlib only)
tools → app / schema / appconfig          (business deps)
```

| Package | Owns | MUST NOT import |
|---|---|---|
| `internal/mcp/proto` | JSON-RPC frame codec, strict decode, error codes, stdio transport (size-limited), lifecycle state machine + version negotiation, in-flight registry (composite key), cancellation semantics, progressToken parse + `notifications/progress`, `notifications/message`, client-capabilities capture | any repo package (`app`, `server`, `tools`, `cli`) |
| `internal/mcp/server` | `tools/list` + `tools/call` dispatch, `Registry`, `Tool[A,O]` + `Register`, mandatory `Annotations`, strict typed arg decode, result wrapping (`content`/`structuredContent`/`_meta`/`isError`), `outputSchema` presence, `Runtime` interface | `tools`, `app`, `schema`, `appconfig` |
| `internal/mcp/tools` | one typed tool per file + hand-written schema, calls `app.Service` / `schema` / `appconfig` | `proto` (use `server.Runtime` instead) |
| `internal/mcp` (facade) | `Server` struct + `Run()` / `SelfTest()`; wires proto+server+tools; registers all tools | — (top of the mcp tree) |

**Cycle-avoidance rule (the one that bites):** progress/logging state lives in `proto.Session`; `server` exposes a `Runtime` interface implemented over `proto.Session`; `tools` import only `server.Runtime` and **never** `proto`.

---

## Finding → PR map

| Finding (review) | Lands in |
|---|---|
| P0 initialize version negotiation | PR2 (`proto/lifecycle.go`) |
| P0 lifecycle gating (handshake before tools/*) | PR2 (`proto/lifecycle.go` + session) |
| P0 strict JSON-RPC (jsonrpc=="2.0", non-empty method, single JSON, error.data) | PR2 (`proto/jsonrpc.go`) |
| P0 oversize frame → `-32600` (not `-32700`); maxLine 128MB→16MB | PR2 (`proto/transport.go`) |
| P0 dryRun strict bool | PR2 surgical patch (facade) → subsumed by PR3 strict typed decode |
| P1 cancellation: no response after cancel + composite-key dup-ID | PR2 (`proto/cancellation.go`) |
| P1 tool annotations matrix | PR3 (mechanism) + PR4/PR5 (per-tool values) |
| P1 error contract unified to `app.Result` | PR3 (wrap helper) + PR4/PR5 (all tool paths) |
| P1 split `sofarpc_config` into 5 tools | PR4 (list) + PR5 (save/remove ×4) |
| P1 split `sofarpc_invoke_plan` (dryRun) | PR4 |
| P1 describe async + progressToken-gated progress | PR2 (plumbing) + PR4 (describe long-running) |
| P2 logging capability + serverInfo title/description + instructions | PR2 (declare) + PR7 (content) |
| P2 panic/stderr sanitization, integerSchema rename, writer goroutine, _meta, outputSchema content | PR7 |
| P2 contract test (ajv), selftest expansion, README compliance level | PR7 |
| Delete `sofarpc invoke` | PR6 |

---

## PR sequencing & rollback

| PR | Scope | Budget | Rollback-safe? |
|---|---|---|---|
| PR1 | Guardrails (app freeze, arch boundary skip-if-absent) | 0.5d | trivially (pure additions) |
| PR2 | `proto` package + all P0 + cancellation; facade wraps proto; tools stay old | 1.5d | yes (drop new pkg, restore old session) |
| PR3 | `server` package (registry/strict-decode/annotations/Runtime), mock-tool tests only | 1d | yes (unused package) |
| PR4 | `tools` wave 1 (resolve/probe/describe/doctor/config_list/invoke_plan); cut read path through registry | 1.5d | yes (old read handlers still present until removed here — keep them until green) |
| **PR5** | `tools` wave 2 (config save/remove ×4, invoke); **delete old handlers**; flip tools/list want | 1.5d | **POINT OF NO RETURN** |
| PR6 | Delete CLI `invoke`; README/docs | 0.5d | n/a |
| PR7 | P2 polish (logging/serverInfo/instructions/panic-sanitize/misc/outputSchema/selftest/README compliance) | 1d | follow-on; not on the <1wk critical path |

Critical path to a shippable cutover is **PR1–PR6** (~6.5d). PR7 is post-cutover hardening; if the week is spent, ship PR1–PR6 and schedule PR7 separately. If PR5 cannot go green within budget, **abandon the branch** — PR1 guardrails and the old single package remain intact on main.

---

# PR1 — Guardrails (no behavior change)

**Why first:** the guardrail must exist before any structural churn so drift in `app.Result` / `InvocationPlan` or a layering violation fails loudly the moment it is introduced.

**Files:**
- Create: `internal/app/apifreeze_test.go`
- Modify: `internal/arch/boundary_test.go`

### Task 1.1 — Freeze the `internal/app` public wire types

- [ ] **Step 1: Write the failing test** `internal/app/apifreeze_test.go`

```go
package app

import (
	"reflect"
	"sort"
	"testing"
)

// jsonFields returns the JSON wire names of an exported struct, skipping json:"-".
func jsonFields(t reflect.Type) []string {
	var out []string
	for i := 0; i < t.NumField(); i++ {
		tag := t.Field(i).Tag.Get("json")
		if tag == "-" {
			continue
		}
		name := tag
		if comma := indexByte(name, ','); comma >= 0 {
			name = name[:comma]
		}
		if name == "" {
			name = t.Field(i).Name
		}
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}

// TestAppPublicTypesFrozen pins the agent-facing app contract for the duration
// of the MCP refactor. Changing any of these field sets is a breaking change to
// the shared CLI/MCP render contract and must be a deliberate, separate commit.
func TestAppPublicTypesFrozen(t *testing.T) {
	want := map[string][]string{
		"Result":              {"code", "data", "error", "meta", "ok", "requestId"},
		"ResultError":         {"cause", "details", "message", "nextTool"},
		"Endpoint":            {"address", "appName", "attachments", "project", "protocol", "server", "timeoutMs"},
		"ProjectRef":          {"info", "name"},
		"MethodSignature":     {"name", "paramTypes"},
		"PlanWarning":         {"code", "details", "message"},
		"Diagnostics":         {"resolution", "timing", "warnings"},
		"ResolveResult":       {"diagnostics", "endpoint", "network", "project", "server", "servers"},
		"InvocationPlan":      {"arguments", "diagnostics", "endpoint", "method", "project", "rawResult", "server", "service", "timeoutMs", "warnings"},
		"InvocationExecution": {"code", "data", "error", "meta", "ok"},
		"ExecutionError":      {"cause", "details", "message"},
		"ProbeResult":         {"address", "diagnostics", "elapsedMs", "error", "meta", "project", "reachable", "server", "service", "timeoutMs"},
	}
	got := map[string][]string{
		"Result":              jsonFields(reflect.TypeOf(Result{})),
		"ResultError":         jsonFields(reflect.TypeOf(ResultError{})),
		"Endpoint":            jsonFields(reflect.TypeOf(Endpoint{})),
		"ProjectRef":          jsonFields(reflect.TypeOf(ProjectRef{})),
		"MethodSignature":     jsonFields(reflect.TypeOf(MethodSignature{})),
		"PlanWarning":         jsonFields(reflect.TypeOf(PlanWarning{})),
		"Diagnostics":         jsonFields(reflect.TypeOf(Diagnostics{})),
		"ResolveResult":       jsonFields(reflect.TypeOf(ResolveResult{})),
		"InvocationPlan":      jsonFields(reflect.TypeOf(InvocationPlan{})),
		"InvocationExecution": jsonFields(reflect.TypeOf(InvocationExecution{})),
		"ExecutionError":      jsonFields(reflect.TypeOf(ExecutionError{})),
		"ProbeResult":         jsonFields(reflect.TypeOf(ProbeResult{})),
	}
	for name, w := range want {
		if !reflect.DeepEqual(got[name], w) {
			t.Fatalf("app.%s wire fields drifted:\n got  %v\n want %v", name, got[name], w)
		}
	}
}

// Compile-time freeze of the app.Service method surface MCP/CLI consume. A
// signature change breaks this var block before it breaks callers.
var _ = []interface{}{
	(*Service).PlanInvocation,
	(*Service).ExecuteInvocation,
	(*Service).Resolve,
	(*Service).ProbeEndpoint,
	New,
	RenderExecution,
	RenderProbe,
	RenderFailure,
	NewRequestID,
	DomainErrorDetails,
}
```

- [ ] **Step 2: Run it — expect PASS** (golden matches current code).

Run: `go test ./internal/app/ -run TestAppPublicTypesFrozen -v`
Expected: PASS. (If it fails, the golden above is wrong vs current source — fix the golden, not the structs.)

- [ ] **Step 3: Commit** — `chore: 冻结 app 公共类型供 MCP 重构期防漂移`

> Render *semantics* (code/nextTool/ok mapping) are already covered by `internal/app/render_test.go`; the freeze test above covers the field-set. No separate cross-render contract test is needed (see "Decisions already locked" #3).

### Task 1.2 — Arch boundary rules for the future packages (skip-if-absent)

- [ ] **Step 1: Add a skip-if-absent helper + new rules** to `internal/arch/boundary_test.go`. The packages don't exist yet, so each rule self-skips until its package compiles — committing the contract now (per §16 "先加边界测试") so it activates automatically as packages land.

```go
// packageExists reports whether go list can load pkg (false before the package
// is created, so layering rules can be committed ahead of the packages).
func packageExists(pkg string) bool {
	cmd := exec.Command("go", "list", pkg)
	cmd.Dir = filepath.Join("..", "..")
	return cmd.Run() == nil
}

func TestMCPLayerBoundaries(t *testing.T) {
	rules := []struct {
		pkg       string
		forbidden []string
	}{
		{modulePath + "/internal/mcp/proto", []string{
			modulePath + "/internal/app", modulePath + "/internal/mcp/server",
			modulePath + "/internal/mcp/tools", modulePath + "/internal/cli",
			modulePath + "/internal/schema", modulePath + "/internal/appconfig",
			modulePath + "/internal/direct",
		}},
		{modulePath + "/internal/mcp/server", []string{
			modulePath + "/internal/mcp/tools", modulePath + "/internal/app",
		}},
		{modulePath + "/internal/mcp/tools", []string{
			modulePath + "/internal/mcp/proto",
		}},
	}
	for _, rule := range rules {
		t.Run(shortPackageName(rule.pkg), func(t *testing.T) {
			if !packageExists(rule.pkg) {
				t.Skipf("%s not created yet", rule.pkg)
			}
			deps := packageDeps(t, rule.pkg)
			for _, forbidden := range rule.forbidden {
				for dep := range deps {
					if samePackageOrChild(dep, forbidden) {
						t.Fatalf("%s must not depend on %s; found %s", rule.pkg, forbidden, dep)
					}
				}
			}
		})
	}
}
```

- [ ] **Step 2: Run — expect PASS (all skipped).** Run: `go test ./internal/arch/ -run TestMCPLayerBoundaries -v`
- [ ] **Step 3: Commit** — `test: 加 mcp 三层 import 边界(包未建时自动 skip)`
- [ ] **Step 4: Full suite green.** Run: `go test ./...`

---

# PR2 — `internal/mcp/proto` + all P0 + cancellation

**Why this is the P0 vehicle:** lifecycle gating, version negotiation, strict framing, and cancellation are all protocol concerns. Building `proto` first and routing the facade's read loop through it makes the shipped binary compliant immediately, while the old tool handlers stay untouched behind the facade. Each is a tripwire test that was previously pinning the *wrong* behavior — flip the test to assert the target, watch it fail, then implement.

**New package `internal/mcp/proto`:**
- `jsonrpc.go` — `Message`/`Request`/`Response`/`Notification` types; `Decode([]byte) (Request, error)` enforcing `jsonrpc=="2.0"`, non-empty `method`, single top-level JSON; error codes `ParseError(-32700) / InvalidRequest(-32600) / MethodNotFound(-32601) / InvalidParams(-32602) / InternalError(-32603) / ServerNotInitialized(-32002) / ShuttingDown(-32000)`; `Error{Code,Message,Data}` (Data optional).
- `transport.go` — `Transport.ReadFrame() ([]byte, error)` (line-delimited, `maxFrameBytes = 16<<20`, oversize → `ErrFrameTooLong`) and `Transport.WriteFrame(Message)`; serialized via a single writer goroutine + outbox channel so a slow write never blocks the read loop.
- `lifecycle.go` — `Session.State` (`StateNew → StateInitializing → StateReady → StateClosing`); `NegotiateVersion(client string) (string, *Error)`; `supportedVersions = []string{"2025-11-25","2025-06-18","2025-03-26","2024-11-05"}`; injected `ServerInfo` / `ServerCapabilities` / `Instructions` (plain proto structs the facade fills) used to assemble the `initialize` result.
- `cancellation.go` — `InFlight` registry keyed by `(idKey, method)`; `Register`, `CompleteIfLive`, `Cancel`; cancel marks the entry so the eventual response is **dropped**.
- `progress.go` — parse `params._meta.progressToken`; `Session.Progress(token, msg, percent)` emits `notifications/progress` only when a token was supplied.
- `logging.go` — `Session.Log(level, msg)` → `notifications/message`.
- `capabilities.go` — `ServerCapabilities{Tools: {ListChanged:false}, Logging: {}}` plain struct.

**Facade changes (`internal/mcp`, API unchanged):**
- `session.go` read loop delegates framing/decoding/lifecycle/cancellation to `proto`; calls the existing `s.handle` only after gating passes.
- `Server.Run()` / `Server.SelfTest()` signatures and `Server` fields **unchanged** (cli/mcp.go untouched).

### Task 2.1 — Strict JSON-RPC decode + oversize framing

- [ ] **Step 1: Write proto tests** `internal/mcp/proto/jsonrpc_test.go` + `transport_test.go`:
  - `jsonrpc != "2.0"` → `InvalidRequest(-32600)`
  - empty/missing `method` on a request → `InvalidRequest(-32600)`
  - trailing garbage after the JSON object → `ParseError(-32700)`
  - large-int argument preserved (`UseNumber`) — port `TestDecodeJSONPreservesLargeNumbers`
  - frame > 16MB → `ErrFrameTooLong`, and the reader resyncs to the next line — port `TestReadLineLimitedRejectsAndResyncs`
- [ ] **Step 2: Run — expect FAIL** (package not implemented). Run: `go test ./internal/mcp/proto/`
- [ ] **Step 3: Implement** `jsonrpc.go` + `transport.go` per the spec above.
- [ ] **Step 4: Run — expect PASS.**
- [ ] **Step 5: Commit** — `feat(proto): strict JSON-RPC decode + 16MB 帧上限`

### Task 2.2 — Lifecycle gating + version negotiation (flip the pinned echo test)

- [ ] **Step 1: Flip the pinned test.** In `internal/mcp/server_test.go` replace `TestInitializeEchoesClientProtocolVersion` with `TestInitializeNegotiatesProtocolVersion` covering three cases:

```go
func TestInitializeNegotiatesProtocolVersion(t *testing.T) {
	cases := []struct{ name, sent, want string; wantErr bool }{
		{"supported", `"2025-06-18"`, "2025-06-18", false},
		{"unsupported-degrades-to-latest", `"1.0.0"`, "2025-11-25", false},
		{"missing-is-invalid-params", ``, "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			params := `{}`
			if c.sent != "" {
				params = `{"protocolVersion":` + c.sent + `}`
			}
			out := &bytes.Buffer{}
			in := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":` + params + `}` + "\n"
			s := &Server{BuildVersion: "test", Stdin: strings.NewReader(in), Stdout: out, Stderr: &bytes.Buffer{}}
			if code := s.Run(); code != 0 {
				t.Fatalf("Run exit = %d", code)
			}
			if c.wantErr {
				if !strings.Contains(out.String(), `"code":-32602`) {
					t.Fatalf("missing protocolVersion must be -32602: %s", out.String())
				}
				return
			}
			if !strings.Contains(out.String(), `"protocolVersion":"`+c.want+`"`) {
				t.Fatalf("negotiated version wrong: %s", out.String())
			}
		})
	}
}
```

- [ ] **Step 2: Add a gating test** `TestToolsCallBeforeInitializeIsRejected`: a `tools/list` with no prior `initialize` → `-32002` ("server not initialized").
- [ ] **Step 3: Add a handshake helper** to `server_test.go` and update every test that currently skips the handshake (they will start failing with `-32002`):

```go
func handshake() string {
	return `{"jsonrpc":"2.0","id":0,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}` + "\n" +
		`{"jsonrpc":"2.0","method":"notifications/initialized"}` + "\n"
}
```
Prepend `handshake()` to the stdin of: `TestToolsListRegistersWorkflowTools`, `TestConfigWriteCanBeDisabled`, `TestConfigSaveAndListProjectTool`, `TestResolveAndInvokeDryRunUseWorkflowTools`, `TestRunAsyncInvokeCanBeCancelledWhileToolsListResponds`. Account for the extra initialize response line when indexing `lines[...]`.

- [ ] **Step 4: Run — expect FAIL** (echo + no-gating still in place).
- [ ] **Step 5: Implement** `proto/lifecycle.go` negotiation + gating in the facade read loop (gate in `session.dispatch` before `handle`; `initialize`/`notifications/initialized` drive the state machine; `initialize` response assembled from injected `ServerInfo`/`Caps`). Keep `SelfTest()` green by having it drive `initialize`→`notifications/initialized`→`tools/list` through the gated path (or by exercising the session, not raw `handle`).
- [ ] **Step 6: Run — expect PASS.**
- [ ] **Step 7: Commit** — `feat(proto): 协议版本协商 + lifecycle gating(P0)`

### Task 2.3 — Cancellation: no response after cancel + composite-key dup-ID (flip the pinned cancel test)

- [ ] **Step 1: Flip the pinned test.** Rename `TestRunAsyncInvokeCanBeCancelledWhileToolsListResponds` → `TestCancelledInvokeSendsNoFinalResponse`. Keep the setup (hanging TCP server, async invoke, concurrent tools/list). Change the assertion: after sending `notifications/cancelled` for `invoke-1`, assert **no** frame with `id=="invoke-1"` is ever written (drain `stdout.ch` until stdin close with a short timeout, fail if `invoke-1` appears), while `list-while-invoke` still gets a result.
- [ ] **Step 2: Add** `TestDuplicateRequestIDDoesNotClobberInFlight` — two concurrent async calls reusing `id:"dup"` with different methods both register and cancel independently (composite key `(id, method)`).
- [ ] **Step 3: Run — expect FAIL** (current code sends the "context canceled" response).
- [ ] **Step 4: Implement** `proto/cancellation.go`: composite-key registry; on cancel mark the entry; the dispatch goroutine checks the live flag right before writing and **drops** the response if cancelled; also fix `errJSONRPCLineTooLong` mapping from `-32700` to `-32600` (was review N10).
- [ ] **Step 5: Run — expect PASS.**
- [ ] **Step 6: Commit** — `feat(proto): cancel 后不发响应 + 复合 key 防 dup-ID(P1)`

### Task 2.4 — Surgical P0: reject non-bool `dryRun` (pre-split safety)

- [ ] **Step 1: Write** `TestInvokeRejectsNonBoolDryRun` in `server_test.go`: `tools/call sofarpc_invoke {... "dryRun":"true"}` → an error result (not a silent real RPC). Assert the rendered `app.Result` is `ok:false` / `BAD_REQUEST` (or a `-32602`), **not** a network attempt.
- [ ] **Step 2: Run — expect FAIL** (`boolArg` coerces `"true"`→false silently → would attempt a real invoke).
- [ ] **Step 3: Implement** a strict bool read in `invocationInput`: if `dryRun` is present and not a JSON bool, return a bad-arguments error. (This is superseded by `server` strict typed decode in PR3, but ships the P0 safety fix now.)
- [ ] **Step 4: Run — expect PASS.**
- [ ] **Step 5: Full suite green.** Run: `go test ./...`
- [ ] **Step 6: Commit** — `fix(mcp): dryRun 严格 bool,拒绝字符串静默降级(P0 安全)`

> After PR2: the shipped binary negotiates versions, enforces the handshake, frames strictly, honors cancellation, and is dryRun-safe. Tools still live in the old package behind the facade.

---

# PR3 — `internal/mcp/server` (registry, strict decode, annotations, Runtime)

**Why now:** the middle layer is built and unit-tested against **mock tools only** — zero SofaRPC coupling — so the registry/dispatch/annotations/strict-decode contracts are locked before any real tool moves. Not yet wired into the facade (still using the old `handle`), so the package is independently green and rollback-safe.

**New package `internal/mcp/server`:**
- `tool.go`
```go
type Runtime interface {
	Progress(ctx context.Context, msg string, percent *float64)
	Log(ctx context.Context, level, msg string)
}

type Annotations struct { // all fields mandatory; constructed explicitly per tool
	ReadOnlyHint, DestructiveHint, IdempotentHint, OpenWorldHint bool
}

type ToolSpec struct {
	Name, Title, Description string
	Annotations             Annotations
	InputSchema             json.RawMessage // hand-written literal
	OutputSchema            json.RawMessage // hand-written literal ("" allowed; presence checked for resolve/invoke/doctor)
	Async                   bool            // run on goroutine + cancel + progress
}

type Tool[A any, O any] struct {
	Spec ToolSpec
	Run  func(ctx context.Context, rt Runtime, args A) (O, error)
}
```
- `registry.go` — `Registry` holding `wrappedTool{ spec; invoke func(ctx, Runtime, json.RawMessage) (any, error) }`; `func Register[A,O any](r *Registry, t Tool[A,O])` builds the closure that strict-decodes `raw → A` then calls `t.Run`; `List()` emits the `tools/list` array (`name/title/description/inputSchema/outputSchema/annotations`).
- `strict_decode.go` — `decodeArgs[A](raw json.RawMessage) (A, *proto.Error)` using `json.Decoder` with `UseNumber()` + `DisallowUnknownFields()`; a `"true"` string into a `bool` field → `InvalidParams(-32602)`. Aliases (`types`, `args`) are real struct fields (so they decode) but omitted from `InputSchema` (review N7: silent server-side compat, not advertised).
- `result.go` / `meta.go` — wrap a tool's `app.Result` (or any `O`) into `{content:[{type:text,text:summary}], structuredContent, _meta:{requestId,elapsedMs}, isError:!ok}`; business errors stay inside `structuredContent.ok=false` and never become JSON-RPC errors (blueprint §9).
- `dispatch.go` — `tools/call` → lookup → strict decode → (async? goroutine+cancel+progress via injected `proto` Runtime : inline) → wrap.

`server` imports `proto` only. The `Runtime` is implemented in the facade over `proto.Session` and injected.

### Task 3.1 — Registry + strict typed decode

- [ ] **Step 1: Write** `internal/mcp/server/registry_test.go` with a mock tool:

```go
type echoArgs struct {
	Name   string `json:"name"`
	DryRun bool   `json:"dryRun,omitempty"`
}
type echoOut struct{ Echo string `json:"echo"` }

func newEchoTool() Tool[echoArgs, echoOut] {
	return Tool[echoArgs, echoOut]{
		Spec: ToolSpec{
			Name: "mock_echo", Title: "Echo", Description: "test",
			Annotations: Annotations{ReadOnlyHint: true, IdempotentHint: true},
			InputSchema: json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"}},"required":["name"],"additionalProperties":false}`),
		},
		Run: func(_ context.Context, _ Runtime, a echoArgs) (echoOut, error) { return echoOut{Echo: a.Name}, nil },
	}
}
```
Assert: (a) `tools/list` includes `mock_echo` with its annotations and inputSchema; (b) dispatch with `{"name":"x"}` returns `structuredContent.echo=="x"`; (c) unknown field `{"name":"x","bogus":1}` → `-32602`; (d) `{"name":"x","dryRun":"true"}` → `-32602` (strict bool).

- [ ] **Step 2: Run — expect FAIL.** Run: `go test ./internal/mcp/server/`
- [ ] **Step 3: Implement** `tool.go`, `registry.go`, `strict_decode.go`, `result.go`, `dispatch.go`.
- [ ] **Step 4: Run — expect PASS.**
- [ ] **Step 5: Commit** — `feat(server): typed registry + strict decode + 结果包装`

### Task 3.2 — Annotations are mandatory + outputSchema presence

- [ ] **Step 1: Write** `internal/mcp/server/annotations_test.go`: registering a tool whose `ToolSpec.Name` is in a "must have outputSchema" set (resolve/invoke/doctor) but with empty `OutputSchema` fails a `Registry.Validate()` call; `tools/list` always emits an `annotations` object (never omitted). Add `TestRuntimeInterfaceSatisfiedByProtoSession` (compile-time `var _ Runtime = (*proto.Session)(nil)` if Session implements it, else a thin adapter).
- [ ] **Step 2: Run — expect FAIL.**
- [ ] **Step 3: Implement** `Registry.Validate()` + always-emit annotations.
- [ ] **Step 4: Run — expect PASS.**
- [ ] **Step 5: Full suite green.** Run: `go test ./...`  (arch `TestMCPLayerBoundaries/server` now active — confirm it passes: `server` imports `proto` only.)
- [ ] **Step 6: Commit** — `feat(server): 强制 annotations + outputSchema 存在性校验`

---

# PR4 — `internal/mcp/tools` wave 1 (read tools) + cut the read path

**Why this split:** read-only tools are lower risk (no config mutation, no real RPC except probe's TCP dial) and let the registry-driven dispatch prove out before the destructive write tools and invoke move. After this PR the read path runs entirely through `server.Registry`; the old read handlers in `server.go` are deleted **in this PR** once their tests are green.

**New files** (one tool per file, each: typed Args/Out struct, hand-written `inputSchema`/`outputSchema`, `Annotations` from the matrix, `Run` calling `app`/`schema`/`appconfig`):
`tools/resolve.go`, `tools/probe.go`, `tools/describe.go`, `tools/doctor.go`, `tools/config_list.go`, `tools/invoke_plan.go`.

**Annotations matrix (locked — from review N2 + blueprint §8):**

| Tool | readOnly | destructive | idempotent | openWorld | Async |
|---|---|---|---|---|---|
| sofarpc_resolve | ✓ | — | ✓ | — | — |
| sofarpc_probe | ✓ | — | ✓ | ✓ | ✓ |
| sofarpc_describe | ✓ | — | ✓ | — | ✓ |
| sofarpc_doctor | ✓ | — | ✓ | — | ✓ |
| sofarpc_invoke_plan | ✓ | — | ✓ | — | ✓ |

### Task 4.1 — `resolve` tool as the template (do this one fully, then replicate)

**Files:** Create `internal/mcp/tools/resolve.go`, `internal/mcp/tools/resolve_test.go`.

- [ ] **Step 1: Write** `resolve_test.go` — `Run` against a temp-HOME config with one project+server returns an `app.Result`-shaped output with `endpoint`. Direct `Run(ctx, nopRuntime{}, ResolveArgs{Server:"user-test"})`, no JSON-RPC.
- [ ] **Step 2: Run — expect FAIL.**
- [ ] **Step 3: Implement** `resolve.go`:

```go
package tools

type ResolveArgs struct {
	Project   string `json:"project,omitempty"`
	Server    string `json:"server,omitempty"`
	TimeoutMS int    `json:"timeoutMs,omitempty"`
}

func ResolveTool(appSvc *app.Service) server.Tool[ResolveArgs, app.Result] {
	return server.Tool[ResolveArgs, app.Result]{
		Spec: server.ToolSpec{
			Name: "sofarpc_resolve", Title: "SofaRPC Resolve",
			Description: "Resolve the configured project, server, and endpoint without touching the network.",
			Annotations: server.Annotations{ReadOnlyHint: true, IdempotentHint: true},
			InputSchema:  resolveInputSchema,  // hand-written literal in this file
			OutputSchema: resolveOutputSchema, // hand-written literal in this file
		},
		Run: func(ctx context.Context, _ server.Runtime, a ResolveArgs) (app.Result, error) {
			resolved, err := appSvc.Resolve(ctx, app.ResolveInput{Project: a.Project, Server: a.Server, TimeoutMS: a.TimeoutMS})
			if err != nil {
				return app.RenderFailure(app.CodeBadRequest, err.Error(), app.DomainErrorDetails(err)), nil
			}
			return renderResolve(resolved), nil // builds app.Result via json.Marshal of the resolved payload
		},
	}
}
```
(Move `resolveInputSchema`/`resolveOutputSchema` literals — adapted from the current `tools()` map — into this file. The current `resolve` handler returns a bespoke map; wrap it into `app.Result` so the error contract is unified: success → `Result{OK:true, Code:SUCCESS, Data: <marshaled payload>}`.)

- [ ] **Step 4: Run — expect PASS.**
- [ ] **Step 5: Commit** — `feat(tools): resolve 工具(template) + app.Result 统一`

### Task 4.2 — Replicate for probe / describe / doctor / config_list / invoke_plan

For each, follow the 4.1 pattern. Per-tool specifics (everything the engineer needs; no "similar to"):

- [ ] **`tools/probe.go`** — `ProbeArgs{Server, Address, Service, TimeoutMS}`; `Async:true`, `OpenWorldHint:true`; `Run` calls `appSvc.ProbeEndpoint` then `app.RenderProbe`, set `RequestID`. Test: dial a refused address → `ok:false, code:CONNECT_FAILED, nextTool:sofarpc_probe`.
- [ ] **`tools/describe.go`** — `DescribeArgs{Project, Server, Query, Service, Method, Limit, IncludeOutOfPrefix}`; `Async:true`. `Run`: resolve project (port `resolveProject` logic into a `tools` helper), `schema.LoadOrBuildIndex`, search/describe, wrap into `app.Result`. Emit `rt.Progress(ctx,"building source index",nil)` before `LoadOrBuildIndex` (gated by token). Test: search mode returns candidates; describe mode returns methods. (This realizes review N15: describe runs async so the read loop is never blocked.)
- [ ] **`tools/doctor.go`** — `DoctorArgs{Project, Server, Service, Method}`; `Async:true`. Port the `checks[]` builder; wrap as `app.Result{OK: allChecksOK, Code: SUCCESS|INTERNAL_ERROR, Data: {checks}}`.
- [ ] **`tools/config_list.go`** — `ConfigListArgs{Project}`; readOnly. Port `listConfig`; wrap into `app.Result`. (The four write actions become separate tools in PR5.)
- [ ] **`tools/invoke_plan.go`** — `InvokePlanArgs` = the invoke args **without** `dryRun`; `ReadOnlyHint:true, IdempotentHint:true, Async:true`. `Run` calls `appSvc.PlanInvocation`; on error `app.RenderFailure(CodeBadRequest, err, DomainErrorDetails(err))`; on success `Result{OK:true, Code:SUCCESS, Data: plan.Display()}`. (This is the dryRun path, split out per review N8/P1#6.)

Each tool: write `Run` test first (FAIL), implement (PASS), commit.

### Task 4.3 — Wire the read path through the registry; delete old read handlers

**Files:** Modify `internal/mcp/server.go` (facade), `internal/mcp/session.go` (facade), `internal/mcp/server_test.go`.

- [ ] **Step 1: Build the registry in the facade.** In `internal/mcp`, construct a `server.Registry`, `server.Register` the six wave-1 tools, implement `server.Runtime` over `proto.Session`, and route `tools/list` + `tools/call` (for these tools) through `Registry`. Keep the old `handleToolCall` switch **only** for the not-yet-migrated names (`sofarpc_config` save/remove, `sofarpc_invoke`) during this PR.
- [ ] **Step 2: Update tests** that referenced the old tool surface:
  - `TestToolsListRegistersWorkflowTools` — temporary want-list = old write/invoke names **plus** `sofarpc_config_list`, `sofarpc_invoke_plan`, minus `sofarpc_resolve/probe/describe/doctor` duplication (they keep their names). (Final want-list lands in PR5; here just keep it passing.)
  - `TestResolveAndInvokeDryRunUseWorkflowTools` — split: `sofarpc_resolve` unchanged; the dryRun call becomes `sofarpc_invoke_plan` (no `dryRun` arg). Update `shouldRunAsync` → async classification now comes from `ToolSpec.Async`.
- [ ] **Step 3: Delete** the migrated handlers from `server.go` (`resolve`, `probe`, `describe`, `doctor`, `listConfig`) and the now-dead `shouldRunAsync` invoke/probe special-casing.
- [ ] **Step 4: Run — expect PASS.** Run: `go test ./...` (arch `tools` rule now active — confirm `tools` does not import `proto`).
- [ ] **Step 5: Commit** — `refactor(mcp): 读路径切到 registry + 删旧读 handler`

---

# PR5 — `internal/mcp/tools` wave 2 (config writes + invoke) + complete cutover  ⚠️ point of no return

**New files:** `tools/config_save_project.go`, `tools/config_save_server.go`, `tools/config_remove_project.go`, `tools/config_remove_server.go`, `tools/invoke.go`.

**Annotations (locked):**

| Tool | readOnly | destructive | idempotent | openWorld |
|---|---|---|---|---|
| sofarpc_config_save_project | — | — | — | — |
| sofarpc_config_save_server | — | — | — | — |
| sofarpc_config_remove_project | — | ✓ | — | — |
| sofarpc_config_remove_server | — | ✓ | — | — |
| sofarpc_invoke | — | ✓ | — | ✓ |

### Task 5.1 — Config write tools (split the fat tool)

- [ ] For each of the four, write `Run` test (FAIL) → implement (PASS) → commit. Port `saveProject`/`saveServer`/`removeProject`/`removeServer` bodies; each gets a **focused** `inputSchema` — e.g. `config_remove_project` makes `name` required and documents `confirm` required-for-effect; `confirm`/`cascade` are real bool fields (strict-decoded). All wrap results into `app.Result`. Honor `DisableConfigWrite` by **not registering** these four when the flag is set (review N3: write tools vanish from `tools/list` instead of erroring).
- [ ] Test `TestDisableConfigWriteHidesWriteTools`: with `DisableConfigWrite:true`, `tools/list` contains the read set + `sofarpc_invoke`/`sofarpc_invoke_plan` but **none** of the four write tools.

### Task 5.2 — `invoke` tool (destructive) + preserve nextTool contract

- [ ] **Step 1: Write** `tools/invoke_test.go` porting `TestInvokePlanningFailureCarriesNextTool` semantics: planning failure → `app.Result{ok:false, code:BAD_REQUEST, error.nextTool!=""}`.
- [ ] **Step 2: Implement** `tools/invoke.go` — `InvokeArgs` (service/method/server/project/paramTypes(+`types` alias field, unadvertised)/orderedArguments(+`args` alias)/arguments/timeoutMs/rawResult; **no dryRun**); `Annotations{DestructiveHint:true, OpenWorldHint:true}`, `Async:true`. `Run`: `PlanInvocation` → on err `RenderFailure(...)`; else `RenderExecution(ExecuteInvocation(...))`, set `RequestID`, emit `rt.Progress` at "plan resolved"/"TCP connected"/"hessian decoded".
- [ ] **Step 3: Run — expect PASS.**
- [ ] **Step 4: Commit** — `feat(tools): invoke 工具(destructive) + 保留 nextTool`

### Task 5.3 — Final cutover: delete the old single-package handlers, flip the tools/list want-list

- [ ] **Step 1: Flip the canonical test.** `TestToolsListRegistersWorkflowTools` final want-list (sorted):
```
sofarpc_config_list, sofarpc_config_remove_project, sofarpc_config_remove_server,
sofarpc_config_save_project, sofarpc_config_save_server, sofarpc_describe,
sofarpc_doctor, sofarpc_invoke, sofarpc_invoke_plan, sofarpc_probe, sofarpc_resolve
```
- [ ] **Step 2: Route all `tools/call` through `Registry`;** delete the entire old `handleToolCall` switch, `config`/`saveProject`/`saveServer`/`removeProject`/`removeServer`/`invoke`/`invocationInput`, and now-dead helpers in `server.go`. Move surviving helpers (`resolveProject`/`resolveServer`/`endpointData`/`publicMethods`/`publicDescription`) into `tools`. Delete `args.go`/`tool_schema.go`/`config_helpers.go` from the facade if fully absorbed (or relocate into `tools`).
- [ ] **Step 3: Update** `TestConfigSaveAndListProjectTool`, `TestConfigWriteCanBeDisabled`, `TestResolveAndInvokeDryRunUseWorkflowTools`, `TestCancelledInvokeSendsNoFinalResponse` to the new tool names (`sofarpc_config_save_project`, `sofarpc_config_list`, etc.). Move tool-level assertions that no longer need JSON-RPC into `internal/mcp/tools`.
- [ ] **Step 4: Run — expect PASS.** Run: `go test ./...` (all three arch `TestMCPLayerBoundaries` subtests now active and green).
- [ ] **Step 5: Commit** — `refactor(mcp): 完成三层切换,删单包旧 handler`

> After PR5: `internal/mcp` is a thin facade; all business is in `tools`, all protocol in `proto`, all dispatch in `server`. Old single-package logic is gone — no cheap rollback past here.

---

# PR6 — Delete `sofarpc invoke` from the CLI

**Files:**
- Modify: `internal/cli/cli.go` (drop `case "invoke"`, drop the help line)
- Delete: `internal/cli/invoke.go`
- Modify: `internal/cli/subcommands_test.go` (delete 3 tests + `strconv` import)
- Modify: `README.md`, `docs/single-binary-install-target.md`, `docs/architecture-abstraction-review.md`, `docs/pure-go-runtime.md`

### Task 6.1 — Remove the subcommand

- [ ] **Step 1:** Delete `internal/cli/invoke.go`; remove `case "invoke": return runInvoke(...)` from `cli.go:29-30` and the `sofarpc invoke [flags]` usage line in `printUsage`.
- [ ] **Step 2:** From `internal/cli/subcommands_test.go` delete `TestInvokeSubcommandReturnsDirectFailureResult`, `TestInvokeRejectsArgTypeMismatch`, `TestBuildInvokeInputPreservesLargeJSONNumbers`, and remove the now-unused `strconv` import.
- [ ] **Step 3: Run — expect PASS** and verify residue is gone:
```bash
rg -n 'runInvoke|buildInvokeInput|executeInvoke|applyAssertions|assertions-json|sofarpc invoke' internal cmd README.md docs
go list -deps ./internal/app ./internal/mcp | rg '/internal/cli'   # must be empty
go test ./...
```
- [ ] **Step 4:** Update prose in `README.md` (remove the `sofarpc invoke` example + `--assertions-json` note), `docs/single-binary-install-target.md`, `docs/architecture-abstraction-review.md`, `docs/pure-go-runtime.md` to drop the CLI invoke / assertions reproduction path; note assertions are out of scope for this epic.
- [ ] **Step 5: Commit** — `refactor(cli): 删除 sofarpc invoke,能力转 sofarpc_invoke 工具`

---

# PR7 — P2 polish (post-cutover hardening; not on the <1wk critical path)

**Files:** `internal/mcp/proto/*` (logging/serverInfo), `internal/mcp` facade (ServerInfo/Instructions injection, panic), tool schema literals, `internal/mcp/server.go` selftest, `README.md`.

- [ ] **Task 7.1 — logging + serverInfo + instructions.** Declare `capabilities.logging` and serve `notifications/message`; add `serverInfo.title="SofaRPC Direct Invoker"` + `description`; set `instructions` ("Run sofarpc_resolve before sofarpc_invoke. When multiple servers exist, always pass `server`. Use sofarpc_describe query=... to find a service FQN first."). Test: `initialize` result contains title/instructions; a long-running tool emits at least one `notifications/message` when a logger is attached.
- [ ] **Task 7.2 — panic + stderr sanitization (review N16/§13).** Replace `fmt.Sprintf("internal error: %v", recovered)` with a fixed `"internal error"` JSON-RPC message; write the panic + stack + an incrementing `errorId` to stderr only; surface `errorId` in `_meta`. Flip `TestHandleWithRecoverReturnsInternalError` to assert the message is exactly `"internal error"` and does **not** contain `"boom"`. Ensure stderr read-errors never include local paths.
- [ ] **Task 7.3 — misc.** Rename `numberSchema`→`integerSchema` (review N18); add `"default":"list"` to any remaining enum (review N19); confirm `maxFrameBytes=16MB` (done in PR2); ensure the writer goroutine + outbox (done in PR2) is the only stdout writer (review: outMu blocking). Move `requestId`/`elapsedMs` into `_meta` (review N14) — verify CLI consumers of `app.Result.RequestID` are unaffected (the field stays on `app.Result`; `_meta` is the MCP-envelope copy only).
- [ ] **Task 7.4 — outputSchema content (review N5).** Add real `outputSchema` literals to `sofarpc_resolve`, `sofarpc_invoke`, `sofarpc_doctor`; `Registry.Validate()` (PR3) already requires their presence.
- [ ] **Task 7.5 — selftest + README compliance (review N23/N24).** Extend `SelfTest()` to run `initialize → notifications/initialized → tools/list → tools/call sofarpc_config_list` (4 steps). Add a "MCP compliance level" section to `README.md`: supported protocol versions `["2025-11-25","2025-06-18","2025-03-26","2024-11-05"]`; declared capabilities `tools`, `logging`; explicitly **unsupported** `resources` / `prompts` / `roots` / `sampling` / `elicitation`.
- [ ] **Task 7.6 — optional contract test (review N22).** If CI has node/ajv available, add a test that validates `tools/list` output against the official MCP JSON Schema; otherwise document it as a follow-up. Not a blocker.
- [ ] **Step: Full suite green.** Run: `go test ./...`
- [ ] **Commit per task.**

---

## Self-Review (spec coverage check)

- **Three-package split** → PR2 (proto) / PR3 (server) / PR4–PR5 (tools); cycle-avoidance via `server.Runtime`; arch tests in PR1 enforce it. ✓
- **P0** version negotiation (2.2), lifecycle gating (2.2), strict JSON-RPC (2.1), dryRun strict (2.4 then 3.1). ✓
- **P1** cancellation/dup-ID (2.3), annotations (3.2 + 4/5 matrices), unified `app.Result` (3.x + every tool), config split (4.2/5.1), invoke_plan split (4.2), describe async + progress (2.x plumbing + 4.2). ✓
- **P2** logging/serverInfo/instructions (7.1), panic/stderr sanitize (7.2), misc rename/default/maxLine/writer/_meta (7.3), outputSchema (7.4), selftest/README (7.5), ajv (7.6). ✓
- **invoke deletion** → PR6 with the exact §16 checklist + residue greps. ✓
- **Guardrail** → app freeze (1.1), boundary tests (1.2). Cross-render contract test dropped (self-use tool, shared `app.Render*`, semantics already in `render_test.go`). ✓
- **Dropped** N20/N21 — not implemented, by decision. ✓
- **Every PR ends on `go test ./...` green**, pinned tests flipped in the same PR that implements the target behavior (2.2 echo→negotiate, 2.3 cancel, 2.1 oversize/resync, 5.3 tools/list want, 7.2 panic message). ✓

## Execution handoff

Two execution options once approved:
1. **Subagent-Driven (recommended)** — fresh subagent per task, review between tasks (REQUIRED SUB-SKILL: superpowers:subagent-driven-development).
2. **Inline** — batch execution with checkpoints (REQUIRED SUB-SKILL: superpowers:executing-plans).

Branch strategy: one feature branch for the whole epic; sub-PRs are commits/stacked PRs; merge to main only after PR5 (cutover) is green and validated, to honor "< 1 week or roll back".
