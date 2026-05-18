# SofaRPC MCP

MCP-first SofaRPC testing toolkit for agents.

The primary entrypoint is `sofarpc-mcp`, a stdio MCP server. `sofarpc-cli` is kept for human configuration, diagnostics, and exact reproduction. Invocation runs through a pure-Go direct BOLT/Hessian2 runtime; no Java process or sidecar Engine is required.

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

Config and context:

- `list_projects`
- `add_project`
- `remove_project`
- `set_current_project`
- `list_servers`
- `add_server`
- `remove_server`

Runtime and RPC:

- `ping_service`
- `search_interface`
- `describe_interface`
- `invoke_method`

`ping_service` checks the configured transport path. It does not prove the remote interface, method, or business behavior exists.

`invoke_method` supports either exact low-level arguments:

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

Schema cache is stored under `~/.sofarpc/cache/schema/` and invalidated by source-set fingerprint. Entries unused for 7 days may be cleaned.

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
