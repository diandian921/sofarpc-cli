package cli

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"

	"github.com/diandian921/sofarpc-mcp/internal/appconfig"
)

// executablePath resolves the running binary. Package var so tests can stub it.
var executablePath = os.Executable

// binVersion runs "<path> <versionArg>" and returns the trimmed output. It is a
// package var so tests can stub external version probing.
var binVersion = func(path string, versionArg string) (string, error) {
	out, err := exec.Command(path, versionArg).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

var mkdirAll = os.MkdirAll

func runSelfInstall(args []string, env Env) int {
	fs := flag.NewFlagSet("self-install", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	allowDowngrade := fs.Bool("allow-downgrade", false, "permit installing an older version over a newer one")
	force := fs.Bool("force", false, "implies --allow-downgrade")
	uninstall := fs.Bool("uninstall", false, "remove installed binaries, keep config and cache")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	root, err := appconfig.InstallRoot()
	if err != nil {
		fmt.Fprintf(env.Stderr, "self-install: resolve install root: %v\n", err)
		return 1
	}
	binDir := filepath.Join(root, "bin")
	ext := exeExt()

	if *uninstall {
		for _, name := range installedBinaryNames() {
			_ = os.Remove(filepath.Join(binDir, name+ext))
		}
		fmt.Fprintf(env.Stdout, "Uninstalled binaries. Kept config and cache under %s\n", root)
		return 0
	}

	selfSrc, err := executablePath()
	if err != nil {
		fmt.Fprintf(env.Stderr, "self-install: locate running binary: %v\n", err)
		return 1
	}
	if resolved, rerr := filepath.EvalSymlinks(selfSrc); rerr == nil {
		selfSrc = resolved
	}

	target := filepath.Join(binDir, "sofarpc"+ext)
	switch decideInstall(env.BuildVersion, target, *allowDowngrade || *force) {
	case installNoop:
		if err := ensureScaffold(env, root, binDir); err != nil {
			fmt.Fprintf(env.Stderr, "self-install: %v\n", err)
			return 1
		}
		removeLegacyBinaries(binDir, ext)
		fmt.Fprintf(env.Stdout, "Already installed at version %s; nothing to do.\n", env.BuildVersion)
		return 0
	case installBlocked:
		fmt.Fprintf(env.Stderr, "self-install: refusing to install older version %s over a newer one; pass --allow-downgrade or --force\n", env.BuildVersion)
		return 1
	}

	if err := ensureScaffold(env, root, binDir); err != nil {
		fmt.Fprintf(env.Stderr, "self-install: %v\n", err)
		return 1
	}
	if err := copyExecutable(selfSrc, target); err != nil {
		fmt.Fprintf(env.Stderr, "self-install: install sofarpc: %v\n", err)
		return 1
	}
	removeLegacyBinaries(binDir, ext)
	deQuarantine(target)

	fmt.Fprintf(env.Stdout, "Installed:\n  %s\n", target)
	printPathHint(env, binDir)
	return 0
}

type installDecision int

const (
	installProceed installDecision = iota
	installNoop
	installBlocked
)

// decideInstall compares the source binary version against the installed
// sofarpc target: same version → no-op; semver older without --allow-downgrade
// → blocked; otherwise → proceed.
func decideInstall(srcVersion, sofarpcTarget string, downgradeAllowed bool) installDecision {
	if !fileExists(sofarpcTarget) {
		return installProceed
	}
	tgtVersion, err := binVersion(sofarpcTarget, "version")
	if err != nil {
		return installProceed
	}
	if tgtVersion == srcVersion {
		return installNoop
	}
	cmp, comparable := compareSemver(srcVersion, tgtVersion)
	if comparable && cmp < 0 && !downgradeAllowed {
		return installBlocked
	}
	return installProceed
}

func ensureScaffold(env Env, root, binDir string) error {
	for _, dir := range []string{binDir, filepath.Join(root, "cache", "schema")} {
		if err := mkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	configPath := filepath.Join(root, "config.json")
	if !fileExists(configPath) {
		if err := appconfig.Save(configPath, appconfig.DefaultConfig()); err != nil {
			return fmt.Errorf("create config.json: %w", err)
		}
	}
	_ = env
	return nil
}

func installedBinaryNames() []string {
	return []string{"sofarpc", "sofarpc-mcp", "sofarpc-cli"}
}

// removeLegacyBinaries best-effort deletes binaries from previous layouts.
// Only fixed names that we ourselves used to install are touched — no
// recursive scan, never any user-placed file.
func removeLegacyBinaries(binDir, ext string) {
	for _, name := range []string{"sofarpc-mcp", "sofarpc-cli"} {
		_ = os.Remove(filepath.Join(binDir, name+ext))
	}
}

func copyExecutable(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".install-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := io.Copy(tmp, in); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Chmod(tmpName, 0o755); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, dst); err != nil {
		// Windows rename cannot replace an existing (or briefly locked) file as
		// reliably as POSIX; remove the destination and retry once.
		if runtime.GOOS == "windows" {
			_ = os.Remove(dst)
			if rerr := os.Rename(tmpName, dst); rerr != nil {
				_ = os.Remove(tmpName)
				return rerr
			}
			return nil
		}
		_ = os.Remove(tmpName)
		return err
	}
	return nil
}

// deQuarantine best-effort strips the macOS quarantine attribute from freshly
// copied binaries. The running binary already passed Gatekeeper, so it may
// de-quarantine its siblings. Errors are ignored by design.
func deQuarantine(paths ...string) {
	if runtime.GOOS != "darwin" {
		return
	}
	for _, p := range paths {
		_ = exec.Command("xattr", "-d", "com.apple.quarantine", p).Run()
	}
}

func printPathHint(env Env, binDir string) {
	for _, entry := range filepath.SplitList(os.Getenv("PATH")) {
		if entry == binDir {
			return
		}
	}
	if runtime.GOOS == "windows" {
		fmt.Fprintf(env.Stdout, "\n%s is not on PATH. Add it, e.g.:\n  setx PATH \"%%PATH%%;%s\"\n", binDir, binDir)
		return
	}
	fmt.Fprintf(env.Stdout, "\n%s is not on PATH. Add this to your shell rc:\n  export PATH=\"%s:$PATH\"\n", binDir, binDir)
}

func exeExt() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// compareSemver compares two vMAJOR.MINOR.PATCH strings (optional leading "v",
// optional semver prerelease/build metadata). It returns (-1|0|1, true) when
// both parse, or (0, false) when either is not semver such as "dev" or a git
// describe string.
// gitDescribeDev matches a git-describe development version suffix ("-<commits>-g<hash>",
// optionally "-dirty"), e.g. v0.1.0-beta.9-18-gdd32f79. These do not order cleanly against
// release tags under semver precedence (semver ranks a numeric prerelease identifier below
// an alphanumeric one, so beta.10 sorts below beta.9-18-gHASH). Treating them as not
// comparable keeps the downgrade guard from wrongly refusing a release over a dev build.
var gitDescribeDev = regexp.MustCompile(`-\d+-g[0-9a-f]+(-dirty)?$`)

func compareSemver(a, b string) (int, bool) {
	if gitDescribeDev.MatchString(a) || gitDescribeDev.MatchString(b) {
		return 0, false
	}
	pa, oka := parseSemver(a)
	pb, okb := parseSemver(b)
	if !oka || !okb {
		return 0, false
	}
	for i := 0; i < 3; i++ {
		if pa.core[i] != pb.core[i] {
			if pa.core[i] < pb.core[i] {
				return -1, true
			}
			return 1, true
		}
	}
	if cmp := comparePrerelease(pa.pre, pb.pre); cmp != 0 {
		return cmp, true
	}
	return 0, true
}

type semVersion struct {
	core [3]int
	pre  string
}

func parseSemver(v string) (semVersion, bool) {
	var out semVersion
	v = strings.TrimPrefix(v, "v")
	if i := strings.IndexByte(v, '+'); i >= 0 {
		v = v[:i]
	}
	if i := strings.IndexByte(v, '-'); i >= 0 {
		out.pre = v[i+1:]
		v = v[:i]
	}
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return out, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return out, false
		}
		out.core[i] = n
	}
	return out, true
}

func comparePrerelease(a, b string) int {
	switch {
	case a == "" && b == "":
		return 0
	case a == "":
		return 1
	case b == "":
		return -1
	}
	ap := strings.Split(a, ".")
	bp := strings.Split(b, ".")
	for i := 0; i < len(ap) && i < len(bp); i++ {
		if ap[i] == bp[i] {
			continue
		}
		an, aNum := parsePrereleaseNumber(ap[i])
		bn, bNum := parsePrereleaseNumber(bp[i])
		switch {
		case aNum && bNum:
			if an < bn {
				return -1
			}
			return 1
		case aNum:
			return -1
		case bNum:
			return 1
		case ap[i] < bp[i]:
			return -1
		default:
			return 1
		}
	}
	if len(ap) < len(bp) {
		return -1
	}
	if len(ap) > len(bp) {
		return 1
	}
	return 0
}

func parsePrereleaseNumber(s string) (int, bool) {
	if s == "" {
		return 0, false
	}
	n, err := strconv.Atoi(s)
	return n, err == nil
}
