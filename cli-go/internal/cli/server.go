package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"sort"

	"github.com/sofarpc/cli-go/internal/alias"
)

// runServer manages the local alias registry under ~/.sofarpc/servers.json.
// The registry is pure client state; the daemon never reads it.
func runServer(args []string, env Env) int {
	if len(args) == 0 {
		fmt.Fprintln(env.Stderr, "server: subcommand required (add|list|remove)")
		return 2
	}
	switch args[0] {
	case "add":
		return runServerAdd(args[1:], env)
	case "list", "ls":
		return runServerList(args[1:], env)
	case "remove", "rm":
		return runServerRemove(args[1:], env)
	default:
		fmt.Fprintf(env.Stderr, "server: unknown subcommand %q\n", args[0])
		return 2
	}
}

func runServerAdd(args []string, env Env) int {
	fs := flag.NewFlagSet("server add", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	desc := fs.String("desc", "", "human description (optional)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	rest := fs.Args()
	if len(rest) != 2 {
		fmt.Fprintln(env.Stderr, "usage: sofarpc server add <alias> <host:port> [--desc <text>]")
		return 2
	}
	name, addr := rest[0], rest[1]

	path, err := alias.DefaultPath()
	if err != nil {
		fmt.Fprintln(env.Stderr, "server add:", err)
		return 1
	}
	reg, err := alias.Load(path)
	if err != nil {
		fmt.Fprintln(env.Stderr, "server add:", err)
		return 1
	}
	if err := reg.Add(name, addr, *desc); err != nil {
		fmt.Fprintln(env.Stderr, "server add:", err)
		return 2
	}
	if err := alias.Save(path, reg); err != nil {
		fmt.Fprintln(env.Stderr, "server add:", err)
		return 1
	}
	out := map[string]interface{}{
		"ok":      true,
		"alias":   name,
		"address": addr,
	}
	if *desc != "" {
		out["description"] = *desc
	}
	emitJSON(env.Stdout, out)
	return 0
}

func runServerList(args []string, env Env) int {
	fs := flag.NewFlagSet("server list", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	asJSON := fs.Bool("json", false, "emit JSON instead of a table")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	path, err := alias.DefaultPath()
	if err != nil {
		fmt.Fprintln(env.Stderr, "server list:", err)
		return 1
	}
	reg, err := alias.Load(path)
	if err != nil {
		fmt.Fprintln(env.Stderr, "server list:", err)
		return 1
	}
	if *asJSON {
		body, _ := json.Marshal(reg)
		fmt.Fprintln(env.Stdout, string(body))
		return 0
	}
	printAliasTable(env.Stdout, reg)
	return 0
}

func runServerRemove(args []string, env Env) int {
	fs := flag.NewFlagSet("server remove", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	rest := fs.Args()
	if len(rest) != 1 {
		fmt.Fprintln(env.Stderr, "usage: sofarpc server remove <alias>")
		return 2
	}
	name := rest[0]

	path, err := alias.DefaultPath()
	if err != nil {
		fmt.Fprintln(env.Stderr, "server remove:", err)
		return 1
	}
	reg, err := alias.Load(path)
	if err != nil {
		fmt.Fprintln(env.Stderr, "server remove:", err)
		return 1
	}
	if err := reg.Remove(name); err != nil {
		fmt.Fprintln(env.Stderr, "server remove:", err)
		return 1
	}
	if err := alias.Save(path, reg); err != nil {
		fmt.Fprintln(env.Stderr, "server remove:", err)
		return 1
	}
	emitJSON(env.Stdout, map[string]interface{}{"ok": true, "removed": name})
	return 0
}

func printAliasTable(w io.Writer, reg *alias.Registry) {
	names := reg.Names()
	if len(names) == 0 {
		fmt.Fprintln(w, "(no aliases registered)")
		return
	}
	sort.Strings(names)
	maxName, maxAddr := len("ALIAS"), len("ADDRESS")
	for _, n := range names {
		if len(n) > maxName {
			maxName = len(n)
		}
		if a := reg.Servers[n].Address; len(a) > maxAddr {
			maxAddr = len(a)
		}
	}
	format := fmt.Sprintf("%%-%ds  %%-%ds  %%s\n", maxName, maxAddr)
	fmt.Fprintf(w, format, "ALIAS", "ADDRESS", "DESCRIPTION")
	for _, n := range names {
		s := reg.Servers[n]
		fmt.Fprintf(w, format, n, s.Address, s.Description)
	}
}
