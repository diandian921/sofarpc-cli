# MCP-First SofaRPC Agent Design

Status: Draft  
Date: 2026-05-17

## Purpose

This project is an MCP-first SofaRPC invocation runtime for agents.

The goal is not to build a general test case platform. The goal is to let an agent discover a SofaRPC interface from local Java source, translate plain JSON into a schema-guided generic invocation payload, invoke the remote service through SofaRPC GenericService, and inspect the structured result.

## Product Boundary

In scope:

- MCP tools for agent use.
- A human-facing CLI for diagnostics, configuration, and reproducing agent calls.
- A shared local Java Engine for SofaRPC execution.
- Local source schema discovery and cache.
- Schema-guided plain JSON invocation.

Out of scope for the first design:

- Case management.
- Test suites.
- Reports.
- Built-in assertion tool.
- Project-level or user-level manually maintained schema.
- Git/source-jar/schema download source.
- Per-project SofaRPC dependency resolution.
- Loading user project classes by default.
- Full Java compiler semantics.

The agent owns test intent and result judgment. The tool owns SofaRPC discovery and invocation.

## Architecture

```text
Agent
  -> sofarpc-mcp
  -> shared per-user Java Engine
  -> SofaRPC GenericService
  -> target SofaRPC service

Human / CI
  -> sofarpc-cli
  -> same shared Java Engine
```

Artifacts:

- `sofarpc-mcp`: Go stdio MCP server, agent-facing.
- `sofarpc-cli`: Go human-facing CLI, diagnostic/config/reproduction tool.
- `sofarpc-engine.jar`: Java 8 compatible SofaRPC execution engine.

The Java Engine is shared per user. Multiple agents should reuse the same warm Engine process.

## Relationship To Existing Implementation

This design is an evolution of the current repository, not a greenfield rewrite.

Existing assets to reuse:

- Go launcher structure under `cli/internal/launcher`, including process spawn, state handling, and startup locking.
- Go IPC framing under `cli/internal/ipc`, especially the 4-byte length-prefixed TCP framing.
- Java Engine server skeleton under `daemon/src/main/java/com/sofarpc/daemon/server`.
- Java lifecycle pieces such as `IdleTracker`, with defaults adjusted to this design.
- Current protocol fixtures and contract-test discipline under `protocol/fixtures` and `protocol/schema`.
- Existing alias/config code as implementation reference for project/server config.

Existing assets to rename or reshape:

- `daemon` becomes implementation language for the Java Engine; user-facing language should say Engine.
- Old `daemon start/stop/status` user commands become `sofarpc-cli engine start/stop/status`.
- Old address alias config evolves into `projects` and `servers` in `~/.sofarpc/config.json`.
- Existing `invoke/ping/health/shutdown` operations map into the new Engine method set.

Existing assets to replace:

- Old invoke assertions are removed from the first MCP design.
- Old case/report language is removed from the first product boundary.
- Old response fixtures that include assertion semantics should be retired or moved into legacy fixtures.
- Old connection cache behavior must be replaced with the bounded consumer lifecycle described below.

Fixture migration:

- Keep existing fixtures as a compatibility reference until the new contract is implemented.
- Add new fixtures for MCP tool outputs and Engine requests before replacing old fixtures.
- Contract tests should verify that `sofarpc-mcp` and `sofarpc-cli` resolve the same config into byte-equivalent Engine requests for the same logical operation.
- Do not delete old fixtures until the new fixtures cover ping, search, describe, invoke, Engine unavailable, timeout, and bad request cases.

## Responsibility Split

Go layer responsibilities:

- MCP protocol and tool schemas.
- CLI command parsing.
- `~/.sofarpc/config.json` read/write.
- Project and server aliases.
- Current MCP session project.
- Engine discovery, startup, token setup, version checks.
- Agent-friendly tool results.

Java Engine responsibilities:

- SofaRPC GenericService invocation.
- Consumer/proxy creation.
- Java source parsing for service/interface/DTO schema.
- Schema cache validation and rebuild.
- Argument validation/conversion from JSON to generic SofaRPC payload values.
- RPC timeout and exception classification.

The Java Engine does not read `config.json`, does not know MCP, and does not manage cases or assertions.

## Local Layout

First version uses a fixed user home directory. No `--home` or `--config` override for now.

