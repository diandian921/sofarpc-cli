# MCPB Bundle (Claude Desktop one-click install)

`scripts/build-mcpb.sh` produces [MCPB](https://github.com/anthropics/mcpb)
bundles — Anthropic's MCP Bundle format (manifest spec `0.3`) — so the SofaRPC
MCP server can be installed into Claude Desktop by opening a single `.mcpb`
file, with no Go toolchain or manual host-config editing.

This is a *complement* to, not a replacement for, the CLI install paths
(`scripts/install.sh`, `sofarpc install <host>`), which remain the way to
register with the Claude Code and Codex CLIs.

## Bundle layout

Each `.mcpb` is a ZIP, one per platform (native binary, no wrong-arch payload):

```
sofarpc-mcp-<version>-<os>-<arch>.mcpb
├── manifest.json          # spec 0.3, server.type=binary
├── README.md
└── server/
    └── sofarpc[.exe]
```

The manifest launches the binary as `sofarpc mcp`:

```json
"server": {
  "type": "binary",
  "entry_point": "server/sofarpc",
  "mcp_config": { "command": "${__dirname}${/}server${/}sofarpc", "args": ["mcp"], "env": {} }
}
```

## Build

```bash
bash scripts/build-mcpb.sh                       # full 6-platform matrix
PLATFORMS="darwin/arm64" bash scripts/build-mcpb.sh   # one platform
MCPB_VERSION=0.2.0 VERSION=v0.2.0 bash scripts/build-mcpb.sh
```

Default matrix: `darwin/amd64 darwin/arm64 linux/amd64 linux/arm64 windows/amd64 windows/arm64`.
Output lands in `dist/mcpb/` with a `SHA256SUMS`.

`VERSION` stamps the binary (`-X main.BuildVersion`, full `git describe`);
`MCPB_VERSION` is the manifest `version` and must be plain semver, defaulting to
the nearest `vX.Y.Z` tag with the leading `v` stripped.

Packaging uses the official `mcpb` CLI (`npm i -g @anthropic-ai/mcpb`) when it is
on `PATH` — which also validates the manifest — and otherwise falls back to
`zip`. A `.mcpb` is just a ZIP, so both produce an identical, valid bundle; the
CLI is optional and the build needs no Node toolchain.

## Install

Open the matching `sofarpc-mcp-<version>-<os>-<arch>.mcpb` with Claude Desktop;
it shows an install dialog. After installing, add a server with the
`sofarpc_config_save_server` tool, then use `sofarpc_resolve` / `sofarpc_probe`
/ `sofarpc_describe` / `sofarpc_invoke`. Config and cache live under
`~/.sofarpc/` (override with `SOFARPC_HOME`).

## Scope notes

- `user_config` is intentionally empty in v1: the server self-locates
  `~/.sofarpc/` and config-write tools are enabled, so a fresh install works
  with no required setup. Advanced users who want a read-only server can add
  `"--disable-config-write"` to `mcp_config.args`.
- MCPB `compatibility.platforms` only distinguishes OS (`darwin`/`win32`/
  `linux`); CPU arch is carried by the bundled binary and the file name.
