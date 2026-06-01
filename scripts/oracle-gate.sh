#!/usr/bin/env bash
# Pre-release gate for the hand-written Hessian2 / BOLT codec.
#
# It verifies the codec against REAL oracles:
#   - Hessian: a JVM running the alipay Hessian library (needs java/javac + the
#     alipay Hessian jar in ~/.m2).
#   - BOLT: the official github.com/sofastack/sofa-bolt-go library (pure Go).
#
# CRITICAL: the Hessian oracle calls t.Skip() when the JVM or the alipay jar is
# absent, and a skipped Go test still exits 0 — which would fake a "pass" without
# verifying anything. This gate treats a SKIPPED oracle as a FAILURE, so a green
# result here always means the codec was actually checked against a real oracle.
#
# Usage: bash scripts/oracle-gate.sh   (run before cutting a release)
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

# Ensure a writable Go module cache. Some environments default GOMODCACHE to a
# read-only path, which makes `go test` fail while fetching deps (e.g. the BOLT
# oracle's sofa-bolt-go) with "mkdir ...: read-only file system". Fall back to a
# stable temp cache so the gate runs rather than failing on infrastructure.
default_modcache="$(go env GOMODCACHE)"
if ! { mkdir -p "$default_modcache" 2>/dev/null && [ -w "$default_modcache" ]; }; then
	fallback="${TMPDIR:-/tmp}/sofarpc-oracle-gocache"
	export GOPATH="$fallback"
	export GOMODCACHE="$fallback/pkg/mod"
	mkdir -p "$GOMODCACHE"
	echo "note: default Go module cache is not writable; using $GOMODCACHE"
	echo
fi

fail=0

run_oracle() {
	local tag="$1" runexpr="$2" label="$3" hint="$4"
	echo "== ${label}  (go test -tags ${tag} -run '${runexpr}' ./internal/direct) =="
	local out
	if ! out="$(go test -tags "$tag" -run "$runexpr" -v ./internal/direct 2>&1)"; then
		echo "$out"
		echo "FAIL: ${label} oracle did not pass."
		fail=1
		return
	fi
	if grep -q -- '--- SKIP' <<<"$out"; then
		grep -- '--- SKIP' <<<"$out"
		echo "FAIL: ${label} oracle was SKIPPED — real verification did not run. ${hint}"
		fail=1
		return
	fi
	if ! grep -q -- '--- PASS' <<<"$out"; then
		echo "$out"
		echo "FAIL: ${label} oracle matched no tests."
		fail=1
		return
	fi
	grep -- '--- PASS' <<<"$out" | sed 's/^/  /'
	echo "OK: ${label} oracle verified."
	echo
}

run_oracle hessian_oracle '^TestHessianJavaContract' \
	"Hessian (JVM alipay)" "Needs java/javac and the alipay Hessian jar in ~/.m2."
run_oracle bolt_oracle '^TestBoltOracle' \
	"BOLT (sofa-bolt-go)" "Needs the sofa-bolt-go module (go test will fetch it)."

if [ "$fail" -ne 0 ]; then
	echo "RELEASE GATE FAILED — do not cut a release."
	exit 1
fi
echo "RELEASE GATE PASSED — codec verified against real oracles."