```text
~/.sofarpc/
  bin/
    sofarpc-mcp
    sofarpc-cli
  lib/
    sofarpc-engine.jar
  config.json
  state/
    token
    engine.json
    engine.lock
    config.lock
  logs/
    engine.log
  cache/
    schema/
```

Release package:

```text
sofarpc-mcp
sofarpc-cli
sofarpc-engine.jar
install.sh
install.ps1
```

Install overwrites binaries and jar, but does not overwrite `config.json`, token, logs, state, or cache.

## Engine Transport

Go clients talk to the Java Engine using JSON-RPC 2.0 over length-prefixed loopback TCP.

Frame:

```text
4-byte big-endian length
UTF-8 JSON-RPC payload
```

Default endpoint:

```text
127.0.0.1:37651
```

The port is configurable in `config.json`. The host must be loopback only:

- `127.0.0.1`
- `localhost`
- `::1`

No HTTP transport. No LAN binding.

Rationale for JSON-RPC:

- MCP itself is JSON-RPC, so request/response/error concepts are familiar to agent infrastructure.
- Namespaced methods such as `schema.search` and `sofarpc.invoke` are clearer than an untyped `op` field as Engine capabilities grow.
- JSON-RPC protocol errors can be reserved for Engine protocol/auth/method failures, while SofaRPC failures stay in normal results.

Migration note:

- The existing length-prefixed transport should be reused.
- Existing envelope fixtures remain useful as behavior fixtures, but the wire payload contract changes to JSON-RPC.
- JSON-RPC is the selected Engine protocol for this design. Reverting to the old envelope requires a separate ADR-level decision, not an implementation-time shortcut.

## Engine Token

The token protects the local shared Engine from arbitrary local processes.

It is not:

- A user login token.
- A SofaRPC server credential.
- A remote access token.
- An agent identity.

Go creates and owns:

```text
~/.sofarpc/state/token
```

Token rules:

- At least 32 random bytes.
- Encoded as base64url or hex.
- macOS/Linux permission: `0600`.
- Windows: current-user readable.

Threat model:

- The token primarily blocks other local users and accidental local clients.
- A same-user process that can read `~/.sofarpc/state/token` can authenticate.
- The token is still useful because fixed loopback TCP alone has no application-level identity check.

Engine is started with a token file path, not a token value:

```text
java -jar ~/.sofarpc/lib/sofarpc-engine.jar \
  --host 127.0.0.1 \
  --port 37651 \
  --token-file ~/.sofarpc/state/token \
  --state-file ~/.sofarpc/state/engine.json \
  --log-file ~/.sofarpc/logs/engine.log \
  --cache-dir ~/.sofarpc/cache/schema
```

Each connection first calls `engine.hello`. After hello succeeds, that connection is authenticated.

## Engine Lifecycle

`sofarpc-mcp` uses lazy start:

- MCP server startup does not start Java.
- Config tools do not start Java.
- Engine starts only when a tool needs it.

Tools that need Engine:

- `ping_service`
- `search_interface`
- `describe_interface`
- `invoke_method`

`engine_status` does not start Engine.

Engine discovery authority:

```text
connect fixed port + engine.hello success
```

`engine.json` is diagnostic only, never authoritative.

Startup flow:

```text
try connect + hello
if unavailable:
  acquire ~/.sofarpc/state/engine.lock
  retry connect + hello
  if still unavailable:
    start Java Engine
    wait for ready + hello
  release lock
```

Default start timeout:

```text
20000ms
```

If the port is occupied by a non-Engine process, return `ENGINE_UNAVAILABLE / port_occupied`. Do not kill the process and do not auto-switch ports.

Startup failure diagnostics:

- `ENGINE_UNAVAILABLE` must include a structured `reason`.
- Java missing or unsupported must return `java_not_found` or `java_version_unsupported`.
- Missing jar must return `engine_jar_not_found`.
- Port conflict must return `port_occupied`.
- Engine start timeout must return `engine_start_timeout`.
- When spawn fails or readiness times out, include captured Java stderr if available and the tail of `engine.log`.
- Do not make agents inspect local log files manually for basic startup failures.

Idle TTL:

- Default `30m`.
- Engine exits itself after no real request activity.
- Idle long connections do not keep Engine alive.
- Engine exits only when no active invoke/schema build is running.

Versioning:

