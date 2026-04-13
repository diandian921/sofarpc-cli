// Package cli wires argv to subcommand handlers. Every subcommand receives the same Env so
// tests can drive the CLI with in-memory buffers instead of real OS streams.
package cli

import (
	"fmt"
	"io"
)

// Env carries process-wide dependencies that subcommands need.
type Env struct {
	BuildVersion string
	Stdin        io.Reader
	Stdout       io.Writer
	Stderr       io.Writer
}

// Handler is the signature every subcommand implements.
type Handler func(args []string, env Env) int

// Run dispatches to the handler matching args[0]. Unknown or missing commands print usage.
func Run(args []string, env Env) int {
	if len(args) == 0 {
		printUsage(env.Stderr)
		return 2
	}
	switch args[0] {
	case "exec":
		return runExec(args[1:], env)
	case "invoke":
		return runInvoke(args[1:], env)
	case "ping":
		return runPing(args[1:], env)
	case "daemon":
		return runDaemon(args[1:], env)
	case "version", "--version", "-v":
		fmt.Fprintln(env.Stdout, env.BuildVersion)
		return 0
	case "help", "--help", "-h":
		printUsage(env.Stdout)
		return 0
	default:
		fmt.Fprintf(env.Stderr, "unknown command: %s\n", args[0])
		printUsage(env.Stderr)
		return 2
	}
}

func printUsage(w io.Writer) {
	fmt.Fprint(w, `sofarpc — agent-first SOFARPC CLI

Usage:
  sofarpc exec --stdin                 Read one envelope from stdin, write one to stdout.
  sofarpc invoke [flags]               Build and send an invoke request.
  sofarpc ping --address host:port     Probe a target address via the daemon.
  sofarpc daemon start|stop|status     Manage the local daemon.
  sofarpc version                      Print build version.
`)
}
