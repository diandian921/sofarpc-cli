# SofaRPC MCP

MCP-first SofaRPC testing toolkit for agents.

The primary entrypoint is `sofarpc-mcp`, a stdio MCP server. `sofarpc-cli` is kept for human configuration, diagnostics, and exact reproduction. Invocation can run through the stable Java Engine, or through the experimental pure-Go direct BOLT/Hessian2 engine for lower-latency direct calls.

## What Gets Installed

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

`config.json`, token, logs, state, and cache are not overwritten on upgrade.

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
sofarpc-engine.jar
install.sh
install.ps1
```

Requirements: Go 1.19+, Maven 3.6+, Java 8+.

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

- `engine_status`
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
  "engine": "go",
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
  },
  "engine": {
    "mode": "java",
    "host": "127.0.0.1",
    "port": 37651,
    "javaHome": null,
    "idleTTL": "30m",
    "startTimeoutMs": 20000,
    "maxConcurrentInvokes": 8
  }
}
```

`engine.mode` accepts:

- `java`: use the shared Java Engine. This is the default compatibility path.
- `go`: use the pure-Go direct BOLT/Hessian2 path for invoke.
- `auto`: try the pure-Go direct path for invoke, then fall back to Java when the direct route cannot produce a response.

## CLI

Use CLI for setup and diagnostics:

```bash
sofarpc-cli project add user /Users/me/workspace/user-service --prefix com.company.user
sofarpc-cli server add user-test 10.0.0.1:12200 --project user
sofarpc-cli server list --json
sofarpc-cli daemon status
sofarpc-cli daemon stop
```

The legacy `exec --stdin`, `ping`, and `invoke` commands remain available for reproduction.

For direct invoke reproduction without starting the Java Engine:

```bash
sofarpc-cli invoke --engine go \
  --address 10.0.0.1:12200 \
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

- `java_not_found`: install Java 8+ or fix `PATH`.
- `java_version_unsupported`: use Java 8 or newer.
- `engine_jar_not_found`: run `./scripts/install.sh` or set `SOFARPCD_JAR`.
- `port_occupied`: port `37651` is already used by a non-reusable local process.
- `engine_start_timeout`: inspect `~/.sofarpc/logs/engine.log`; the MCP/CLI error includes a bounded log tail when available.
- `CONFIG_INVALID`: fix `~/.sofarpc/config.json`; the tool will not overwrite broken JSON.
- unresolved external DTO fields: local source parsing cannot see external jar parents. Exact `paramTypes + orderedArguments` remains available.

## Test

```bash
mvn -f daemon/pom.xml test
cd cli && go test ./...
```

Some tests open loopback ports. In restricted sandboxes, they need permission to bind `127.0.0.1`.

## Design Docs

- [MCP-first design](docs/mcp-first-sofarpc-agent-design.md)
- [Implementation plan](docs/mcp-first-implementation-plan.md)
- [Design review](docs/mcp-first-sofarpc-agent-design-review.md)