- `engine.hello` returns `engineVersion` and `protocolVersion`.
- Go checks compatibility.
- Incompatible protocol triggers graceful Engine restart.
- Only one Engine version runs per user at a time.
- Cross-session restart must be coordinated: reject new Engine requests, wait for active requests up to the graceful shutdown budget, then restart.
- Default `sofarpc-mcp` reconnect behavior is transparent: if the Engine exits due to idle TTL or restart, the next tool call reconnects and may lazy-start a new Engine.

Runtime config changes:

- `engine_status` should report both running settings and desired settings from `config.json`.
- If running settings differ from desired settings, return `restartRequired=true`.
- Changing `engine.port` makes clients connect to the new port; any old Engine on the previous port will exit by idle TTL or explicit CLI stop.
- Changing same-port runtime settings such as `idleTTL` or `maxConcurrentInvokes` does not silently restart Engine.
- MCP does not provide Engine restart; users use `sofarpc-cli engine restart` when they want changed settings to take effect immediately.

## Engine Concurrency

Default:

```text
maxConcurrentInvokes = 8
```

Rules:

- Invokes beyond the limit queue.
- Queue wait counts toward the request timeout.
- Queue timeout before RPC is sent returns `ENGINE_BUSY`.
- RPC timeout after request is sent returns `RPC_TIMEOUT`.
- Same-service schema builds are deduplicated.
- `timeoutMs` is a total budget. Before each phase, compute the remaining budget.
- If queue/schema phases consume part of the budget, the SofaRPC call receives only the remaining budget.
- Do not pass the full original `timeoutMs` to SofaRPC after queue/schema time has already elapsed.

Consumer/proxy lifecycle:

- The Engine must use a bounded cache for SofaRPC `ConsumerConfig<GenericService>` / proxy instances.
- Cache key must not include per-invocation timeout.
- Cache key should include the stable target identity: protocol, address, appName, service FQN, and other consumer-construction fields that truly affect the proxy.
- Per-call timeout must be applied through per-invocation context when SofaRPC supports it, not by creating a new consumer per timeout.
- Cache eviction must call `unRefer` / destroy on the evicted consumer config.
- Default cache size should be explicit, for example `maxConsumers = 256`.
- Eviction should be LRU or an equivalent bounded policy.
- `ping_service` must reuse the same consumer cache and must not create unbounded one-off consumers.
- This replaces the current unbounded `ConnectionManager` behavior where timeout participates in `RpcTargetKey`.

## CLI Engine Commands

CLI is diagnostic/config/reproduction tooling, not the primary product.

Engine commands:

```text
sofarpc-cli engine status
sofarpc-cli engine start
sofarpc-cli engine stop
sofarpc-cli engine restart
sofarpc-cli engine logs
```

Rules:

- `engine status` does not start Engine.
- `engine stop` defaults to graceful stop.
- Graceful stop waits 10s.
- `engine stop --force` is an escape hatch.
- `engine restart = graceful stop + start`.
- Restart does not force kill unless `--force` is explicit.

## Logs

Default log path:

```text
~/.sofarpc/logs/engine.log
```

Log by default:

- Engine start/stop/pid/port.
- Hello auth failure.
- Schema build start/success/failure.
- Schema cache hit/stale.
- RPC summary: server/address, service, method, elapsed, code.
- Exception class and message summary.

Do not log by default:

- Full request body.
- Full response body.
- Token.
- Sensitive config values.

Rolling policy:

- 50 MB per file.
- Keep 5 files.
- No user-facing config for this initially.

## Configuration

Configuration file:

```text
~/.sofarpc/config.json
```

Format: JSON, 2-space indentation when written by the tool.

Example:

```json
{
  "projects": {
    "user": {
      "workspaceRoot": "/Users/me/workspace/user-service",
      "servicePrefixes": ["com.company.user."]
    }
  },
  "servers": {
    "user-test": {
      "address": "10.0.0.1:12200",
      "project": "user",
      "protocol": "bolt",
      "timeoutMs": 5000,
      "appName": "sofarpc-agent",
      "attachments": {}
    }
  },
  "engine": {
    "host": "127.0.0.1",
    "port": 37651,
    "javaHome": null,
    "idleTTL": "30m",
    "startTimeoutMs": 20000,
    "maxConcurrentInvokes": 8
  }
}
```

Project/server names:

```text
^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$
```

`workspaceRoot`:

- Stored as canonical absolute path.
- `~` is expanded.
- Symlinks are resolved.
- Must exist and be a directory.

