package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/diandian921/sofarpc-cli/cli/internal/appconfig"
)

// runServer manages configured RPC servers in ~/.sofarpc/config.json.
// Go resolves server names before invoking the direct runtime.
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
	project := fs.String("project", "", "bound project name")
	protocol := fs.String("protocol", appconfig.DefaultServerProtocol, "rpc protocol")
	timeoutMS := fs.Int("timeout-ms", appconfig.DefaultServerTimeoutMS, "default total timeout in milliseconds")
	appName := fs.String("app-name", appconfig.DefaultServerAppName, "SofaRPC consumer app name")
	overwrite := fs.Bool("overwrite", false, "replace an existing server")
	var attachments repeatedString
	fs.Var(&attachments, "attachment", "attachment as key=value; may be repeated")
	_ = fs.String("desc", "", "legacy alias description; ignored")
	rest, err := parseMixed(fs, args)
	if err != nil {
		return 2
	}
	if len(rest) != 2 {
		fmt.Fprintln(env.Stderr, "usage: sofarpc server add <name> <host:port> --project <project> [--timeout-ms <ms>] [--attachment k=v]")
		return 2
	}
	name, addr := rest[0], rest[1]

	if *project == "" {
		fmt.Fprintln(env.Stderr, "server add: --project is required")
		return 2
	}
	attachmentMap, err := parseAttachments(attachments)
	if err != nil {
		fmt.Fprintln(env.Stderr, "server add:", err)
		return 2
	}
	path, lock, err := configPaths()
	if err != nil {
		fmt.Fprintln(env.Stderr, "server add:", err)
		return 1
	}
	var server appconfig.Server
	_, err = appconfig.Update(path, lock, func(cfg *appconfig.Config) error {
		var addErr error
		server, addErr = cfg.AddServer(name, appconfig.Server{
			Address:     addr,
			Project:     *project,
			Protocol:    *protocol,
			TimeoutMS:   *timeoutMS,
			AppName:     *appName,
			Attachments: attachmentMap,
		}, *overwrite)
		return addErr
	})
	if err != nil {
		fmt.Fprintln(env.Stderr, "server add:", err)
		return 1
	}
	out := map[string]interface{}{
		"ok":     true,
		"name":   name,
		"server": server,
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
	path, err := appconfig.DefaultPath()
	if err != nil {
		fmt.Fprintln(env.Stderr, "server list:", err)
		return 1
	}
	cfg, err := appconfig.Load(path)
	if err != nil {
		fmt.Fprintln(env.Stderr, "server list:", err)
		return 1
	}
	if *asJSON {
		body, _ := json.Marshal(map[string]interface{}{"ok": true, "servers": serversList(cfg)})
		fmt.Fprintln(env.Stdout, string(body))
		return 0
	}
	printServerTable(env.Stdout, cfg)
	return 0
}

func runServerRemove(args []string, env Env) int {
	fs := flag.NewFlagSet("server remove", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	confirm := fs.Bool("confirm", false, "required to remove the server")
	rest, err := parseMixed(fs, args)
	if err != nil {
		return 2
	}
	if len(rest) != 1 {
		fmt.Fprintln(env.Stderr, "usage: sofarpc server remove <name> --confirm")
		return 2
	}
	name := rest[0]

	path, lock, err := configPaths()
	if err != nil {
		fmt.Fprintln(env.Stderr, "server remove:", err)
		return 1
	}
	_, err = appconfig.Update(path, lock, func(cfg *appconfig.Config) error {
		return cfg.RemoveServer(name, *confirm)
	})
	if err != nil {
		fmt.Fprintln(env.Stderr, "server remove:", err)
		return 1
	}
	emitJSON(env.Stdout, map[string]interface{}{"ok": true, "removed": name})
	return 0
}

func printServerTable(w io.Writer, cfg appconfig.Config) {
	names := cfg.ServerNames()
	if len(names) == 0 {
		fmt.Fprintln(w, "(no servers configured)")
		return
	}
	sort.Strings(names)
	maxName, maxAddr, maxProject := len("SERVER"), len("ADDRESS"), len("PROJECT")
	for _, n := range names {
		if len(n) > maxName {
			maxName = len(n)
		}
		s := cfg.Servers[n]
		if a := s.Address; len(a) > maxAddr {
			maxAddr = len(a)
		}
		if len(s.Project) > maxProject {
			maxProject = len(s.Project)
		}
	}
	format := fmt.Sprintf("%%-%ds  %%-%ds  %%-%ds  %%s\n", maxName, maxAddr, maxProject)
	fmt.Fprintf(w, format, "SERVER", "ADDRESS", "PROJECT", "TIMEOUT")
	for _, n := range names {
		s := cfg.Servers[n]
		fmt.Fprintf(w, format, n, s.Address, s.Project, fmt.Sprintf("%dms", s.TimeoutMS))
	}
}

func serversList(cfg appconfig.Config) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(cfg.Servers))
	for _, name := range cfg.ServerNames() {
		out = append(out, map[string]interface{}{"name": name, "server": cfg.Servers[name]})
	}
	return out
}

func parseAttachments(values []string) (map[string]string, error) {
	out := map[string]string{}
	for _, value := range values {
		key, val, ok := strings.Cut(value, "=")
		if !ok || strings.TrimSpace(key) == "" {
			return nil, fmt.Errorf("invalid attachment %q: expected key=value", value)
		}
		out[strings.TrimSpace(key)] = val
	}
	return out, nil
}
