package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"time"

	"github.com/sofarpc/cli-go/internal/ipc"
	"github.com/sofarpc/cli-go/internal/launcher"
	"github.com/sofarpc/cli-go/internal/protocol"
)

// runDaemon multiplexes the three local lifecycle commands. None of them take extra args in V1.
func runDaemon(args []string, env Env) int {
	if len(args) == 0 {
		fmt.Fprintln(env.Stderr, "daemon: subcommand required (start|stop|status)")
		return 2
	}
	switch args[0] {
	case "start":
		return runDaemonStart(args[1:], env)
	case "stop":
		return runDaemonStop(args[1:], env)
	case "status":
		return runDaemonStatus(args[1:], env)
	default:
		fmt.Fprintf(env.Stderr, "daemon: unknown subcommand %q\n", args[0])
		return 2
	}
}

func runDaemonStart(args []string, env Env) int {
	fs := flag.NewFlagSet("daemon start", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	jar := fs.String("jar", "", "path to sofarpcd.jar (overrides autodiscovery)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	cfg, err := launcher.DefaultConfig(env.BuildVersion)
	if err != nil {
		fmt.Fprintln(env.Stderr, "daemon start:", err)
		return 1
	}
	if *jar != "" {
		cfg.JarPath = *jar
	}
	conn, err := launcher.Connect(cfg)
	if err != nil {
		fmt.Fprintln(env.Stderr, "daemon start:", err)
		return 1
	}
	out, _ := json.Marshal(map[string]interface{}{
		"ok":     true,
		"state":  conn.State,
		"health": conn.Health,
	})
	fmt.Fprintln(env.Stdout, string(out))
	return 0
}

func runDaemonStatus(args []string, env Env) int {
	fs := flag.NewFlagSet("daemon status", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	paths, err := launcher.DefaultPaths()
	if err != nil {
		return internalFailure(env, err)
	}
	state, err := launcher.ReadState(paths.StateFile)
	if err != nil {
		return internalFailure(env, err)
	}
	result := map[string]interface{}{"ok": false, "running": false}
	if state == nil {
		emitJSON(env.Stdout, result)
		return 0
	}
	result["state"] = state
	if !launcher.IsPIDAlive(state.PID) {
		result["running"] = false
		result["reason"] = "pid not alive"
		emitJSON(env.Stdout, result)
		return 0
	}
	client := &ipc.Client{
		Addr:           fmt.Sprintf("127.0.0.1:%d", state.Port),
		DialTimeout:    launcher.DefaultDialTimeout,
		RequestTimeout: 2 * time.Second,
	}
	req, _ := protocol.NewRequest(protocol.OpHealth, struct{}{})
	resp, err := client.Call(req)
	if err != nil || !resp.OK {
		result["running"] = false
		if err != nil {
			result["reason"] = err.Error()
		} else {
			result["reason"] = resp.Code
		}
		emitJSON(env.Stdout, result)
		return 0
	}
	var health protocol.HealthData
	_ = json.Unmarshal(resp.Data, &health)
	result["ok"] = true
	result["running"] = true
	result["health"] = health
	emitJSON(env.Stdout, result)
	return 0
}

func runDaemonStop(args []string, env Env) int {
	fs := flag.NewFlagSet("daemon stop", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	graceMS := fs.Int64("grace-ms", 0, "grace period before daemon forcefully exits")
	waitMS := fs.Int64("wait-ms", 5000, "how long to wait for the pid to disappear")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	paths, err := launcher.DefaultPaths()
	if err != nil {
		return internalFailure(env, err)
	}
	state, err := launcher.ReadState(paths.StateFile)
	if err != nil {
		return internalFailure(env, err)
	}
	if state == nil {
		emitJSON(env.Stdout, map[string]interface{}{"ok": true, "stopped": false, "reason": "no state file"})
		return 0
	}
	client := &ipc.Client{
		Addr:           fmt.Sprintf("127.0.0.1:%d", state.Port),
		DialTimeout:    launcher.DefaultDialTimeout,
		RequestTimeout: 2 * time.Second,
	}
	req, _ := protocol.NewRequest(protocol.OpShutdown, protocol.ShutdownPayload{GraceMS: *graceMS})
	_, sendErr := client.Call(req)
	waitUntil := time.Now().Add(time.Duration(*waitMS) * time.Millisecond)
	for time.Now().Before(waitUntil) {
		if !launcher.IsPIDAlive(state.PID) {
			_ = launcher.DeleteState(paths.StateFile)
			emitJSON(env.Stdout, map[string]interface{}{"ok": true, "stopped": true})
			return 0
		}
		time.Sleep(50 * time.Millisecond)
	}
	reason := "timeout waiting for daemon to exit"
	if sendErr != nil {
		reason = sendErr.Error()
	}
	emitJSON(env.Stdout, map[string]interface{}{"ok": false, "stopped": false, "reason": reason})
	return 1
}

func emitJSON(w fmtWriter, v interface{}) {
	body, _ := json.Marshal(v)
	fmt.Fprintln(w, string(body))
}

type fmtWriter interface {
	Write(p []byte) (int, error)
}

func internalFailure(env Env, err error) int {
	fmt.Fprintln(env.Stderr, "daemon:", err)
	return 1
}