`servicePrefixes`:

- Project-level only.
- Multiple prefixes allowed.
- Saved with package boundary suffix, e.g. `com.company.user.`.
- Default strong filter when present.
- `includeOutOfPrefix=true` can expand search for special cases.

Server config:

- `address`: single `host:port`.
- `project`: bound project.
- `protocol`: default `bolt`.
- `timeoutMs`: default `5000`, total invoke budget.
- `appName`: default `sofarpc-agent`.
- `attachments`: default `{}`.

Attachments:

- `map<string,string>`.
- JSON content must be passed as a JSON string value.
- Server-level attachments and invoke-level attachments are merged.
- Invoke attachments override server attachments.

Configuration entrypoints:

- `config.json` is a stable user-editable JSON file.
- Humans may edit `~/.sofarpc/config.json` directly.
- CLI provides the normal explicit human configuration path.
- MCP may expose configuration write tools for agent-assisted setup, but they are sensitive write tools and must not run silently.

Config writes:

- Only config tools write config.
- `invoke/search/describe/ping` never write config.
- MCP config write tools must clearly state that they modify `~/.sofarpc/config.json`.
- MCP hosts/users must confirm sensitive write tools before execution.
- Configuration writes must never be triggered implicitly by `invoke`, `search`, `describe`, or `ping`.
- Writes use `~/.sofarpc/state/config.lock`.
- Reads do not lock.
- Writes reread latest config, apply change, write temp file, then atomic rename.
- Same-name add fails unless `overwrite=true`.
- Remove requires `confirm=true`.
- `remove_project` fails if servers reference it unless `cascade=true`.

Config read failures:

- Invalid JSON returns `CONFIG_INVALID`.
- Do not overwrite, truncate, or auto-recreate a broken `config.json`.
- Error details should include config path and parse location/detail when available.
- Users may fix the JSON manually or recreate it explicitly through CLI after moving the broken file aside.

Config upgrades:

- Treat `config.json` as a stable user contract.
- Do not auto-migrate or rewrite on startup/read.
- New fields use in-memory defaults.
- Old fields may be read compatibly when practical.
- Breaking changes should fail clearly and use an explicit future `config migrate`.
- Cache/state can be versioned and auto-upgraded or rebuilt.

## MCP Tools

Final MCP tool list:

```text
list_projects
add_project
remove_project
set_current_project

list_servers
add_server
remove_server

ping_service
search_interface
describe_interface
invoke_method
engine_status
```

Sensitive MCP write tools:

- `add_project`
- `remove_project`
- `add_server`
- `remove_server`

These tools exist for agent-assisted setup, but they cross a trust boundary because they can change long-lived project roots and server addresses. They require explicit user/host confirmation and must be possible to disable in the MCP server itself.

Hard disable:

- `sofarpc-mcp` must support a server-side switch to disable config write tools, for example `--disable-config-write`.
- Default is enabled for personal developer machines, because agent-assisted setup is part of the intended workflow.
- Host/user confirmation is a UX safeguard, not a server-enforced security boundary.
- Security-sensitive deployments should run `sofarpc-mcp --disable-config-write`.
- When disabled, `add_project`, `remove_project`, `add_server`, and `remove_server` are not registered or always return a hard disabled error.
- This does not rely on MCP host confirmation behavior.
- CLI config commands and direct JSON editing remain available.

Naming:

- MCP tools use `snake_case` / `verb_object`.
- Internal Engine JSON-RPC uses `namespace.method`.

Mapping:

```text
search_interface    -> schema.search
describe_interface  -> schema.describe
invoke_method       -> sofarpc.invoke
ping_service        -> sofarpc.ping
engine_status       -> engine.status
```

Tool descriptions should be short. Input field descriptions should be strict about required fields, defaults, and mutually exclusive fields.

All core MCP tools return:

- `content`: short text summary.
- `structuredContent`: stable JSON for agents.
- `isError=true` for tool execution failures.

SofaRPC/config/schema failures are tool execution errors, not MCP protocol errors.

## Project Context Resolution

Project context priority:

```text
explicit project parameter
> server.project
> session current project
> PROJECT_REQUIRED
```

`set_current_project` is session-only. It does not write config and there is no persistent default project in the first design.

If `server` and explicit `project` disagree, return `BAD_REQUEST / project_server_mismatch`.

Users do not need to manually pass tool parameters. Agents fill `project` and `server` based on conversation context. If context is ambiguous, the agent should ask.

