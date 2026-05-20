# Single Binary Install Target

Status:

- **Step 1 — fold to a single binary** — implemented.
  `sofarpc-mcp` no longer exists; `sofarpc mcp` is the stdio subcommand.
- **Step 2 — `sofarpc install <host>` high-level verb** — implemented.
  Chains `self-install` then `setup <host>`; tested.
- **Step 3 — network `scripts/install.sh` (curl one-liner) + Windows
  `scripts/install.ps1` + tarball cleanup** — implemented.
  Bootstrap downloads the matching release archive, verifies SHA256,
  extracts, and runs `./sofarpc install <host>`. The tarball no longer
  ships an in-archive install script; users run `./sofarpc install` directly.
- **Release prerequisite** — pending. The bootstrap resolves
  `releases/latest` via redirect, so it only works once a plain `vX.Y.Z`
  GitHub Release with attached archives + SHA256SUMS exists on a
  root-module commit.

## Goal

Make installation understandable as one idea:

```text
install sofarpc
register sofarpc with Codex or Claude
```

Users should not need to know that the product has an MCP server mode, a
self-install step, or host-specific registration details.

## Command Shape

The single binary owns both human CLI workflows and MCP serving:

```text
sofarpc
  invoke
  ping
  project add|list|remove
  server add|list|remove
  config / doctor / version
  self-install
  setup codex|claude|all
  install codex|claude|all
  mcp
```

Human CLI usage:

```bash
sofarpc server list
sofarpc ping salesfundmp-test
sofarpc invoke ...
```

MCP host usage:

```bash
~/.sofarpc/bin/sofarpc mcp
```

Host registration should store:

```text
command = /absolute/path/to/.sofarpc/bin/sofarpc
args    = ["mcp"]
```

It should not register a separate `sofarpc-mcp` binary.

## Supported Install Paths

### 1. One-line Script, Main User Path

macOS and Linux:

```bash
curl -fsSL https://raw.githubusercontent.com/diandian921/sofarpc-cli/main/scripts/install.sh | bash -s -- codex
```

Claude:

```bash
curl -fsSL https://raw.githubusercontent.com/diandian921/sofarpc-cli/main/scripts/install.sh | bash -s -- claude
```

Both:

```bash
curl -fsSL https://raw.githubusercontent.com/diandian921/sofarpc-cli/main/scripts/install.sh | bash -s -- all
```

Pinned version:

```bash
curl -fsSL https://raw.githubusercontent.com/diandian921/sofarpc-cli/main/scripts/install.sh | bash -s -- --version v0.1.0-beta.5 codex
```

Expected script behavior:

```text
detect OS and architecture
resolve version, defaulting to latest release
download the matching release archive
verify SHA256SUMS
extract sofarpc
run ./sofarpc install codex|claude|all
```

The script is only an acquisition bootstrap. It should not know host config
formats and should not duplicate install logic.

### 2. Go Install, Developer Path

```bash
go install github.com/diandian921/sofarpc-cli/cmd/sofarpc@v0.1.0-beta.5
sofarpc install codex
```

Other targets:

```bash
sofarpc install claude
sofarpc install all
```

This path installs only `sofarpc`. There is no separate:

```bash
go install .../cmd/sofarpc-mcp
```

### 3. Manual Release Package, Offline Path

macOS and Linux:

```bash
tar -xzf sofarpc-v0.1.0-beta.5-darwin-arm64.tar.gz
cd sofarpc-v0.1.0-beta.5-darwin-arm64
./sofarpc install codex
```

Windows:

```powershell
Expand-Archive sofarpc-v0.1.0-beta.5-windows-amd64.zip
cd sofarpc-v0.1.0-beta.5-windows-amd64
.\sofarpc.exe install codex
```

If the user wants to install the binary without registering a host:

```bash
./sofarpc self-install
```

## Installed Layout

The canonical install layout becomes:

```text
~/.sofarpc/
  bin/
    sofarpc
  config.json
  cache/
```

`config.json` and `cache/` are preserved across upgrades.

## Responsibility Boundaries

### `install.sh`

Target role:

```text
remote bootstrap for macOS and Linux users
downloads a release archive
verifies checksums
executes ./sofarpc install <target>
```

It should not:

```text
write Codex or Claude config directly
contain duplicated self-install logic
know private host config formats
```

### `sofarpc install <target>`

High-level user command. It should compose the lower-level commands:

```text
self-install
mcp self-test
host setup
```

For example:

```bash
sofarpc install codex
```

does:

```text
copy the running sofarpc into ~/.sofarpc/bin/sofarpc
run ~/.sofarpc/bin/sofarpc mcp --selftest
register Codex to launch ~/.sofarpc/bin/sofarpc mcp
```

### `sofarpc self-install`

Low-level binary placement. It only installs the binary:

```text
copy current sofarpc to ~/.sofarpc/bin/sofarpc
preserve config.json
preserve cache/
print PATH hint if needed
```

It should not register Codex or Claude.

### `sofarpc setup <target>`

Low-level host registration. It only tells the host where the MCP server is:

```text
command = ~/.sofarpc/bin/sofarpc
args    = ["mcp"]
```

It should continue to use host CLIs instead of writing private host config
formats directly.

### `sofarpc mcp`

Runs the stdio MCP server.

Self-test moves from:

```bash
sofarpc-mcp --selftest
```

to:

```bash
sofarpc mcp --selftest
```

## What Disappears

The target state removes these user-visible concepts:

```text
sofarpc-mcp as a separate binary
go install of two binaries
pair-version validation between sofarpc and sofarpc-mcp
registering a direct path to sofarpc-mcp
```

The internal MCP server still exists. Only the binary boundary changes.

## Migration Notes

During migration, `self-install` may remove legacy binaries:

```text
~/.sofarpc/bin/sofarpc-mcp
~/.sofarpc/bin/sofarpc-cli
```

Existing host registrations that point at `sofarpc-mcp` should be replaced by:

```text
command = ~/.sofarpc/bin/sofarpc
args    = ["mcp"]
```

`sofarpc setup --force codex|claude|all` can own that replacement.

## Non-goals

Do not change the command name yet. This target keeps:

```text
sofarpc
```

Do not introduce platform-native installers during beta:

```text
.pkg
.msi
notarized app bundles
```

Do not make `install.sh` the source of truth. The source of truth is the Go
binary command:

```bash
sofarpc install <target>
```
