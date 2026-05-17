package launcher

import (
	"context"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const javaVersionCheckTimeout = 3 * time.Second

var javaVersionRE = regexp.MustCompile(`version "([^"]+)"`)

func ValidateJava(javaBin string) error {
	ctx, cancel := context.WithTimeout(context.Background(), javaVersionCheckTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, javaBin, "-version")
	out, err := cmd.CombinedOutput()
	if err != nil {
		reason := ReasonSpawnFailed
		if isExecNotFound(err) {
			reason = ReasonJavaNotFound
		}
		return NewDiagnosticError(reason, "Java runtime is not available", err).
			WithDetail("javaBin", javaBin).
			WithDetail("javaOutput", strings.TrimSpace(string(out)))
	}
	major, ok := parseJavaMajorVersion(string(out))
	if ok && major < 8 {
		return NewDiagnosticError(ReasonJavaVersionUnsupported, "Java runtime version is unsupported", nil).
			WithDetail("javaBin", javaBin).
			WithDetail("javaVersion", strings.TrimSpace(string(out))).
			WithDetail("required", "8+")
	}
	return nil
}

func parseJavaMajorVersion(output string) (int, bool) {
	match := javaVersionRE.FindStringSubmatch(output)
	if len(match) != 2 {
		return 0, false
	}
	version := match[1]
	parts := strings.Split(version, ".")
	if len(parts) == 0 {
		return 0, false
	}
	if parts[0] == "1" && len(parts) > 1 {
		major, err := strconv.Atoi(parts[1])
		return major, err == nil
	}
	major, err := strconv.Atoi(parts[0])
	return major, err == nil
}
