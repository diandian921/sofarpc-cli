# SofaRPC MCP

MCP-first SofaRPC testing toolkit for agents.

The primary entrypoint is `sofarpc-mcp`, a stdio MCP server. `sofarpc` is kept for human configuration, diagnostics, and exact reproduction. Invocation runs through a pure-Go direct BOLT/Hessian2 runtime; no Java process or sidecar is required.

## What Gets Installed

```text
~/.sofarpc/                 (override with SOFARPC_HOME)
  bin/
    sofarpc
    sofarpc-mcp
  config.json
  cache/
    schema/
```

`config.json` and cache are never overwritten; upgrade just replaces the
binaries at the same canonical path.

## Install

From a release tarball (no Go toolchain needed):

```bash
tar -xzf sofarpc-vX.Y.Z-darwin-arm64.tar.gz
cd sofarpc-vX.Y.Z-darwin-arm64
./install.sh
```

Or with Go (requires a published plain `vX.Y.Z` tag on a root-module commit;
the legacy `cli/v*` tags from the subdirectory-module era are not resolvable
under the current module path):

```bash
go install github.com/diandian921/sofarpc-cli/cmd/sofarpc@vX.Y.Z
go install github.com/diandian921/sofarpc-cli/cmd/sofarpc-mcp@vX.Y.Z
# call the just-installed binary by absolute path (GOBIN, else GOPATH/bin):
BIN="$(go env GOBIN)"; BIN="${BIN:-$(go env GOPATH)/bin}"
"$BIN/sofarpc" self-install
```

`@latest` is only correct once a plain `vX.Y.Z` tag is cut on a root-module
commit. Until then the tarball channel above is the supported install path.

`install.sh` is a thin bootstrap; all install logic is in `sofarpc
self-install`, which creates the layout above and prints a PATH hint if
`~/.sofarpc/bin` is not on `PATH` (it never edits shell profiles).

Build release archives for the full platform matrix:

```bash
./scripts/package.sh
```

Each archive contains `sofarpc`, `sofarpc-mcp`, `README.md`, `install.sh`,
`install.ps1`; a single `SHA256SUMS` covers all archives. Requirements: Go
1.19+ when building from source.

## MCP Configuration

Do not hand-write host config. Register with the host's own CLI via:

```bash
sofarpc setup claude          # or: codex, or: all
sofarpc setup all --dry-run   # preview the exact commands, mutate nothing
```

`setup` registers the fully expanded absolute path to `sofarpc-mcp` (never
`~`), propagates `SOFARPC_HOME` only when it is non-default, and verifies the
binary with `sofarpc-mcp --selftest` before touching host config. Re-run
behavior is host-dependent: Codex exposes `mcp get --json` so setup is
exactly idempotent (matching entry → no-op); Claude has no JSON read-back, so
setup is existence-safe — it will not silently overwrite an existing entry and
requires `--force` to replace one.

## MCP Tools

The MCP surface is intentionally small and workflow-oriented:

- `sofarpc_config`: list or update `~/.sofarpc/config.json`.
- `sofarpc_resolve`: resolve the project, server, and endpoint without touching the network.
- `sofarpc_probe`: probe TCP reachability for a configured server or explicit address.
- `sofarpc_describe`: search local Java source or describe a service/method schema.
- `sofarpc_invoke`: invoke a method, or return the planned request when `dryRun=true`.
- `sofarpc_doctor`: run structured diagnostics for config, source schema, and invoke prerequisites.

`sofarpc_probe` checks the configured transport path. It does not prove the remote interface, method, or business behavior exists.

`sofarpc_config` uses an `action` field for mutations. `--disable-config-write` keeps the tool visible but rejects mutating actions such as `save_project`, `save_server`, `remove_project`, and `remove_server`.

`sofarpc_invoke` supports either exact low-level arguments:

```json
{
  "server": "user-test",
  "service": "com.example.UserService",
  "method": "getUser",
  "paramTypes": ["java.lang.String"],
  "orderedArguments": ["u001"]
}
```

or named arguments when local source can resolve the method signature:

