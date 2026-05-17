# MCP-First SofaRPC Implementation Plan

Status: Draft  
Date: 2026-05-17

## Goal

Turn `docs/mcp-first-sofarpc-agent-design.md` into an executable implementation sequence.

This plan is intentionally evolutionary. The repository already has a Go thin client, Java daemon, launcher, IPC framing, fixtures, and tests. The plan first hardens the existing runtime, then reshapes it into the MCP-first product.

## Principles

- Reuse existing launcher, framing, server, lifecycle, and contract-test structure.
- Do not start with a greenfield rewrite.
- Keep each slice shippable and testable.
- Stabilize Engine lifecycle and SofaRPC consumer reuse before adding schema discovery.
- Keep MCP tools small and structured; do not add case/suite/report/assertion features.
- Treat `config.json` as a user-editable stable contract.

## Current Baseline

Already present:

- Go client under `cli/`.
- Java daemon under `daemon/`.
- Length-prefixed TCP framing.
- Launcher and daemon state/lock handling.
- `invoke`, `ping`, `health`, `shutdown`-style operations.
- Protocol fixtures and contract tests.
- Initial consumer-cache hardening work:
  - bounded cache,
  - timeout removed from cache key,
  - consumer lease model,
  - per-call timeout through `RpcInvokeContext`,
  - regression tests.

Known unrelated dirty worktree items must be handled carefully:

- Deleted old docs:
  - `docs/agent-first-architecture-design.md`
  - `docs/agent-integration.md`
  - `docs/daemon-design.md`
- New design docs:
  - `docs/mcp-first-sofarpc-agent-design.md`
  - `docs/mcp-first-sofarpc-agent-design-review.md`
- Untracked local/context directories:
  - `.claude/`
  - `.codex-pet-runs/`
  - `CONTEXT.md`

Do not revert or delete unrelated user changes.

## Phase 0: Lock The Runtime Safety Fixes

Purpose: finish the already-started consumer-cache bug fix so the current daemon is not carrying known OOM/flaky-call risk.

Tasks:

- Final-review `ConnectionManager` lease/ref-count behavior.
- Ensure LRU eviction defers `unRefer` until active leases return.
- Ensure cold consumer creation uses the current request's remaining timeout budget.
- Ensure `RpcTargetKey` excludes per-call timeout.
- Keep `maxConsumers` bounded and fail fast for invalid values.
- Add or keep tests for:
  - same target reuses consumer,
  - timeout does not fragment cache,
  - LRU closes evicted idle consumer,
  - active evicted consumer closes only after lease returns,
  - `destroyAll`,
  - `destroyByAddress`,
  - per-call timeout goes through `RpcInvokeContext`,
  - cold consumer connect timeout does not exceed call budget.

Acceptance:

```bash
mvn -f daemon/pom.xml test
```

must pass.

Notes:

- This phase does not introduce MCP or schema discovery.
- This phase is allowed to improve the old daemon because it directly removes a production-risk bug.

## Phase 1: Engine Startup Diagnostics

Purpose: make agent-facing failures debuggable instead of forcing users to inspect logs manually.

Tasks:

- Teach launcher/spawn code to capture startup failure details:
  - Java executable not found,
  - Java version unsupported,
  - Engine jar missing,
  - process spawn failure,
  - port occupied,
  - ready/hello timeout,
  - captured stderr when available,
  - tail of Engine log when available.
- Normalize these into structured errors.
- Preserve useful cause chains.
- Add tests around launcher failure paths.
- Keep diagnostics bounded; do not dump huge logs.

Acceptance:

- Unit tests for launcher diagnostics.
- Manual or e2e check that a missing jar returns a structured error with path and reason.
- Manual or e2e check that an occupied port returns `ENGINE_UNAVAILABLE / port_occupied`.

## Phase 2: Runtime Naming And Layout Alignment

Purpose: align implementation names and paths with the new product language without breaking existing behavior all at once.

Tasks:

- Introduce Engine-facing names in user output:
  - `engine status/start/stop/restart/logs`,
  - Java implementation may still use daemon package names internally for now.
- Move or adapt install layout toward:

```text
~/.sofarpc/
  bin/
  lib/
  config.json
  state/
  logs/
  cache/
```

- Keep compatibility shims for old paths only if needed for migration.
- Document any temporary compatibility paths.

Acceptance:

