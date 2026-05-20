package buildversion

import (
	"runtime/debug"
	"strings"
)

var readBuildInfo = debug.ReadBuildInfo

// Resolve returns the version to expose from command binaries.
//
// Release archives stamp this with -ldflags. `go install module@version`
// cannot pass our ldflags, so fall back to the Go module version recorded in
// build metadata. Local source builds still report "dev".
func Resolve(stamped string) string {
	stamped = strings.TrimSpace(stamped)
	if stamped != "" && stamped != "dev" {
		return stamped
	}
	info, ok := readBuildInfo()
	if !ok {
		return "dev"
	}
	version := strings.TrimSpace(info.Main.Version)
	if version == "" || version == "(devel)" {
		return "dev"
	}
	return version
}
