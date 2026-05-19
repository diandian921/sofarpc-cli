# Install and Host Setup First-Principles Review

Status: historical review of an earlier draft of
`docs/install-and-host-setup-first-principles.md`. The implementation has since
landed; keep this document as decision context, not as active requirements.

## Verdict

The first-principles design is the better direction and should become the main
install/setup plan.

The key idea is correct: MCP hosts only need a stable absolute path to a
working `sofarpc-mcp` binary. All acquisition channels should normalize to that
path, and host registration should depend only on it. This removes most of the
upgrade and re-registration complexity instead of managing it.

## What Is Strong

- The canonical `~/.sofarpc/bin/sofarpc-mcp` indirection is the right center of
  the design.
- `sofarpc` + `sofarpc-mcp` is a cleaner final binary pair than
  `sofarpc-cli` + `sofarpc-mcp`.
- `self-install` in Go is preferable to maintaining drifting shell and
  PowerShell install logic.
- Host registration as idempotent convergence is better than blind add/remove.
- Avoiding direct writes to Claude/Codex private config formats is the right
  long-term boundary.
- Upgrade becoming "copy new binaries to the same canonical path" is exactly
  the desired shape.
- `.tar.gz` for macOS/Linux and `.zip` for Windows is the right beta packaging
  split.

## Required Fixes Before Implementation

### 1. Module Path Is Not Yet True

The document uses:

```bash
go install github.com/sofarpc/cli/cmd/sofarpc@latest
go install github.com/sofarpc/cli/cmd/sofarpc-mcp@latest
```

The current remote is `github.com:diandian921/sofarpc-cli.git`, and the module
path in `cli/go.mod` is `github.com/sofarpc/cli`.

Before publishing install instructions, choose one truth:

- migrate the repository/module to the final `github.com/sofarpc/cli` location;
- or update examples to the actual published module path.

Do not ship placeholder or aspirational `go install` commands.

### 2. Host Registration Must Use Expanded Absolute Paths

The design says host registration depends on an absolute path, but examples use
`~/.sofarpc/bin/sofarpc-mcp`.

Do not register `~` with MCP hosts. Hosts launch commands directly and may not
perform shell expansion. Register the fully expanded path:

```text
/Users/<user>/.sofarpc/bin/sofarpc-mcp
```

On Windows, register the fully expanded Windows path to `sofarpc-mcp.exe`.

### 3. Custom `SOFARPC_HOME` Must Be Propagated To Hosts

If a user installs with a non-default `SOFARPC_HOME`, registering only the
binary path is not enough. The MCP process will otherwise start without
`SOFARPC_HOME` and fall back to `~/.sofarpc/config.json`.

When `SOFARPC_HOME` differs from the default, `setup` must register it in the
host environment:

```bash
claude mcp add -e SOFARPC_HOME=/path/to/sofarpc --scope user sofarpc -- /path/to/sofarpc/bin/sofarpc-mcp
codex mcp add --env SOFARPC_HOME=/path/to/sofarpc sofarpc -- /path/to/sofarpc/bin/sofarpc-mcp
```

The default case should not add unnecessary environment noise.

### 4. `go install` Plus `self-install` Has A PATH Ordering Trap

After:

```bash
go install <module>/cmd/sofarpc@vX.Y.Z
```

running:

```bash
sofarpc self-install
```

may execute the old canonical `~/.sofarpc/bin/sofarpc` if that directory appears
before `$GOBIN` or `$GOPATH/bin` in `PATH`.

The documentation and implementation need a safe path:

- tell users to run the newly installed binary by absolute path; or
- make the release bootstrap call the extracted binary directly; or
- later add a dedicated `sofarpc update` flow.

Do not assume `sofarpc self-install` necessarily refers to the just-installed
binary.

### 5. "Never Parse Host Config" Limits Diff Quality

The design wants declarative convergence and a diff when the existing entry is
different. That is correct, but it must respect the "never parse/write foreign
config" invariant.

Codex has structured output:

```bash
codex mcp get sofarpc --json
```

Claude Code may not expose a stable JSON form for `mcp get`. If only human text
is available, do not build brittle text parsing into the product. The rule
should be:

- structured host query available -> compare and print an accurate diff;
- only unstructured output available -> report that the entry exists and require
  `--force`, without pretending to know every field.

## Recommended Document Cleanup

`docs/install-and-host-setup-first-principles.md` should supersede
`docs/install-and-host-setup.md`.

Do one of the following before implementation:

- delete the older document; or
- mark it as superseded at the top and link to the first-principles version.

Two active target-state documents for the same install/setup surface will cause
future implementation drift.

## Suggested Implementation Order

1. Update the first-principles document with the required fixes above.
2. Rename installed command target from `sofarpc-cli` to `sofarpc`.
3. Add `sofarpc self-install` and make shell/PowerShell scripts thin wrappers.
4. Add `sofarpc setup claude|codex|all` using host CLIs only.
5. Add `sofarpc-mcp --selftest`.
6. Rewrite packaging to produce OS-appropriate archives and checksums.

Each step is shippable and testable on its own.
