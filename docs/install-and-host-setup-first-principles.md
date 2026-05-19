# Install and Host Setup — First-Principles Design

Status: implemented beta design.

This document records the implemented install and host-registration shape. It
does not patch `install-and-host-setup.md`; it rederives the design from one
principle and keeps only what follows from it. Written in English to stay
consistent with `README.md` and `CONTEXT.md`.

This revision folds in the fixes from
`install-and-host-setup-first-principles-review.md`. The five review items
are not five patches: four of them (module path, `~`, `SOFARPC_HOME`,
self-install resolution) share one root cause, captured below as a single
corollary.

## The Core Principle

Host registration needs exactly one thing: **a stable absolute path to a
working `sofarpc-mcp` binary**. The host does not care how the binary got
there.

So the whole problem reduces to one sentence:

> Normalize every acquisition channel onto a single canonical path that never
> changes, and make the host depend only on that path.

Everything below is a derivation. The complexity of a naive design — which path
to register, whether upgrade needs re-registration, atomic replace, dual
install scripts — does not arise here because the indirection removes its
cause.

## Corollary: Nothing Symbolic Crosses a Boundary

A "stable path" is only stable if it is *fully resolved*. Any reference that
crosses out to another tool (an MCP host) or into a fresh process (the
just-installed binary) must be a concrete, absolute, install-time value —
never symbolic, ambient, or aspirational.

Four classes are therefore banned at boundaries:

- **Symbolic** — `~/.sofarpc/...`. Hosts `exec` directly; no shell expands
  the tilde.
- **Ambient** — relying on the host inheriting `SOFARPC_HOME` from the user's
  interactive shell. It will not.
- **PATH-resolved** — `sofarpc self-install` resolving the *name* via `PATH`
  may hit an older binary, not the one just acquired.
- **Aspirational** — a module path that does not match the real VCS location,
  so `go install` cannot resolve it.

One rule subsumes all four: *resolve every cross-boundary path to an absolute
value at install time and record it; let the binary recover its own home from
its own location.* Each item below is an application of this rule.

## Canonical Layout

One install root, overridable by `SOFARPC_HOME`, default `~/.sofarpc`:

```text
~/.sofarpc/bin/sofarpc          Human CLI: config, diagnostics, repro, lifecycle.
~/.sofarpc/bin/sofarpc-mcp      Stdio MCP server. THE path hosts register.
~/.sofarpc/cache/schema/        Resolved schema cache.
~/.sofarpc/config.json          User config. Created only when missing.
```

`~/.sofarpc/bin/sofarpc-mcp` is a constant — the indirection layer. Acquisition
writes to it; hosts read it; upgrade overwrites it. Nothing downstream ever
learns a different path.

## Binary Names and Module Paths

Final command names are `sofarpc` and `sofarpc-mcp`. The legacy name
`sofarpc-cli` is dropped (see Implementation Status).

**Decided — Option B.** The Go module lives in the repo's `cli/` subdirectory,
so the module path must include that segment to be resolvable. `cli/go.mod`
declares `module github.com/diandian921/sofarpc-cli/cli` (Go 1.19). The path
is now true today, so `go install` resolves:

```bash
go install github.com/diandian921/sofarpc-cli/cli/cmd/sofarpc@latest
go install github.com/diandian921/sofarpc-cli/cli/cmd/sofarpc-mcp@latest
```

Option A (moving the repo under a `github.com/sofarpc` org) was rejected for
beta: there is no controlled neutral org, and "SOFARPC" is Ant Group's
project/trademark — claiming that namespace without affiliation is a real
risk, not a naming nicety. B keeps the module path honest with zero external
dependency; a permanent path (a vanity import such as `go.sofarpc.dev/cli`)
is deferred to GA and is a one-time, pre-GA migration.

**Subdirectory-module tag rule.** Because the module is under `cli/`, Go
resolves versions from tags prefixed with the module subdirectory:
publish `cli/v0.1.0-beta.2`, not `v0.1.0-beta.2`. Users still write the bare
selector — `go install …/cli/cmd/sofarpc@v0.1.0-beta.2` — and Go maps it to
the `cli/v0.1.0-beta.2` tag. `package.sh`, `install.sh`, and `install.ps1`
strip the `cli/` prefix from `git describe` so archive names and the stamped
version never contain a slash. Pin a tag for reproducible beta testing;
`@latest` for stable. Reproducibility comes from `sofarpc version`
self-reporting the release version: release archives stamp it through
`-ldflags`; `go install module@version` uses the Go module version recorded in
build metadata.

## Acquisition Channels

Two front doors for two populations; both end in the same normalization step,
so they are not two code paths.

```text
go install ──┐
             ├──►  sofarpc self-install  ──►  ~/.sofarpc/bin/
tarball ─────┘
```