## Engine JSON-RPC Methods

Internal Engine methods:

```text
engine.hello
engine.status
engine.shutdown
schema.search
schema.describe
sofarpc.ping
sofarpc.invoke
```

Engine does not expose project/server alias management. Go resolves aliases and passes resolved data.

## Schema Discovery

Schema source:

- Local Java source only.
- No Git download.
- No source jar download.
- No manual project/user schema.
- No Maven/Gradle compile requirement.

Known schema source limit:

- External jar DTO parents and shared base request classes may be unresolved.
- This is expected in enterprise projects with `BaseRequest` / `BaseResponse` patterns.
- The first design does not fetch source jars and does not load project classes, so those inherited external fields may be absent from schema.
- Generic invocation does not instantiate local user DTO classes. The risk is not "cannot new the DTO"; the risk is "cannot validate or convert provided fields whose Java types are unknown".
- If a provided request field requires type-specific validation/conversion and its type cannot be resolved, invocation fails with `SCHEMA_REQUIRED`.
- Future escape hatches are `$type`, `typedArguments`, source-jar support, or per-project classpath fallback.

Source roots:

- Default scan only `src/main/java` and `*/src/main/java`.
- Do not scan `src/test/java`.
- Ignore `target`, `build`, `.git`, `.idea`, `node_modules`.
- Limit directory depth.

`search_interface`:

- Builds a fast method index if missing/stale.
- Does not build full DTO schema.
- Returns method-level candidates.
- Does not return full request/response schema.
- Supports `limit`, default 5, max 20.

Fast index contains:

- Service FQN.
- Interface simple name.
- Package name.
- Method name.
- Parameter type names.
- Return type name.
- Method/interface Javadoc summary.

Rich DTO index may be filled in the background later, but user does not manage warmup.

`describe_interface`:

- `service` must be FQN.
- Optional `method`.
- Without method: return method list and signatures.
- With method: return detailed parameter and return schema for matching methods/overloads.
- Does not return source code.

`invoke_method`:

- `service` must be FQN.
- Does not auto-search.
- Ensures target service schema automatically.
- If schema is missing/stale, rebuilds it automatically.

`ping_service`:

- `service` must be FQN.
- Does not need method.
- Does not need schema.
- Lazy starts Engine.
- Creates/checks consumer/proxy only.
- A successful ping means the configured transport path is locally usable enough to create a consumer/proxy.
- It does not prove that the remote service interface exists, that a method exists, or that a business method can succeed.

## Schema Cache

Cache path:

```text
~/.sofarpc/cache/schema/
```

Directory shape:

```text
~/.sofarpc/cache/schema/
  projects/
    user-<workspaceHash>/
      index.json
      services/
        <serviceHash>.json
```

`workspaceHash`:

- SHA-256 of canonical `workspaceRoot`.
- First 12 hex chars.

`serviceHash`:

- SHA-256 of service FQN.
- First 16 hex chars.

Service cache file includes full project name, workspace root, service FQN, schema, manifest, and `lastAccessedAt`.

Cache validation:

- Manifest records related source files.
- Manifest also records a source-set fingerprint for candidate Java files under discovered source roots.
- Each file records path, size, mtime nanos, and sha256.
- On normal validation, first check size + mtime.
- If unchanged, use cache.
- If changed, compute sha256.
- If sha256 unchanged, update manifest metadata and use cache.
- If sha256 changed, rebuild that service schema.
- If the source-set fingerprint changes, re-check the service/index because a newly added Java file may introduce overloads or newly resolvable DTO types that were not present in the old manifest.

Manifest should include the service interface, request DTOs, response DTOs, nested DTOs, enums, parent classes, field types, and generic types that are visible in source.

Source-set fingerprint:

- Derived from relative paths plus size/mtime for candidate `.java` files under relevant source roots and service prefixes.
- Does not require hashing file contents on the hot path.
- Prevents the blind spot where only already-known manifest files are checked and new relevant files are ignored.

Cache cleanup:

- Cache files unused for 7 days may be cleaned.
- Cleanup runs outside request hot paths, such as Engine startup async cleanup.
- Cleanup failure is logged and never blocks invocation.

## Java Source Parsing Boundary

Must be accurate enough for:

- Service interface.
- Method name.
- Overloads.
- Parameter count, names, and types.
- Return type.
- DTO fields and field types.
- Enum values.
- List/Set/Map/array.
- Basic types, String, dates, BigDecimal.
- Ordinary project-source inheritance fields.

Allowed to degrade:

- Lombok-generated accessor semantics.
- MapStruct.
- Complex Jackson behavior.
- Complex generic bounds.
- External jar deep object structure.
- Runtime dynamic types.

Lombok fields are treated as normal DTO fields if the source contains private fields. Getter/setter presence is not required for schema.

Unresolved types:

- Unresolved return fields do not block.
- Unresolved request fields do not block if not provided.
- Unresolved request fields block only when the caller provides that field and the Engine must perform type-specific validation or conversion for it.
- Unresolved method parameter FQN blocks invocation because GenericService still needs method argument type names.
- Overload resolution depending on unresolved types can block.
- External jar parent classes do not automatically block if the user only sets fields visible in local source and the Engine can represent those fields in the generic payload.
- External parent fields are marked unresolved/absent; the server may still enforce them at business level.

No `$type` or `typedArguments` in the first version. They are possible future escape hatches.

## Interface Search

`search_interface` input:

```json
{
  "project": "user",
  "query": "用户 查询 user query get userId",
  "limit": 5
}
```

The agent turns natural language into a search query. Engine does deterministic search only; it does not use an LLM and does not understand full user tasks.

Tokenization:

- Split camelCase and PascalCase identifiers, for example `queryUserInfo` -> `query`, `user`, `info`.
- Split snake_case and kebab-case.
- Preserve the original identifier as a token.
- Lowercase ASCII tokens for matching.
- For contiguous CJK text, preserve the full sequence and also emit character bigrams. For example `查询用户` -> `查询用户`, `查询`, `询用`, `用户`.
- Do not require Chinese-English semantic translation inside Engine; the agent may add English terms to `query`.

Search signals:

High weight:

- Interface simple name.
- Method name.
- Parameter type name.
- DTO field name.

Medium weight:

- Package name.
- Return type name.
- Enum name.
- Service prefix.

Low weight:

- Javadoc/comment.
- Annotation value.

Confidence behavior:

- High confidence unique match can be used by the agent.
- Multiple close matches return candidates and should be confirmed.
- Low confidence should ask the user for more context.

`servicePrefixes` are strong filters by default. Prefixes outside the configured range are excluded unless `includeOutOfPrefix=true`.

Search is tolerant:

- Missing workspace/source root returns `SCHEMA_REQUIRED`.
- Partially unparsable Java files are skipped with diagnostics.
- No interfaces returns `SCHEMA_REQUIRED / no_interfaces_found`.

`describe_interface` and `invoke_method` are not tolerant for the target schema; target schema failures return explicit errors.

## Invocation Contract

`invoke_method.service`, `describe_interface.service`, and `ping_service.service` must be Java FQN.

`invoke_method` accepts either:

- `arguments`
- `orderedArguments`

They are mutually exclusive.

Preferred named form:

```json
{
  "server": "user-test",
  "service": "com.company.user.api.UserQueryService",
  "method": "queryUser",
  "arguments": {
    "request": {
      "userId": "123"
    }
  }
}
```

Ordered form:

```json
{
  "server": "user-test",
  "service": "com.company.user.api.UserQueryService",
  "method": "query",
  "orderedArguments": [
    {
      "userId": "123"
    }
  ]
}
```

`orderedArguments` only changes parameter matching. Generic payload assembly still uses schema, field validation, and type conversion.

Overloads:

- Engine can match by method name, parameter count, parameter names, parameter types, and argument structure.
- If ambiguous, return `AMBIGUOUS_METHOD_OVERLOAD`.
- Optional `paramTypes` is supported only for overload disambiguation.
- `paramTypes` does not replace schema parsing.

Unknown fields:

- `strictFields=true` by default.
- Unknown DTO field returns `BAD_REQUEST`.
- Map fields allow arbitrary keys.
- Single invoke may set `strictFields=false`, in which case unknown DTO fields are ignored.
- If the target DTO has an unresolved external parent, unknown-field errors should mention that inherited fields may be invisible and suggest retrying with `strictFields=false` only when the user intentionally targets inherited/external fields.
- Do not silently accept unknown fields just because an unresolved parent exists; keep strict mode as the default.

Missing fields:

