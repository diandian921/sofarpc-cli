package launcher

import (
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
	TokenFile  string
	JVMArgs    []string
	BuildVer   string
	ExtraArgs  []string
	WorkingDir string
}

// ResolveJarPath finds the daemon jar in this priority order:
//  1. Explicit path argument (may be empty)
//  2. SOFARPCD_JAR environment variable
//  3. Sibling of the current Go binary: <bin-dir>/sofarpc-engine.jar
//  4. ~/.sofarpc/lib/sofarpc-engine.jar
//  5. Legacy fallbacks: sofarpcd.jar beside the binary or under ~/.sofarpc/daemon/
func ResolveJarPath(explicit string) (string, error) {
	if explicit != "" {
		if _, err := os.Stat(explicit); err == nil {
			return explicit, nil
		} else {
			return "", NewDiagnosticError(ReasonEngineJarNotFound, "Engine jar was not found", err).
				WithDetail("source", "explicit").
				WithDetail("jarPath", explicit)
		}
	}
	if env := os.Getenv("SOFARPCD_JAR"); env != "" {
		if _, err := os.Stat(env); err == nil {
			return env, nil
		} else {
			return "", NewDiagnosticError(ReasonEngineJarNotFound, "Engine jar was not found", err).
				WithDetail("source", "SOFARPCD_JAR").
				WithDetail("jarPath", env)
		}
	}
	candidates := make([]string, 0, 2)
	if exe, err := os.Executable(); err == nil {
		for _, name := range []string{"sofarpc-engine.jar", "sofarpcd.jar"} {
			candidate := filepath.Join(filepath.Dir(exe), name)
			candidates = append(candidates, candidate)
			if _, err := os.Stat(candidate); err == nil {
				return candidate, nil
			}
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		for _, candidate := range []string{
			filepath.Join(home, ".sofarpc", "lib", "sofarpc-engine.jar"),
			filepath.Join(home, ".sofarpc", "daemon", "sofarpcd.jar"),
		} {
			candidates = append(candidates, candidate)
			if _, err := os.Stat(candidate); err == nil {
				return candidate, nil
			}
		}
	}
	return "", NewDiagnosticError(ReasonEngineJarNotFound, "Engine jar was not found", nil).
		WithDetail("candidates", candidates).
		WithDetail("hint", "set SOFARPCD_JAR or install sofarpc-engine.jar under ~/.sofarpc/lib")
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
	if cfg.TokenFile != "" {
		args = append(args, "--token-file", cfg.TokenFile)
	}
	args = append(args, "--idle-ttl-ms", strconv.FormatInt(cfg.IdleTTLMS, 10))
	if cfg.BuildVer != "" {
		args = append(args, "--build-version", cfg.BuildVer)
	}
	args = append(args, cfg.ExtraArgs...)

	logF, err := os.OpenFile(cfg.LogFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return 0, NewDiagnosticError(ReasonSpawnFailed, "Engine process could not be started", err).
			WithDetail("phase", "open_log").
			WithDetail("logFile", cfg.LogFile)
	}

	cmd := exec.Command(cfg.JavaBin, args...)
	cmd.Stdin = nil
	cmd.Stdout = logF
	cmd.Stderr = logF
	cmd.Dir = cfg.WorkingDir
	detachProcess(cmd)
	if err := cmd.Start(); err != nil {
		_ = logF.Close()
		reason := ReasonSpawnFailed
		if isExecNotFound(err) {
			reason = ReasonJavaNotFound
		}
		return 0, NewDiagnosticError(reason, "Engine process could not be started", err).
			WithDetail("javaBin", cfg.JavaBin).
			WithDetail("jarPath", cfg.JarPath).
			WithDetail("logFile", cfg.LogFile).
			WithLogTail(cfg.LogFile, 4096)
	}
	if err := cmd.Process.Release(); err != nil {
		return 0, NewDiagnosticError(ReasonSpawnFailed, "Engine process could not be detached", err).
			WithDetail("pid", cmd.Process.Pid).
			WithDetail("logFile", cfg.LogFile).
			WithLogTail(cfg.LogFile, 4096)
	}
	return cmd.Process.Pid, nil
}