- **`go install`** — users with a Go toolchain. Built from source, carries no
  macOS quarantine attribute. Recommended on macOS.
- **Prebuilt tarball** — users without Go. Unsigned during beta, so quarantined
  on macOS; `self-install` handles that.

## Self-Install: Logic in the Binary, Not in Shell

The binary is already cross-platform, so it installs itself:

```bash
sofarpc self-install
```

- create `~/.sofarpc/bin`, `~/.sofarpc/cache/schema`;
- locate `sofarpc-mcp` **as a sibling of the running executable only** — no
  `PATH` fallback (a PATH hit could be an older `sofarpc-mcp`, mixing
  versions). If no sibling exists, error out; an explicit `--mcp-path` is the
  only override;
- **verify the delivery set is consistent**: `sofarpc` and `sofarpc-mcp` ship
  together, so before copying, check both report identical version / build
  metadata. Refuse on mismatch rather than install a half-new pair;
- copy both binaries into `~/.sofarpc/bin`;
- create `~/.sofarpc/config.json` only when missing; never overwrite config,
  cache, or logs;
- on macOS, best-effort strip `com.apple.quarantine` from the copied binaries
  (the running binary already passed Gatekeeper, so it may de-quarantine its
  siblings);
- if `~/.sofarpc/bin` is not on `PATH`, print the exact line to add — without
  mutating shell profiles.

This collapses `install.sh` + `install.ps1` and their drift into one tested Go
path. The shell scripts become ≤10-line bootstraps that download/extract and
call `sofarpc self-install`, or are removed in favor of a README instruction.

### Home Resolution: Self-Locating, Env Only as Fallback

Applying the corollary, the binary recovers its own home instead of depending
on an ambient `SOFARPC_HOME`. Resolution order:

1. explicit `SOFARPC_HOME` if set;
2. else, if the running executable sits in a `bin/` whose parent contains
   `config.json`, that parent is home (the canonical-install case);
3. else, default `~/.sofarpc`.

So a default install needs no environment at all, and a custom-home install
keeps working because the binary physically lives under that home. Step 1
remains for the `go install` case, where the binary is in `GOBIN`, not under
any home.

### The `go install` + `self-install` PATH Trap

After `go install` the new binary is in `GOBIN`/`GOPATH/bin`, but bare
`sofarpc self-install` resolves via `PATH` and may run the *old* binary in
`~/.sofarpc/bin` if that directory comes first — silently reinstalling the
stale version over itself. Mitigation, in priority order:

- the `go install` instructions invoke the new binary by its real install
  location: `GOBIN` if set, else `$(go env GOPATH)/bin`. Concretely
  `BIN="$(go env GOBIN)"; BIN="${BIN:-$(go env GOPATH)/bin}"; "$BIN/sofarpc"
  self-install`. `go env GOBIN` is empty unless set, so the fallback is
  required;
- the tarball bootstrap calls the extracted binary directly, never a
  PATH-resolved name;
- `self-install` compares source vs installed target by version + build hash:
  same → no-op; newer → install; **older → not silently blocked** but gated
  behind `--allow-downgrade` (or `--force`) so an intentional downgrade is
  still possible while the PATH trap stays caught.

Never assume `sofarpc self-install` refers to the just-acquired binary.

## Host Setup: Declarative and Idempotent

Registration is convergence to a desired state, not imperative add/remove:

```bash
sofarpc setup claude
sofarpc setup codex
sofarpc setup all
```

The desired entry is resolved at run time, not symbolic: name `sofarpc`,
command the **fully expanded absolute path** to `sofarpc-mcp` (e.g.
`/Users/<user>/.sofarpc/bin/sofarpc-mcp`, or the Windows path to
`sofarpc-mcp.exe`) — never `~/...`. When home is non-default, the entry also
carries `SOFARPC_HOME` as a host-registered env var so the host does not rely
on inheriting it; the default case adds no env. `setup` reads the host's
current entry. Convergence is **host-conditional**, because "equal → no-op"
requires reliably knowing the current entry, which only a structured read-back
gives:

- absent → add (both hosts);
- **Codex** (`mcp get --json`) → true idempotence: equal → no-op; different →
  diff and apply only with `--force`;
- **Claude** (no JSON, human text only) → conservative: present → *cannot
  confirm equality without parsing human text*, so do not mutate; report it
  exists and require `--force`, where `--force` performs remove + add. No
  silent "equal → no-op" claim is made for Claude.

So `setup` is fully idempotent on Codex and existence-safe on Claude. Repeated
runs are safe and are the expected upgrade behavior. Flags:

```text
--dry-run     Print the diff and planned host command. Mutate nothing.
--force       Replace an entry that exists but does not match desired.
--name        MCP server name. Default sofarpc.
--claude-scope user|local|project   Default user (this is a global dev tool).
```