- Existing CLI commands still work or have clear replacements.
- Installation/update does not overwrite `config.json`.
- Existing tests pass.

## Phase 3: Config Model

Purpose: replace address-only aliases with explicit projects and servers.

Tasks:

- Add `~/.sofarpc/config.json` model:
  - `projects`,
  - `servers`,
  - `engine`.
- Validate project/server names.
- Canonicalize `workspaceRoot`.
- Normalize `servicePrefixes` to package-boundary suffix.
- Implement config read:
  - no read lock,
  - invalid JSON returns `CONFIG_INVALID`,
  - never auto-overwrite broken config.
- Implement config write:
  - `config.lock`,
  - reread latest,
  - atomic write,
  - stable formatting.
- Provide CLI commands:
  - `project list/add/remove`,
  - `server list/add/remove`.
- Keep direct JSON editing supported.

Acceptance:

- Unit tests for config parse/defaults/validation.
- Unit tests for atomic update and conflict behavior.
- CLI smoke tests for add/list/remove project/server.

## Phase 4: Engine Protocol Contract

Purpose: move the Go-to-Java Engine contract to JSON-RPC over existing length-prefixed TCP.

Tasks:

- Define Engine JSON-RPC method contracts:
  - `engine.hello`,
  - `engine.status`,
  - `engine.shutdown`,
  - `sofarpc.ping`,
  - `sofarpc.invoke`,
  - later `schema.search`,
  - later `schema.describe`.
- Add fixtures for JSON-RPC requests/responses.
- Preserve existing envelope fixtures as legacy behavior reference until replacement is complete.
- Implement protocol decoder/dispatcher in Java Engine.
- Implement Go client request/response handling.
- Clearly separate JSON-RPC protocol errors from SofaRPC result errors.

Acceptance:

- Contract tests from shared fixtures pass on Go and Java sides.
- Existing ping/invoke behavior is covered by JSON-RPC fixtures.
- Old envelope fixtures are not deleted until equivalent new fixtures exist.

## Phase 5: MCP Server Skeleton

Purpose: introduce `sofarpc-mcp` without taking on schema discovery yet.

Tasks:

- Add Go binary entrypoint `sofarpc-mcp`.
- Implement stdio MCP server.
- Share internal packages with `sofarpc-cli` for:
  - config,
  - Engine discovery,
  - Engine JSON-RPC client,
  - result shaping.
- Expose initial tools:
  - `engine_status`,
  - `list_projects`,
  - `list_servers`,
  - `set_current_project`.
- Add sensitive write tools only after config model exists:
  - `add_project`,
  - `remove_project`,
  - `add_server`,
  - `remove_server`.
- Implement `--disable-config-write`.
- Ensure config write tools are marked clearly in descriptions.

Acceptance:

- MCP server starts without starting Java Engine.
- `engine_status` does not start Engine.
- `list_projects/list_servers` do not start Engine.
- Config write tools are absent or hard-disabled when `--disable-config-write` is set.

## Phase 6: MCP Ping And Invoke Without Schema Discovery

Purpose: wire agent tools to current exact-call capability before adding local-source schema intelligence.

Tasks:

- Implement `ping_service`:
  - requires server alias,
  - requires service FQN,
  - does not require schema,
  - lazy-starts Engine.
- Implement exact `invoke_method` using existing generic invoke fields:
  - server alias,
  - service FQN,
  - method,
  - `paramTypes`,
  - `orderedArguments` or current equivalent.
- Return MCP `structuredContent`.
- Map errors to stable `ok=false` tool results.

Acceptance:

- Agent can call configured server/service through MCP.
- `ping_service` result text clearly states that it does not prove remote interface/method existence.
- CLI and MCP produce equivalent Engine requests for the same exact call.

## Phase 7: Java Source Fast Index And Search

Purpose: let agents discover service/method candidates from local Java source.

Tasks:

- Discover source roots:
  - `src/main/java`,
  - `*/src/main/java`.
- Ignore:
  - `src/test/java`,
  - `target`,
  - `build`,
  - `.git`,
  - `.idea`,
  - `node_modules`.
- Build fast method index:
  - package,
  - service FQN,
  - interface simple name,
  - method name,
  - param type names,
  - return type name,
  - Javadoc/comment summary.
- Apply `servicePrefixes` as strong filters by default.
- Add tokenization:
  - camelCase/PascalCase,
  - snake_case/kebab-case,
  - original identifier token,
  - lowercase ASCII,
  - CJK full sequence + bigrams.
