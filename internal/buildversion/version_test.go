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
