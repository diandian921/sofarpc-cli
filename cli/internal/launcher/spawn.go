package launcher

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
)

// SpawnConfig configures how the launcher invokes the Java daemon.
type SpawnConfig struct {
	JavaBin    string
	JarPath    string
	Port       int
	IdleTTLMS  int64
	StateFile  string
	LogFile    string
	JVMArgs    []string
	BuildVer   string
	ExtraArgs  []string
	WorkingDir string
}

// ResolveJarPath finds the daemon jar in this priority order:
//  1. Explicit path argument (may be empty)
//  2. SOFARPCD_JAR environment variable
//  3. Sibling of the current Go binary: <bin-dir>/sofarpcd.jar
//  4. ~/.sofarpc/daemon/sofarpcd.jar
func ResolveJarPath(explicit string) (string, error) {
	if explicit != "" {
		if _, err := os.Stat(explicit); err == nil {
			return explicit, nil
		}
		return "", fmt.Errorf("jar not found at %s", explicit)
	}
	if env := os.Getenv("SOFARPCD_JAR"); env != "" {
		if _, err := os.Stat(env); err == nil {
			return env, nil
		}
		return "", fmt.Errorf("SOFARPCD_JAR points to missing file: %s", env)
	}
	if exe, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(exe), "sofarpcd.jar")
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		candidate := filepath.Join(home, ".sofarpc", "daemon", "sofarpcd.jar")
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	return "", errors.New("daemon jar not found; set SOFARPCD_JAR or place sofarpcd.jar next to the binary")
}

// ResolveJavaBin returns JAVA_HOME/bin/java if set, otherwise just "java".
func ResolveJavaBin() string {
	if home := os.Getenv("JAVA_HOME"); home != "" {
		return filepath.Join(home, "bin", "java")
	}
	return "java"
}

// Spawn launches the daemon as a detached child process. Returns the child PID.
func Spawn(cfg SpawnConfig) (int, error) {
	args := append([]string{}, cfg.JVMArgs...)
	args = append(args, "-jar", cfg.JarPath)
	args = append(args, "--port", strconv.Itoa(cfg.Port))
	args = append(args, "--state-file", cfg.StateFile)
	args = append(args, "--idle-ttl-ms", strconv.FormatInt(cfg.IdleTTLMS, 10))
	if cfg.BuildVer != "" {
		args = append(args, "--build-version", cfg.BuildVer)
	}
	args = append(args, cfg.ExtraArgs...)

	logF, err := os.OpenFile(cfg.LogFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return 0, fmt.Errorf("open daemon log %s: %w", cfg.LogFile, err)
	}

	cmd := exec.Command(cfg.JavaBin, args...)
	cmd.Stdin = nil
	cmd.Stdout = logF
	cmd.Stderr = logF
	cmd.Dir = cfg.WorkingDir
	detachProcess(cmd)
	if err := cmd.Start(); err != nil {
		_ = logF.Close()
		return 0, fmt.Errorf("spawn daemon: %w", err)
	}
	if err := cmd.Process.Release(); err != nil {
		return 0, fmt.Errorf("release daemon process: %w", err)
	}
	return cmd.Process.Pid, nil
}
