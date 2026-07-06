package app

import (
	"fmt"

	"github.com/diandian921/sofarpc-mcp/internal/appconfig"
)

// ProfileSaveAdvice reports the activeProfile state right after a server save,
// so no save can silently decide project-level routing: AddProfile makes the
// first profile active (the config contract requires an activeProfile once
// profiles exist), and a save whose profile is not active gets an explicit
// warning. changed reports whether this save moved activeProfile; warning is
// empty when the saved profile is the active one. Both the CLI and the MCP
// config surface render these fields, so the wording lives here once.
func ProfileSaveAdvice(cfg appconfig.Config, saved appconfig.Server, prevActive string) (active string, changed bool, warning string) {
	if saved.Profile == "" {
		return "", false, ""
	}
	active = cfg.Projects[saved.Project].ActiveProfile
	changed = active != prevActive
	if saved.Profile != active {
		warning = fmt.Sprintf(
			"profile %q is saved but not active: project-level calls keep resolving activeProfile %q. Pass profile=%q per call, save with setActive=true, or run `sofarpc project use %s %s`.",
			saved.Profile, active, saved.Profile, saved.Project, saved.Profile)
	}
	return active, changed, warning
}
