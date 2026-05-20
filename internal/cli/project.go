package cli

import (
	"encoding/json"
	"flag"
	"fmt"

	"github.com/diandian921/sofarpc-cli/internal/appconfig"
)

func runProject(args []string, env Env) int {
	if len(args) == 0 {
		fmt.Fprintln(env.Stderr, "project: subcommand required (add|list|remove)")
		return 2
	}
	switch args[0] {
	case "add":
		return runProjectAdd(args[1:], env)
	case "list", "ls":
		return runProjectList(args[1:], env)
	case "remove", "rm":
		return runProjectRemove(args[1:], env)
	default:
		fmt.Fprintf(env.Stderr, "project: unknown subcommand %q\n", args[0])
		return 2
	}
}

func runProjectAdd(args []string, env Env) int {
	fs := flag.NewFlagSet("project add", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	var prefixes repeatedString
	fs.Var(&prefixes, "prefix", "service package prefix; may be repeated")
	overwrite := fs.Bool("overwrite", false, "replace an existing project")
	rest, err := parseMixed(fs, args)
	if err != nil {
		return 2
	}
	if len(rest) != 2 {
		fmt.Fprintln(env.Stderr, "usage: sofarpc project add <name> <workspaceRoot> [--prefix <java.package>] [--overwrite]")
		return 2
	}
	path, lock, err := configPaths()
	if err != nil {
		fmt.Fprintln(env.Stderr, "project add:", err)
		return 1
	}
	var project appconfig.Project
	_, err = appconfig.Update(path, lock, func(cfg *appconfig.Config) error {
		var addErr error
		project, addErr = cfg.AddProject(rest[0], rest[1], prefixes, *overwrite)
		return addErr
	})
	if err != nil {
		fmt.Fprintln(env.Stderr, "project add:", err)
		return 1
	}
	emitJSON(env.Stdout, map[string]interface{}{"ok": true, "name": rest[0], "project": project})
	return 0
}

func runProjectList(args []string, env Env) int {
	fs := flag.NewFlagSet("project list", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	path, err := appconfig.DefaultPath()
	if err != nil {
		fmt.Fprintln(env.Stderr, "project list:", err)
		return 1
	}
	cfg, err := appconfig.Load(path)
	if err != nil {
		fmt.Fprintln(env.Stderr, "project list:", err)
		return 1
	}
	out := make([]map[string]interface{}, 0, len(cfg.Projects))
	for _, name := range cfg.ProjectNames() {
		out = append(out, map[string]interface{}{"name": name, "project": cfg.Projects[name]})
	}
	body, _ := json.Marshal(map[string]interface{}{"ok": true, "projects": out})
	fmt.Fprintln(env.Stdout, string(body))
	return 0
}

func runProjectRemove(args []string, env Env) int {
	fs := flag.NewFlagSet("project remove", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	confirm := fs.Bool("confirm", false, "required to remove the project")
	cascade := fs.Bool("cascade", false, "also remove servers bound to the project")
	rest, err := parseMixed(fs, args)
	if err != nil {
		return 2
	}
	if len(rest) != 1 {
		fmt.Fprintln(env.Stderr, "usage: sofarpc project remove <name> --confirm [--cascade]")
		return 2
	}
	path, lock, err := configPaths()
	if err != nil {
		fmt.Fprintln(env.Stderr, "project remove:", err)
		return 1
	}
	_, err = appconfig.Update(path, lock, func(cfg *appconfig.Config) error {
		return cfg.RemoveProject(rest[0], *confirm, *cascade)
	})
	if err != nil {
		fmt.Fprintln(env.Stderr, "project remove:", err)
		return 1
	}
	emitJSON(env.Stdout, map[string]interface{}{"ok": true, "removed": rest[0]})
	return 0
}

type repeatedString []string

func (v *repeatedString) String() string {
	body, _ := json.Marshal([]string(*v))
	return string(body)
}

func (v *repeatedString) Set(s string) error {
	*v = append(*v, s)
	return nil
}

func configPaths() (string, string, error) {
	path, err := appconfig.DefaultPath()
	if err != nil {
		return "", "", err
	}
	lock, err := appconfig.DefaultLockPath()
	if err != nil {
		return "", "", err
	}
	return path, lock, nil
}
