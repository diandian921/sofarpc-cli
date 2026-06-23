# SofaRPC Config Profile Design Plan

> Status (2026-06-23): implemented on branch `feat/profile-config`.
> The first delivery includes v2 config load/save, v1 compatibility, derived
> `<project>-<profile>` server view, `activeProfile` resolution, MCP `profile`
> arguments, CLI `server add --profile`, tests, and README/skill updates.

## Background

Current `config.json` stores RPC endpoints as a flat `servers` map:

```json
{
  "servers": {
    "salesfundmp-local": {
      "address": "127.0.0.1:12300",
      "project": "salesfundmp"
    },
    "salesfundmp-test": {
      "address": "10.74.194.40:12200",
      "project": "salesfundmp"
    }
  }
}
```

This works, but the environment is only encoded in the server name. The tool cannot reliably tell that `local` and `test` are profiles of the same project; it can only infer that from a naming convention.

The target direction is to borrow the Spring profile idea: each project owns a set of named profiles, and each project can declare its own `activeProfile`.

## Goals

- Make profile/environment explicit instead of hiding it in server names.
- Keep per-project defaults clear and avoid repeating common fields such as `protocol`, `timeoutMs`, `appName`, and `attachments`.
- Let each project choose its own `activeProfile`.
- Preserve backward compatibility with existing v1 flat `servers`.
- Preserve old invocation style by server name during migration.
- Add a cleaner new invocation style: `project + profile`.

## Non-Goals

- Do not remove support for `server` names immediately.
- Do not put profile metadata into SofaRPC `attachments`; attachments are sent with RPC requests and are not local config metadata.
- Do not make one global `activeProfile`; projects may have different default profiles.

## Target Config Format

Proposed schema version: `2`.

```json
{
  "version": 2,
  "defaults": {
    "protocol": "bolt",
    "timeoutMs": 5000,
    "appName": "sofarpc-agent",
    "attachments": {}
  },
  "projects": {
    "fundsalesmrksupport": {
      "activeProfile": "test",
      "workspaceRoot": "/Users/wuweihua/workspace/thfundworkspace/fundsalesmrksupport",
      "servicePrefixes": [
        "com.thfund.sales.fundsalesmrksupport.facade."
      ],
      "profiles": {
        "local": {
          "address": "127.0.0.1:12201"
        },
        "test": {
          "address": "10.74.194.42:12200"
        }
      }
    },
    "salesfundmp": {
      "activeProfile": "test",
      "workspaceRoot": "/Users/wuweihua/workspace/thfundworkspace/salesfundmp",
      "servicePrefixes": [
        "com.thfund.salesfundmp.facade."
      ],
      "profiles": {
        "local": {
          "address": "127.0.0.1:12300"
        },
        "test": {
          "address": "10.74.194.40:12200"
        }
      }
    }
  }
}
```

## Field Semantics

### `defaults`

Global defaults for RPC endpoint fields:

- `protocol`: default transport protocol. Current default is `bolt`.
- `timeoutMs`: default total RPC timeout.
- `appName`: SofaRPC consumer app name sent as request metadata. Current default is `sofarpc-agent`.
- `attachments`: static request attachments. Values should still be redacted in MCP output.

Project profiles inherit these fields.

### `projects.<name>.activeProfile`

Default profile for the project when the caller specifies a project but no profile.

Example:

```json
{
  "project": "salesfundmp"
}
```

resolves to:

```text
salesfundmp + salesfundmp.activeProfile
```

### `projects.<name>.profiles`

Named endpoint profiles under a project. Each profile must include `address`. It may override any endpoint default:

```json
{
  "profiles": {
    "test": {
      "address": "10.74.194.40:12200",
      "timeoutMs": 10000
    }
  }
}
```

## Resolution Rules

Priority order:

1. Explicit `address` wins and bypasses configured project/profile endpoint resolution.
2. Explicit `server` resolves through the compatibility server view.
3. Explicit `project + profile` resolves that project profile.
4. Explicit `project` with no `profile` resolves the project's `activeProfile`.
5. If only one project/profile endpoint exists, it may be inferred for backward-compatible convenience.
6. Otherwise return `ENDPOINT_NOT_FOUND` with project/profile candidates.

## Compatibility Model

Keep an in-memory compatibility view:

```go
Config.Servers map[string]Server
```

For v2 config, this view is derived from project profiles:

```text
<project>-<profile>
```

Examples:

- `salesfundmp` + `local` -> `salesfundmp-local`
- `salesfundmp` + `test` -> `salesfundmp-test`
- `fundsalesmrksupport` + `test` -> `fundsalesmrksupport-test`

This lets existing code paths and old calls keep working:

```json
{
  "server": "salesfundmp-test"
}
```

while enabling the new form:

```json
{
  "project": "salesfundmp",
  "profile": "test"
}
```

## Migration Strategy

### Phase 1: Read v1 and v2

- Bump supported config version to `2`.
- Continue reading v1 flat `servers`.
- Add v2 fields:
  - top-level `defaults`
  - project-level `activeProfile`
  - project-level `profiles`
- During `Load`, normalize both formats into a common runtime view.

### Phase 2: Write v2

- New saves should write v2 profile format.
- Existing `sofarpc_config_save_server` can remain as a compatibility tool but internally map to `project + profile`.
- Add or extend config write support with `profile`.

Suggested behavior for existing save server:

```json
{
  "name": "salesfundmp-test",
  "project": "salesfundmp",
  "profile": "test",
  "address": "10.74.194.40:12200"
}
```

If `profile` is omitted, infer it from `name` only when `name` matches `<project>-<profile>`. Otherwise require `profile`.

### Phase 3: Prefer Profile APIs

Update MCP schemas and docs so agents prefer:

```json
{
  "project": "salesfundmp",
  "profile": "test"
}
```

but keep:

```json
{
  "server": "salesfundmp-test"
}
```

as a stable compatibility path.

## API Surface Changes

### `sofarpc_resolve`

Add optional `profile`:

```json
{
  "project": "salesfundmp",
  "profile": "test"
}
```

Response should include both compatibility server and profile metadata:

```json
{
  "project": "salesfundmp",
  "profile": "test",
  "server": "salesfundmp-test",
  "endpoint": {
    "address": "10.74.194.40:12200",
    "protocol": "bolt",
    "timeoutMs": 5000,
    "appName": "sofarpc-agent",
    "attachments": {}
  }
}
```

### `sofarpc_invoke` and `sofarpc_invoke_plan`

Add optional `profile`:

```json
{
  "project": "salesfundmp",
  "profile": "test",
  "service": "com.thfund.salesfundmp.facade.SomeFacade",
  "method": "query",
  "arguments": {}
}
```

Validation:

- If both `server` and `profile` are provided, they must resolve to the same project/profile.
- If `profile` is provided, `project` should be required unless `server` is also provided.

### `sofarpc_probe`

Add optional `profile`, with the same resolution rules as invoke.

### `sofarpc_config_list`

Return projects with profile metadata:

```json
{
  "projects": [
    {
      "name": "salesfundmp",
      "project": {
        "activeProfile": "test",
        "workspaceRoot": "...",
        "servicePrefixes": ["..."],
        "profiles": {
          "local": {
            "address": "127.0.0.1:12300"
          },
          "test": {
            "address": "10.74.194.40:12200"
          }
        }
      }
    }
  ],
  "servers": [
    {
      "name": "salesfundmp-test",
      "server": {
        "project": "salesfundmp",
        "profile": "test",
        "address": "10.74.194.40:12200"
      }
    }
  ]
}
```

The `servers` array stays as a compatibility view for agents that already consume it.

## Validation Rules

- Project names keep the existing name rule.
- Profile names should use the same name rule as server names: `^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$`.
- `activeProfile`, when present, must exist in the project's `profiles`.
- Every profile must have a valid `host:port` address.
- Defaults are applied in order:
  1. built-in defaults
  2. top-level `defaults`
  3. project profile overrides
  4. per-call overrides such as `timeoutMs`
- `attachments` should merge rather than replace only if we explicitly need project-level attachments later. For now, top-level defaults plus profile override is enough.

## Implementation Steps

1. Extend `internal/appconfig` types:
   - `Config.Defaults`
   - `Project.ActiveProfile`
   - `Project.Profiles`
   - `EndpointDefaults` or equivalent shared endpoint struct
   - `Profile` endpoint struct
2. Bump `CurrentConfigVersion` to `2`.
3. Update `Load`:
   - accept v1 and v2
   - apply defaults
   - derive `Config.Servers` from v2 profiles
   - keep v1 `servers` support
4. Update `Save`:
   - write v2 shape
   - avoid persisting derived `servers` for pure v2 configs
5. Add resolver support for `profile` in app layer:
   - `ResolveInput.Profile`
   - `InvocationInput.Profile`
   - `ProbeInput.Profile`
6. Update MCP argument schemas:
   - `resolve`
   - `probe`
   - `invoke`
   - `invoke_plan`
   - config save tools
7. Update CLI:
   - `server add` accepts `--profile`
   - `server list` displays project/profile/address
   - `ping` continues accepting server name; optionally add `--project --profile` later
8. Update docs and examples.
9. Add tests for v1 compatibility and v2 profile behavior.

## Test Plan

### App Config Tests

- Load missing config returns v2 defaults.
- Load v1 flat `servers` still works.
- Load v2 profile config derives compatibility servers.
- Reject unknown future version.
- Reject profile config whose `activeProfile` does not exist.
- Reject profile without address.
- Save v2 does not write derived `servers` unless intentionally supporting mixed mode.

### Resolution Tests

- `project + profile` resolves expected endpoint.
- `project` only resolves project `activeProfile`.
- `server` resolves derived compatibility server.
- `server + profile` mismatch is rejected.
- Ambiguous project without active profile returns candidates.

### MCP Tests

- `sofarpc_config_list` returns profiles and compatibility servers.
- `sofarpc_invoke_plan` accepts `profile`.
- `sofarpc_probe` accepts `profile`.
- Attachment values remain redacted in all profile/server/endpoint output.

### CLI Tests

- Existing `sofarpc ping salesfundmp-test` still resolves.
- `server list --json` includes profile metadata.
- `server add --project salesfundmp --profile test ...` writes v2 profile shape.

## Decisions Taken

1. Should top-level `defaults.attachments` merge with profile `attachments`, or should profile `attachments` replace defaults?

   Decision: profile endpoint fields override top-level defaults. Merge semantics can be added later if there is a concrete use case.

2. Should `activeProfile` be required when a project has multiple profiles?

   Decision: yes. It removes ambiguity and matches the Spring active profile mental model.

3. Should v2 allow top-level `servers` as a manual override?

   Decision: no for new writes. Accept flat `servers` for v1 compatibility and derive the compatibility view for v2.

4. Should profile name be called `profile` or `env`?

   Decision: `profile`. It matches the Spring mental model and leaves room for non-environment profiles later.

## Implemented Slice

- v2 config types and validation.
- v2 load/save with v1 flat `servers` compatibility.
- Derived compatibility server names in the shape `<project>-<profile>`.
- `profile` in resolve/probe/invoke/invoke_plan inputs.
- MCP config save support for `profile`.
- CLI `server add --profile` and profile-aware `server list`.
- Focused tests for config loading, profile resolution, MCP view redaction, and CLI save behavior.