```json
{
  "server": "user-test",
  "service": "com.example.UserService",
  "method": "getUser",
  "arguments": {
    "userId": "u001"
  }
}
```

Use `dryRun=true` to inspect the endpoint, parameter types, ordered arguments, and protocol payload without sending a SofaRPC request.

Set `rawResult=true` when debugging serialization or response shape problems. The response then includes both the normal flattened `result` and the decoded Java object shape as `rawResult`.

Assertions are intentionally not part of `sofarpc_invoke`. For assertion-based exact reproduction, use `sofarpc invoke --assertions-json`.

## Config File

`~/.sofarpc/config.json` is stable and user-editable. The current schema version
is `1`. Older files without `version` are read as version 1; unsupported future
versions are rejected with `CONFIG_UNSUPPORTED_VERSION`.

```json
{
  "version": 1,
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
  }
}
```

## CLI

Use CLI for setup and diagnostics:

```bash
sofarpc project add user /Users/me/workspace/user-service --prefix com.company.user
sofarpc server add user-test 10.0.0.1:12200 --project user
sofarpc server list --json
```

The `invoke` and `ping` commands are available for exact reproduction; both emit the same structured result contract the MCP tools return.

For invoke reproduction:

```bash
sofarpc invoke \
  --address user-test \
  --service com.example.UserService \
  --method getUser \
  --arg-types com.example.GetUserRequest \
  --args-json '[{"userId":"u001"}]'
```

## Local Source Schema

The MCP server parses local Java source only. It does not download Git repos, source jars, Maven dependencies, or load project classes.

Scanned roots:

- `src/main/java`
- `*/src/main/java`

Ignored:

- `src/test/java`
- `target`
- `build`
- `.git`
- `.idea`
- `node_modules`

Schema cache is stored under `~/.sofarpc/cache/schema/` and invalidated by source content fingerprint. Entries unused for 7 days may be cleaned.

## Runtime Boundaries

The pure-Go runtime covers direct BOLT generic invocation and the common Hessian2 value shapes used by DTO-style requests and responses. Declared Java argument and DTO field types are used for numeric encoding, so values such as `Integer`, `Long`, and `Double` do not depend on Go's JSON number shape. The current Java compatibility status is tracked in `docs/compatibility-matrix.md`.

Known limits:

- object reference preservation is not implemented for request encoding; cyclic request values are rejected.
- `BigInteger`, Go request encoding for `java.util.Date`, enum payloads without source schema, and provider-specific Hessian extensions need more compatibility work before relying on them broadly. Schema-known enum parameters and DTO fields are covered by Hessian oracle tests.
- map keys are flattened to strings in the normal `result`; use `rawResult=true` when key type matters during diagnosis.

## Security Boundaries

`sofarpc-mcp` is a local developer tool. Treat stdout as the JSON-RPC protocol stream; diagnostics and future logging must go to stderr. `sofarpc_probe` can dial an explicit address for diagnostics, so prefer configured servers when running against untrusted agent input.

Each JSON-RPC stdin frame is capped at 128 MiB. Oversized frames are rejected with a parse error and the server continues reading subsequent frames.

## Troubleshooting

- `CONFIG_INVALID`: fix `~/.sofarpc/config.json`; the tool will not overwrite broken JSON.
- `CONFIG_UNSUPPORTED_VERSION`: the config file was written by a newer unsupported version.
- `CONNECT_FAILED`: check the configured server address and network route.
- `RPC_TIMEOUT`: increase `timeoutMs` or check provider/network latency.
- unresolved external DTO fields: local source parsing cannot see external jar parents. Exact `paramTypes + orderedArguments` remains available.

## Test

```bash
cd cli && go test ./...
```

Some tests open loopback ports. In restricted sandboxes, they need permission to bind `127.0.0.1`.

## Design Docs

- [Pure-Go runtime](docs/pure-go-runtime.md)
- [Architecture abstraction review](docs/architecture-abstraction-review.md)
- [Install and host setup first-principles design](docs/install-and-host-setup-first-principles.md)

Historical notes:

- [Install and host setup first-principles review](docs/install-and-host-setup-first-principles-review.md)
- [Superseded install and host setup decision](docs/install-and-host-setup.md)
