package buildversion

import (
	"runtime/debug"
	"strings"
)

var readBuildInfo = debug.ReadBuildInfo

// Resolve returns the version to expose from command binaries.
//
// Release archives stamp this with -ldflags. `go install module@version` cannot
// pass our ldflags, so fall back to the Go module version recorded in build
// metadata. Local source builds still report "dev".
func Resolve(stamped string) string {
	stamped = strings.TrimSpace(stamped)
	if stamped != "" && stamped != "dev" {
		// Single normalization point: a caller (CI, raw `git describe`) may
		// stamp the subdirectory-module tag verbatim; strip the cli/ prefix
		// here so the scripts' stripping is belt-and-suspenders, not the only
		// guard.
		return strings.TrimPrefix(stamped, "cli/")
	}
	info, ok := readBuildInfo()
	if !ok {
		return "dev"
	}
	version := strings.TrimSpace(info.Main.Version)
	if version == "" || version == "(devel)" {
		return "dev"
	}
	return strings.TrimPrefix(version, "cli/")
}