- Allowed.
- Reference types become null.
- Primitive types use Java defaults.
- No Bean Validation/Javadoc-based required-field inference.

Null handling:

- Reference types allow null.
- Primitive types reject null with `BAD_REQUEST`.

Number conversion:

- Integer/int: integer number or numeric string; no decimals.
- Long/long: integer number or numeric string; no decimals; large values should be strings.
- BigDecimal: number or decimal string.
- Double/Float: number or numeric string.
- No lossy conversion.

Enum:

- Match enum `name`.
- Case sensitive.
- Invalid value returns allowed values.

Collections:

- `List<T>` / array: JSON array.
- `Set<T>`: JSON array.
- `Map<K,V>`: JSON object.
- List/Set elements are converted by `T`.
- Map keys support String, primitive wrappers, and enum.
- `Map<String,Object>` allows arbitrary JSON.
- Raw List/Map allowed with no deep type conversion.

Dates:

- Date/Instant: ISO-8601 datetime with timezone, e.g. `2026-05-17T10:30:00+08:00`.
- LocalDate: `yyyy-MM-dd`.
- LocalDateTime: `yyyy-MM-dd'T'HH:mm:ss`.
- Millisecond timestamp can be a compatibility option, but string is preferred.

Java to JSON return conversion:

- String -> string.
- boolean/Boolean -> boolean.
- int/Integer -> number.
- double/Double -> number.
- long/Long -> string.
- BigInteger -> string.
- BigDecimal -> string.
- Date/LocalDate/LocalDateTime -> ISO-8601 string.
- Enum -> enum name string.
- byte[] -> base64 string.
- List/Set/array -> array.
- Map -> object.
- Object -> object.
- null -> null.

No `$type`/`$value` wrappers in normal return JSON. Type details belong in `diagnostics` or `describe_interface`.

## Results

Successful invoke:

```json
{
  "ok": true,
  "code": "SUCCESS",
  "result": {
    "userId": "123",
    "name": "Alice"
  },
  "elapsedMs": 32
}
```

Failure:

```json
{
  "ok": false,
  "code": "RPC_TIMEOUT",
  "error": {
    "message": "SOFARPC request timed out after 5000ms"
  },
  "elapsedMs": 5000
}
```

`verbose=true` may include diagnostics such as schema cache state, schema build time, RPC elapsed time, return type, and Engine pid.

Do not include full schema/signature/candidates in invoke success by default. Agents can call `describe_interface` or `search_interface` when needed.

## Error Codes

Final stable codes:

```text
SUCCESS
BAD_REQUEST
CONFIG_INVALID
PROJECT_REQUIRED
SCHEMA_REQUIRED
SCHEMA_TIMEOUT
AMBIGUOUS_PROJECT
AMBIGUOUS_INTERFACE
AMBIGUOUS_METHOD_OVERLOAD
CONNECT_FAILED
RPC_TIMEOUT
INVOKE_FAILED
ENGINE_UNAVAILABLE
ENGINE_BUSY
INTERNAL_ERROR
```

JSON-RPC protocol errors are reserved for internal protocol/method/auth problems. SofaRPC, schema, config, and invocation failures return normal tool results with `ok=false`.

## Installation And Java

First version:

- Does not bundle JRE.
- Requires local Java 8+.
- Engine bytecode target is Java 8.
- Fixed SofaRPC client dependency inside Engine.
- Does not dynamically load project SofaRPC versions.

Known RPC compatibility limits:

- First version targets direct bolt + Hessian/generic invocation.
- Services requiring protobuf, custom serialization, registry-only discovery, or project-specific SofaRPC client behavior may fail with `INVOKE_FAILED` or `CONNECT_FAILED`.
- These are not solved by schema parsing; they require later transport/serialization/profile support.

Java selection priority:

```text
SOFARPC_JAVA_HOME
config.engine.javaHome
JAVA_HOME
PATH java
```

If Java is missing or unsupported, return `ENGINE_UNAVAILABLE` with a clear reason.

## Future Iterations

Possible future additions, not part of the first design:

- `$type` field-level escape hatch.
- `typedArguments`.
- Bundled JRE packages.
- `--home` / `SOFARPC_HOME`.
- Go binary embedding and releasing `sofarpc-engine.jar`.
- Per-project classloader fallback.
- Per-project SofaRPC dependency profile.
- Registry/service discovery.
- More advanced attachments templates.
- Built-in assertions.
- Case/suite/report management.
