package appconfig

import (
	"os"
	"path/filepath"
)

// EnvHome is the environment variable that overrides the install root.
const EnvHome = "SOFARPC_HOME"

// DefaultDirName is the install root directory created under the user home
// when no override applies.
const DefaultDirName = ".sofarpc"

// Home resolves the runtime install root. It is the single indirection point:
// every path the CLI and MCP server depend on derives from it, so callers
// never hardcode ~/.sofarpc.
//
// Resolution order:
//  1. explicit SOFARPC_HOME if set;
//  2. else, if the running executable sits in a bin/ whose parent contains
//     config.json, that parent (the canonical-install case);
//  3. else, <user home>/.sofarpc.
func Home() (string, error) {
	if env, ok := envHomeOverride(); ok {
		return env, nil
	}
	if root, ok := selfLocatedRoot(); ok {
		return root, nil
	}
	return defaultRoot()
}

// envHomeOverride returns an absolute SOFARPC_HOME when the variable is set. A
// relative value is resolved against the current directory so the runtime root
// does not drift as the process changes CWD; if Abs fails the raw value is used.
func envHomeOverride() (string, bool) {
	env := os.Getenv(EnvHome)
	if env == "" {
		return "", false
	}
	if abs, err := filepath.Abs(env); err == nil {
		return abs, true
	}
	return env, true
}

// InstallRoot resolves the destination for self-install. It deliberately omits
// the self-locate step: during installation the running binary is the source,
// not yet placed under the canonical root.
func InstallRoot() (string, error) {
	if env, ok := envHomeOverride(); ok {
		return env, nil
	}
	return defaultRoot()
}

// DefaultInstallRoot is the install root that applies when SOFARPC_HOME is
// unset: <user home>/.sofarpc. Used to decide whether a resolved root is the
// default (and thus whether SOFARPC_HOME must be propagated to hosts).
func DefaultInstallRoot() (string, error) {
	return defaultRoot()
}

func defaultRoot() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, DefaultDirName), nil
}

func selfLocatedRoot() (string, bool) {
	exe, err := os.Executable()
	if err != nil {
		return "", false
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return rootFromExe(exe)
}

// rootFromExe reports the canonical root for a binary at exe: it must live in
// a bin/ directory whose parent contains a config.json file.
func rootFromExe(exe string) (string, bool) {
	binDir := filepath.Dir(exe)
	if filepath.Base(binDir) != "bin" {
		return "", false
	}
	root := filepath.Dir(binDir)
	if info, err := os.Stat(filepath.Join(root, "config.json")); err == nil && !info.IsDir() {
		return root, true
	}
	return "", false
}