`--force` is needed only for the genuine name-collision case. The "already
exists, fail" and non-atomic remove/add problems of an imperative design do not
occur, because convergence never blindly removes.

## Hard Invariant: Never Parse or Write Foreign Config

`setup` only ever (a) invokes the host's own MCP CLI, or (b) prints a
copy-pasteable snippet and exits non-zero with a clear message. Paths are
fully expanded; `SOFARPC_HOME` is passed only when non-default:

```bash
# default home
claude mcp add --scope user sofarpc -- /Users/me/.sofarpc/bin/sofarpc-mcp
codex  mcp add               sofarpc -- /Users/me/.sofarpc/bin/sofarpc-mcp

# custom SOFARPC_HOME=/data/sofarpc
claude mcp add -e SOFARPC_HOME=/data/sofarpc --scope user sofarpc -- /data/sofarpc/bin/sofarpc-mcp
codex  mcp add --env SOFARPC_HOME=/data/sofarpc            sofarpc -- /data/sofarpc/bin/sofarpc-mcp
```

Writing a config format we do not own is permanent debt, not a future feature.
This is an invariant, not a "for now". The snippet fallback triggers when the
host CLI is missing, the command fails, or its output is unrecognized — not
only when the CLI is absent.

### Diff Fidelity Is Bounded by the Host

Declarative convergence wants a diff, but the never-parse invariant means the
diff can only be as good as the host's structured read-back. Verified:

- **Codex** — `codex mcp get sofarpc --json` exists → compare fields and print
  an accurate diff.
- **Claude Code** — `claude mcp get sofarpc` has no JSON form, only human
  text → do not build brittle text parsing. Report "entry exists and differs"
  and require `--force`, without claiming to know each field.

Never parse human-formatted host output to fake precision.

## Verification: Two Layers

After convergence, `setup` verifies both layers:

```bash
claude mcp get sofarpc        # config layer: host sees the entry
codex  mcp get sofarpc
sofarpc-mcp --selftest        # binary layer: server initializes, exits 0
```

`--selftest` brings up the server machinery and exits without serving stdio. A
config pointing at a broken binary fails here, not at first agent use. Setup
never starts an interactive agent session.

## Upgrade Is a Non-Event

Derived, not a separate feature: re-acquire, run `self-install` to overwrite
`~/.sofarpc/bin/*`. The host config is unchanged because it is an indirection.
Re-running `setup` is a verified no-op. No upgrade-specific logic exists.

## Release Packaging (Target)

```text
sofarpc-vX.Y.Z-darwin-arm64.tar.gz
sofarpc-vX.Y.Z-darwin-amd64.tar.gz
sofarpc-vX.Y.Z-linux-amd64.tar.gz
sofarpc-vX.Y.Z-windows-amd64.zip
SHA256SUMS
```

`.tar.gz` for macOS/Linux preserves the executable bit; `.zip` is native on
Windows. Each archive contains `sofarpc`, `sofarpc-mcp`, `README.md`, and the
bootstrap scripts. No `.pkg`/`.msi`/notarization during beta.

## Implementation Status

The beta implementation now matches this target shape:

- **Module-path decision — done (Option B).** `cli/go.mod` is
  `github.com/diandian921/sofarpc-cli/cli` (the module lives in the repo's
  `cli/` subdirectory, so the path carries that segment); all internal
  imports rewritten. The `go install` channel is now advertisable alongside
  the tarball.
- **Command rename — done.** The installed human CLI is `sofarpc`; the MCP
  server is `sofarpc-mcp`. No `sofarpc-cli` compatibility shim is shipped for
  beta.
- **Self-install — done.** `sofarpc self-install` owns the cross-platform
  install logic, verifies the `sofarpc`/`sofarpc-mcp` delivery set, and creates
  config/cache scaffolding without overwriting user config.
- **Canonical host setup — done.** `sofarpc setup claude|codex|all` registers
  the expanded absolute path to `sofarpc-mcp`, propagates non-default
  `SOFARPC_HOME`, and uses host CLIs rather than parsing or writing host
  config files.
- **Binary self-test — done.** `sofarpc-mcp --selftest` verifies that the MCP
  server can initialize and enumerate tools without starting a stdio session.
- **Packaging rewrite — done.** `scripts/package.sh` emits the cross-platform
  release matrix, uses `.tar.gz` for macOS/Linux and `.zip` for Windows,
  includes `README.md` plus bootstrap scripts, and writes `SHA256SUMS`.

## Non-Goals

- no daemon or service manager;
- no automatic shell profile mutation;
- no host registration from acquisition/`self-install` by default;
- no parsing or writing of host config formats, ever;
- no platform-native installers or notarization during beta.
