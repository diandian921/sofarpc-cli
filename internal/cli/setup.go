package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/diandian921/sofarpc-cli/internal/appconfig"
)

// hostExec runs a host CLI command. It is a package var so tests can stub the
// real claude/codex invocations.
var hostExec = func(name string, args ...string) (stdout string, stderr string, code int, err error) {
	cmd := exec.Command(name, args...)
	var out, errBuf strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	err = cmd.Run()
	code = 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			code = exitErr.ExitCode()
			err = nil
		}
	}
	return out.String(), errBuf.String(), code, err
}

type desiredEntry struct {
	name    string
	command string
	// args is the argv tail passed to command; for the single-binary layout
	// the MCP host launches `sofarpc mcp`, so args is ["mcp"].
	args    []string
	homeEnv string // non-empty only when SOFARPC_HOME is non-default
}

func runSetup(args []string, env Env) int {
	fs := flag.NewFlagSet("setup", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	name := fs.String("name", "sofarpc", "MCP server name to register")
	dryRun := fs.Bool("dry-run", false, "print the host commands that would run; mutate nothing")
	force := fs.Bool("force", false, "replace an existing entry that exists but does not match desired")
	claudeScope := fs.String("claude-scope", "user", "Claude registration scope: user|local|project")
	rest, err := parseMixed(fs, args)
	if err != nil {
		return 2
	}
	if len(rest) != 1 {
		fmt.Fprintln(env.Stderr, "usage: sofarpc setup claude|codex|all [flags]")
		return 2
	}

	entry, err := buildDesiredEntry(*name)
	if err != nil {
		fmt.Fprintf(env.Stderr, "setup: %v\n", err)
		return 1
	}

	var targets []string
	switch rest[0] {
	case "claude":
		targets = []string{"claude"}
	case "codex":
		targets = []string{"codex"}
	case "all":
		targets = []string{"claude", "codex"}
	default:
		fmt.Fprintf(env.Stderr, "setup: unknown target %q (want claude|codex|all)\n", rest[0])
		return 2
	}
	if slicesContains(targets, "claude") && !validClaudeScope(*claudeScope) {
		fmt.Fprintf(env.Stderr, "setup: invalid --claude-scope %q (want user|local|project)\n", *claudeScope)
		return 2
	}

	// Verify the binary layer before mutating any host config: never register a
	// missing or broken sofarpc-mcp. Skipped for --dry-run (no mutation).
	if !*dryRun {
		if err := mcpPreflight(entry.command); err != nil {
			fmt.Fprintf(env.Stderr, "setup: binary check failed: %v\n", err)
			return 1
		}
	}

	exitCode := 0
	for _, target := range targets {
		var code int
		switch target {
		case "claude":
			code = setupClaude(env, entry, *claudeScope, *force, *dryRun)
		case "codex":
			code = setupCodex(env, entry, *force, *dryRun)
		}
		if code != 0 {
			exitCode = 1
		}
	}
	return exitCode
}

// mcpPreflight verifies the binary layer before registering: the command must
// exist and `command mcp --selftest` must pass. Package var so tests can stub it.
var mcpPreflight = func(command string) error {
	if !fileExists(command) {
		return fmt.Errorf("%s does not exist; run `sofarpc self-install` first", command)
	}
	out, errOut, code, err := hostExec(command, "mcp", "--selftest")
	if err != nil {
		return fmt.Errorf("cannot run %s mcp --selftest: %v", command, err)
	}
	if code != 0 {
		return fmt.Errorf("%s mcp --selftest failed: %s", command, strings.TrimSpace(errOut+out))
	}
	return nil
}

func buildDesiredEntry(name string) (desiredEntry, error) {
	// Home() (not InstallRoot()) so a binary run from a custom-home install
	// self-locates correctly even when SOFARPC_HOME is unset.
	root, err := appconfig.Home()
	if err != nil {
		return desiredEntry{}, err
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return desiredEntry{}, err
	}
	command := filepath.Join(abs, "bin", "sofarpc"+exeExt())
	entry := desiredEntry{name: name, command: command, args: []string{"mcp"}}
	def, err := defaultRootPath()
	if err == nil && abs != def {
		entry.homeEnv = abs
	}
	return entry, nil
}

func defaultRootPath() (string, error) {
	root, err := appconfig.DefaultInstallRoot()
	if err != nil {
		return "", err
	}
	return filepath.Abs(root)
}

func validClaudeScope(scope string) bool {
	switch scope {
	case "user", "local", "project":
		return true
	default:
		return false
	}
}

func slicesContains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

// setupCodex uses codex's structured read-back for true idempotence.
func setupCodex(env Env, entry desiredEntry, force, dryRun bool) int {
	addArgs := []string{"mcp", "add"}
	if entry.homeEnv != "" {
		addArgs = append(addArgs, "--env", "SOFARPC_HOME="+entry.homeEnv)
	}
	addArgs = append(addArgs, entry.name, "--", entry.command)
	addArgs = append(addArgs, entry.args...)

	if dryRun {
		printPlanned(env, "codex", addArgs)
		return 0
	}

	out, _, code, err := hostExec("codex", "mcp", "get", entry.name, "--json")
	if err != nil {
		return missingHostCLI(env, "codex", entry, err)
	}
	switch {
	case code != 0:
		return runHostMutation(env, "codex", addArgs, entry)
	case codexEntryMatches(out, entry):
		fmt.Fprintf(env.Stdout, "codex: %q already points at %s; no change.\n", entry.name, entry.command)
		return 0
	case !force:
		fmt.Fprintf(env.Stderr, "codex: %q exists but differs from desired; re-run with --force to replace.\n", entry.name)
		return 1
	default:
		if code := runHostRemoval(env, "codex", entry); code != 0 {
			return code
		}
		return runHostMutation(env, "codex", addArgs, entry)
	}
}

// setupClaude is existence-safe only: claude mcp get has no JSON form, so per
// the never-parse-foreign-config invariant we do not infer equality from human
// text. Present-but-unverified requires --force.
func setupClaude(env Env, entry desiredEntry, scope string, force, dryRun bool) int {
	addArgs := []string{"mcp", "add", "--scope", scope}
	if entry.homeEnv != "" {
		addArgs = append(addArgs, "-e", "SOFARPC_HOME="+entry.homeEnv)
	}
	addArgs = append(addArgs, entry.name, "--", entry.command)
	addArgs = append(addArgs, entry.args...)

	if dryRun {
		printPlanned(env, "claude", addArgs)
		return 0
	}

	_, _, code, err := hostExec("claude", "mcp", "get", entry.name)
	if err != nil {
		return missingHostCLI(env, "claude", entry, err)
	}
	if code != 0 {
		return runHostMutation(env, "claude", addArgs, entry)
	}
	if !force {
		fmt.Fprintf(env.Stderr, "claude: %q already exists. claude mcp get has no JSON form, so equality cannot be confirmed without parsing human text. Re-run with --force to replace.\n", entry.name)
		return 1
	}
	if code := runHostRemoval(env, "claude", entry); code != 0 {
		return code
	}
	return runHostMutation(env, "claude", addArgs, entry)
}

func runHostRemoval(env Env, host string, entry desiredEntry) int {
	_, stderr, code, err := hostExec(host, "mcp", "remove", entry.name)
	if err != nil {
		return missingHostCLI(env, host, entry, err)
	}
	if code != 0 {
		fmt.Fprintf(env.Stderr, "%s: remove existing %q failed: %s\n", host, entry.name, strings.TrimSpace(stderr))
		return 1
	}
	return 0
}

func runHostMutation(env Env, host string, addArgs []string, entry desiredEntry) int {
	_, stderr, code, err := hostExec(host, addArgs...)
	if err != nil {
		return missingHostCLI(env, host, entry, err)
	}
	if code != 0 {
		fmt.Fprintf(env.Stderr, "%s: registration failed: %s\n", host, strings.TrimSpace(stderr))
		return 1
	}
	fmt.Fprintf(env.Stdout, "%s: registered %q -> %s\n", host, entry.name, entry.command)
	verifyRegistration(env, host, entry.name)
	return 0
}

func verifyRegistration(env Env, host, name string) {
	_, _, code, err := hostExec(host, "mcp", "get", name)
	if err != nil || code != 0 {
		fmt.Fprintf(env.Stderr, "%s: warning: post-setup verification could not confirm %q\n", host, name)
		return
	}
	fmt.Fprintf(env.Stdout, "%s: verified %q is registered. Run 'sofarpc-mcp --selftest' to validate the binary.\n", host, name)
}

// codexEntryMatches parses codex's --json output (its own machine-readable
// contract, which we are allowed to read) and compares the registered command
// and SOFARPC_HOME env. The tree is walked rather than bound to one exact
// path, so it works whether codex reports flat or nested under "transport".
// Invalid JSON is not considered a match; setup then requires --force.
func codexEntryMatches(jsonOut string, entry desiredEntry) bool {
	var doc interface{}
	if err := json.Unmarshal([]byte(jsonOut), &doc); err != nil {
		return false
	}
	if !jsonHasStringField(doc, "command", entry.command) {
		return false
	}
	// All desired argv tail values must be present in the registered args.
	// A stale entry pointing at the same binary but without "mcp" would
	// otherwise falsely match yet fail to launch the MCP server.
	for _, arg := range entry.args {
		if !jsonArgsContain(doc, arg) {
			return false
		}
	}
	if entry.homeEnv != "" && !jsonEnvHas(doc, "SOFARPC_HOME", entry.homeEnv) {
		return false
	}
	return true
}

// jsonArgsContain reports whether any "args" array in the tree contains the
// given string value, accommodating flat or transport-nested codex shapes.
func jsonArgsContain(node interface{}, value string) bool {
	switch n := node.(type) {
	case map[string]interface{}:
		if args, ok := n["args"].([]interface{}); ok {
			for _, v := range args {
				if s, ok := v.(string); ok && s == value {
					return true
				}
			}
		}
		for _, v := range n {
			if jsonArgsContain(v, value) {
				return true
			}
		}
	case []interface{}:
		for _, v := range n {
			if jsonArgsContain(v, value) {
				return true
			}
		}
	}
	return false
}

// jsonHasStringField reports whether any object in the tree has key mapping to
// the exact string value.
func jsonHasStringField(node interface{}, key, value string) bool {
	switch n := node.(type) {
	case map[string]interface{}:
		if s, ok := n[key].(string); ok && s == value {
			return true
		}
		for _, v := range n {
			if jsonHasStringField(v, key, value) {
				return true
			}
		}
	case []interface{}:
		for _, v := range n {
			if jsonHasStringField(v, key, value) {
				return true
			}
		}
	}
	return false
}

// jsonEnvHas reports whether the tree carries key=value as an environment
// variable. Codex has used both env maps and env_vars lists across versions, so
// accept either structured shape.
func jsonEnvHas(node interface{}, key, value string) bool {
	switch n := node.(type) {
	case map[string]interface{}:
		if s, ok := n[key].(string); ok && s == value {
			return true
		}
		if k, ok := n["key"].(string); ok && k == key {
			if v, ok := n["value"].(string); ok && v == value {
				return true
			}
		}
		if env, ok := n["env"].(map[string]interface{}); ok {
			if s, ok := env[key].(string); ok && s == value {
				return true
			}
		}
		for _, v := range n {
			if jsonEnvHas(v, key, value) {
				return true
			}
		}
	case []interface{}:
		for _, v := range n {
			if jsonEnvHas(v, key, value) {
				return true
			}
		}
	}
	return false
}

func missingHostCLI(env Env, host string, entry desiredEntry, cause error) int {
	fmt.Fprintf(env.Stderr, "%s: cannot drive %q CLI (%v). Register manually:\n", host, host, cause)
	if host == "codex" {
		args := []string{"mcp", "add"}
		if entry.homeEnv != "" {
			args = append(args, "--env", "SOFARPC_HOME="+entry.homeEnv)
		}
		args = append(args, entry.name, "--", entry.command)
		args = append(args, entry.args...)
		fmt.Fprintf(env.Stderr, "  %s\n", formatDisplayCommand("codex", args))
	} else {
		args := []string{"mcp", "add"}
		if entry.homeEnv != "" {
			args = append(args, "-e", "SOFARPC_HOME="+entry.homeEnv)
		}
		args = append(args, "--scope", "user", entry.name, "--", entry.command)
		args = append(args, entry.args...)
		fmt.Fprintf(env.Stderr, "  %s\n", formatDisplayCommand("claude", args))
	}
	return 1
}

func printPlanned(env Env, host string, args []string) {
	fmt.Fprintf(env.Stdout, "[dry-run] %s\n", formatDisplayCommand(host, args))
}

func formatDisplayCommand(name string, args []string) string {
	parts := make([]string, 0, len(args)+1)
	parts = append(parts, quoteDisplayArg(name))
	for _, arg := range args {
		parts = append(parts, quoteDisplayArg(arg))
	}
	return strings.Join(parts, " ")
}

func quoteDisplayArg(arg string) string {
	if arg == "" {
		return "''"
	}
	if !strings.ContainsAny(arg, " \t\r\n'\"\\$`!#&;()|<>*?[]{}") {
		return arg
	}
	if runtime.GOOS == "windows" {
		return "'" + strings.ReplaceAll(arg, "'", "''") + "'"
	}
	return "'" + strings.ReplaceAll(arg, "'", "'\\''") + "'"
}
