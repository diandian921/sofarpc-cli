# Install and Host Setup Decision

> **SUPERSEDED.** This document is kept only for decision history. The active
> target-state design is
> [`install-and-host-setup-first-principles.md`](./install-and-host-setup-first-principles.md),
> which rederives this from a single principle and folds in the review fixes.
> Do not implement from this file.

Status: superseded by the first-principles design.

This document fixes the intended distribution, binary naming, and MCP host
registration shape before changing code. It is deliberately narrow: install
binaries, then optionally register the MCP server with Claude Code and Codex.

## Binary Names

Final user-facing binaries:

```text
sofarpc       Human CLI for config, diagnostics, invoke reproduction, setup.
sofarpc-mcp   Stdio MCP server launched by agent hosts.
```

`sofarpc-cli` should not be the final installed command name. It is redundant
for users and makes `go install` output awkward. The repository name may remain
`sofarpc-cli` temporarily, but the command name should be `sofarpc`.

Target Go command directories:

```text
cli/cmd/sofarpc
cli/cmd/sofarpc-mcp
```

If the published module path changes, examples must be updated at release time.
Use pinned beta tags for reproducible beta testing and `@latest` for normal
stable install:

```bash
go install <module>/cmd/sofarpc@v0.1.0-beta.2
go install <module>/cmd/sofarpc-mcp@v0.1.0-beta.2

go install <module>/cmd/sofarpc@latest
go install <module>/cmd/sofarpc-mcp@latest
```

## Release Package Format

Preferred release artifacts:

```text
sofarpc-vX.Y.Z-darwin-arm64.tar.gz
sofarpc-vX.Y.Z-darwin-amd64.tar.gz
sofarpc-vX.Y.Z-linux-amd64.tar.gz
sofarpc-vX.Y.Z-windows-amd64.zip
SHA256SUMS
```

Use `.tar.gz` for macOS/Linux because it preserves executable bits naturally.
Use `.zip` for Windows because it is the native expectation there.

Package contents:

```text
sofarpc / sofarpc.exe
sofarpc-mcp / sofarpc-mcp.exe
install.sh
install.ps1
README.md
```

Do not introduce `.pkg`, `.msi`, or a custom executable installer for the beta.
Those formats add signing, notarization, uninstall, and platform-specific
maintenance before the install contract has stabilized.

## Install Script Responsibility

`install.sh` and `install.ps1` should only install local files:

- create `~/.sofarpc/bin`;
- create `~/.sofarpc/cache/schema`;
- create `~/.sofarpc/config.json` only when missing;
- copy `sofarpc` and `sofarpc-mcp` into the bin directory;
- never overwrite user config, cache, or logs.

They should not silently register MCP servers with Claude or Codex. Host
registration is a separate action because it mutates another tool's config.

## Host Setup Command

Add host registration to the CLI:

```bash
sofarpc setup claude
sofarpc setup codex
sofarpc setup all
```

Recommended flags:

```text
--name sofarpc                  MCP server name.
--mcp-command <path>            Defaults to the installed sofarpc-mcp path.
--disable-config-write          Register MCP with this server flag.
--dry-run                       Print planned commands/config only.
--force                         Replace an existing server of the same name.
```

Claude-specific flag:

```text
--claude-scope user|local|project
```

Default Claude scope should be `user`, not Claude's CLI default `local`,
because this setup command is for a globally installed developer tool.

## Registration Strategy

Prefer each host's official CLI over editing config files directly.

Claude Code:

```bash
claude mcp add --scope user sofarpc -- /absolute/path/to/sofarpc-mcp
```

Codex:

```bash
codex mcp add sofarpc -- /absolute/path/to/sofarpc-mcp
```

With config writes disabled:

```bash
claude mcp add --scope user sofarpc -- /absolute/path/to/sofarpc-mcp --disable-config-write
codex mcp add sofarpc -- /absolute/path/to/sofarpc-mcp --disable-config-write
```

If the host CLI is not installed, do not guess private config formats. Print a
manual JSON/TOML snippet and return a clear error. Direct file editing may be
added later only if the host config contract is stable and covered by tests.

## Replacement Behavior

If a server named `sofarpc` already exists:

- without `--force`, report the existing entry and fail without mutation;
- with `--force`, remove the existing entry through the host CLI and add the new
  one.

This avoids accidental replacement of a user-customized MCP configuration.

## Post-Setup Verification

After registration, the command should verify the host can see the server:

```bash
claude mcp get sofarpc
codex mcp get sofarpc --json
```

Do not start an interactive agent session as part of setup. The setup command
only verifies the host configuration layer.

## Non-Goals

- no daemon or service manager;
- no automatic shell profile mutation;
- no automatic Claude/Codex registration from `install.sh` by default;
- no direct edits to unknown host config formats;
- no platform-native installers during beta.
