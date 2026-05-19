package buildversion

import (
	"runtime/debug"
	"testing"
)

func TestResolvePrefersStampedVersion(t *testing.T) {
	if got := Resolve("v1.2.3"); got != "v1.2.3" {
		t.Fatalf("Resolve stamped = %q", got)
	}
}

func TestResolveFallsBackToModuleVersion(t *testing.T) {
	prev := readBuildInfo
	readBuildInfo = func() (*debug.BuildInfo, bool) {
		return &debug.BuildInfo{Main: debug.Module{Version: "v1.2.3"}}, true
	}
	t.Cleanup(func() { readBuildInfo = prev })

	if got := Resolve("dev"); got != "v1.2.3" {
		t.Fatalf("Resolve module version = %q", got)
	}
}

func TestResolveStripsSubmoduleTagPrefix(t *testing.T) {
	prev := readBuildInfo
	readBuildInfo = func() (*debug.BuildInfo, bool) {
		return &debug.BuildInfo{Main: debug.Module{Version: "cli/v1.2.3"}}, true
	}
	t.Cleanup(func() { readBuildInfo = prev })

	if got := Resolve("dev"); got != "v1.2.3" {
		t.Fatalf("Resolve prefixed module version = %q", got)
	}
}

func TestResolveDevWhenNoVersion(t *testing.T) {
	prev := readBuildInfo
	readBuildInfo = func() (*debug.BuildInfo, bool) {
		return &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}}, true
	}
	t.Cleanup(func() { readBuildInfo = prev })

	if got := Resolve("dev"); got != "dev" {
		t.Fatalf("Resolve devel = %q", got)
	}
}

func TestResolveStripsCliPrefixFromStampedTag(t *testing.T) {
	if got := Resolve("cli/v0.1.0-beta.2"); got != "v0.1.0-beta.2" {
		t.Fatalf("Resolve(stamped cli/ tag) = %q, want v0.1.0-beta.2", got)
	}
	if got := Resolve("v1.2.3"); got != "v1.2.3" {
		t.Fatalf("Resolve(plain tag) = %q, want v1.2.3", got)
	}
}
