package cli

import (
	"flag"
	"fmt"

	"github.com/sofarpc/cli/internal/launcher"
)

func runEngine(args []string, env Env) int {
	if len(args) == 0 {
		fmt.Fprintln(env.Stderr, "engine: subcommand required (start|stop|restart|status|logs)")
		return 2
	}
	switch args[0] {
	case "start":
		return runDaemonStart(args[1:], env)
	case "stop":
		return runDaemonStop(args[1:], env)
	case "status":
		return runDaemonStatus(args[1:], env)
	case "restart":
		if code := runDaemonStop([]string{"--wait-ms", "10000"}, env); code != 0 {
			return code
		}
		return runDaemonStart(args[1:], env)
	case "logs":
		return runEngineLogs(args[1:], env)
	default:
		fmt.Fprintf(env.Stderr, "engine: unknown subcommand %q\n", args[0])
		return 2
	}
}

func runEngineLogs(args []string, env Env) int {
	fs := flag.NewFlagSet("engine logs", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	bytes := fs.Int("bytes", 8192, "number of trailing log bytes to print")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	paths, err := launcher.DefaultPaths()
	if err != nil {
		fmt.Fprintln(env.Stderr, "engine logs:", err)
		return 1
	}
	tail, err := launcher.TailFile(paths.LogFile, *bytes)
	if err != nil {
		fmt.Fprintln(env.Stderr, "engine logs:", err)
		return 1
	}
	fmt.Fprint(env.Stdout, tail)
	return 0
}
