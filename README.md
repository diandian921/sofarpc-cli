# SofaRPC MCP

MCP-first SofaRPC testing toolkit for agents.

The primary entrypoint is `sofarpc-mcp`, a stdio MCP server. `sofarpc-cli` is kept for human configuration, diagnostics, and exact reproduction. Invocation runs through a pure-Go direct BOLT/Hessian2 runtime; no Java process or sidecar is required.

## What Gets Installed

```text
~/.sofarpc/
  bin/
    sofarpc-mcp
    sofarpc-cli
  config.json
  logs/
  cache/
    schema/
```

`config.json`, logs, and cache are not overwritten on upgrade.

## Install

From source:

```bash
./scripts/install.sh
export PATH="$HOME/.sofarpc/bin:$PATH"
```

Build a release package:

```bash
./scripts/package.sh
```

The package contains:

```text
sofarpc-mcp
sofarpc-cli
install.sh
install.ps1
```

Requirements: Go 1.19+ when building from source. Release packages contain prebuilt binaries.

## MCP Configuration

Example MCP server command:

```json
{
  "mcpServers": {
    "sofarpc": {
      "command": "~/.sofarpc/bin/sofarpc-mcp"
    }
  }
}
```

To hide config write tools:

```json
{
  "mcpServers": {
    "sofarpc": {
      "command": "~/.sofarpc/bin/sofarpc-mcp",
      "args": ["--disable-config-write"]
    }
  }
}
```

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

Assertions are intentionally not part of `sofarpc_invoke`. For assertion-based exact reproduction, use `sofarpc-cli exec --stdin` or the CLI `invoke` command.

## Config File

`~/.sofarpc/config.json` is stable and user-editable:

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
  }
}
```

## CLI

Use CLI for setup and diagnostics:

```bash
sofarpc-cli project add user /Users/me/workspace/user-service --prefix com.company.user
sofarpc-cli server add user-test 10.0.0.1:12200 --project user
sofarpc-cli server list --json
```

The `exec --stdin`, `ping`, and `invoke` commands are available for exact reproduction.

For invoke reproduction:

```bash
sofarpc-cli invoke \
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

The pure-Go runtime covers direct BOLT generic invocation and the common Hessian2 value shapes used by DTO-style requests and responses. Declared Java argument and DTO field types are used for numeric encoding, so values such as `Integer`, `Long`, and `Double` do not depend on Go's JSON number shape.

Known limits:

- object reference preservation is not implemented for request encoding; cyclic request values are rejected.
- `java.util.Date`, `byte[]`, complex enum payloads, and provider-specific Hessian extensions need dedicated compatibility tests before relying on them broadly.
- map keys are flattened to strings in the normal `result`; use `rawResult=true` when key type matters during diagnosis.

## Security Boundaries

`sofarpc-mcp` is a local developer tool. Treat stdout as the JSON-RPC protocol stream; diagnostics and future logging must go to stderr. `sofarpc_probe` can dial an explicit address for diagnostics, so prefer configured servers when running against untrusted agent input.

Each JSON-RPC stdin frame is capped at 128 MiB. Oversized frames are rejected with a parse error and the server continues reading subsequent frames.

## Troubleshooting

- `CONFIG_INVALID`: fix `~/.sofarpc/config.json`; the tool will not overwrite broken JSON.
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