- Implement `search_interface`.
- Return method-level candidates with confidence and evidence.

Acceptance:

- Tests for source root discovery.
- Tests for prefix filtering.
- Tests for camelCase and CJK bigram tokenization.
- Search returns candidates without full DTO schema.
- Partial parse failures do not fail the whole search.

## Phase 8: Describe Interface And Schema Cache

Purpose: provide enough schema for agents to construct calls safely.

Tasks:

- Implement `schema.describe`.
- Parse service methods and overloads.
- Parse DTO fields visible in source.
- Parse enums.
- Parse ordinary project-source inheritance.
- Mark unresolved external types.
- Build service schema cache:
  - project/workspace hash,
  - service hash,
  - schema,
  - manifest,
  - last accessed time.
- Implement manifest validation:
  - size/mtime fast path,
  - sha256 on changed files,
  - source-set fingerprint to catch new relevant files.
- Implement 7-day unused cache cleanup off the request hot path.

Acceptance:

- `describe_interface` requires service FQN.
- Optional method returns method-specific schema.
- Unresolved return fields do not block describe.
- Cache invalidates on changed file and source-set change.

## Phase 9: Schema-Guided Invoke

Purpose: let agents pass plain JSON instead of fully typed low-level generic args.

Tasks:

- Implement named `arguments`.
- Keep `orderedArguments` for parameter-name unavailable cases.
- Support optional `paramTypes` only for overload disambiguation.
- Implement overload matching.
- Implement field/type conversion rules:
  - unknown field strict mode,
  - `strictFields=false`,
  - missing field behavior,
  - null vs primitive,
  - numbers,
  - enum,
  - collections,
  - dates.
- Convert plain JSON into generic invocation payload.
- Do not instantiate local user DTO classes.
- Ensure unresolved request fields only block when provided and type-specific conversion is required.

Acceptance:

- Tests for argument conversion.
- Tests for overload ambiguity.
- Tests for unresolved external parent behavior.
- Tests for `strictFields=false`.
- MCP `invoke_method` can call with named arguments for a described method.

## Phase 10: Packaging And Install

Purpose: deliver the two Go binaries plus Java Engine jar in the agreed layout.

Tasks:

- Build release zip:
  - `sofarpc-mcp`,
  - `sofarpc-cli`,
  - `sofarpc-engine.jar`,
  - `install.sh`,
  - `install.ps1`.
- Install to:

```text
~/.sofarpc/bin/
~/.sofarpc/lib/
```

- Create empty `config.json` only if missing.
- Do not overwrite token/config/log/cache.
- Java 8+ detection with clear error.

Acceptance:

- Fresh install works on macOS/Linux.
- Windows install script exists, even if deeper Windows validation is later.
- Upgrade does not overwrite existing config.

## Phase 11: Documentation Refresh

Purpose: make the repo match the new product shape.

Tasks:

- Update README to MCP-first language.
- Point agent users at MCP tools first.
- Document CLI as diagnostics/config/reproduction.
- Remove or archive old daemon-first docs after equivalent new docs exist.
- Add a short troubleshooting section for:
  - Java missing,
  - Engine start timeout,
  - port occupied,
  - bad config,
  - schema required,
  - unresolved external type.

Acceptance:

- README no longer presents the old CLI-first contract as primary.
- Old docs are either restored as legacy docs or intentionally removed in the same commit as replacements.

## Suggested Work Order

Do this first:

1. Phase 0: lock runtime safety fixes.
2. Phase 1: startup diagnostics.
3. Phase 3: config model.
4. Phase 4: Engine JSON-RPC contract.
5. Phase 5: MCP skeleton.

Then:

6. Phase 6: MCP ping/exact invoke.
7. Phase 7: search.
8. Phase 8: describe/cache.
9. Phase 9: schema-guided invoke.
10. Phase 10/11: packaging/docs.

## Definition Of Done For The First Usable Agent Slice

The first useful MCP slice is complete when:

- A user can configure a project/server.
- MCP starts without starting Java.
- Agent can list projects/servers.
- Agent can ping a configured service FQN.
- Agent can invoke an exact method with explicit `paramTypes` and arguments.
- Engine startup failures return structured diagnostics.
- Consumer cache is bounded and safe under concurrent invocation.

Schema discovery can arrive after this slice; it is not required for the first MCP end-to-end path.
