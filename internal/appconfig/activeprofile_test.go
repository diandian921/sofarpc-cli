package appconfig

import (
	"strings"
	"testing"
)

// TestSetActiveProfileSwitchesAndValidates pins the explicit counterpart to
// AddProfile's implicit first-profile default: switching works, an unknown
// profile lists the known ones, and an unknown project errors.
func TestSetActiveProfileSwitchesAndValidates(t *testing.T) {
	cfg := Config{
		Version: CurrentConfigVersion,
		Projects: map[string]Project{
			"user": {
				WorkspaceRoot: "/tmp/user",
				ActiveProfile: "a",
				Profiles: map[string]Profile{
					"a": {Address: "127.0.0.1:1"},
					"b": {Address: "127.0.0.1:2"},
				},
			},
		},
	}

	project, err := cfg.SetActiveProfile("user", "b")
	if err != nil {
		t.Fatalf("SetActiveProfile: %v", err)
	}
	if project.ActiveProfile != "b" || cfg.Projects["user"].ActiveProfile != "b" {
		t.Fatalf("activeProfile not switched: returned %q, stored %q",
			project.ActiveProfile, cfg.Projects["user"].ActiveProfile)
	}

	if _, err := cfg.SetActiveProfile("user", "nope"); err == nil || !strings.Contains(err.Error(), "a, b") {
		t.Fatalf("unknown profile must list known ones, got %v", err)
	}
	if _, err := cfg.SetActiveProfile("ghost", "a"); err == nil {
		t.Fatal("unknown project must error")
	}
	if cfg.Projects["user"].ActiveProfile != "b" {
		t.Fatal("failed switches must not mutate the config")
	}
}

// TestAddProfileImplicitActiveOnlyForFirst pins the config contract the save
// surfaces warn about: the first profile becomes active, later ones do not.
func TestAddProfileImplicitActiveOnlyForFirst(t *testing.T) {
	cfg := Config{
		Version:  CurrentConfigVersion,
		Projects: map[string]Project{"user": {WorkspaceRoot: "/tmp/user"}},
	}
	if _, err := cfg.AddProfile("user", "a", Profile{Address: "127.0.0.1:1"}, false); err != nil {
		t.Fatalf("add first profile: %v", err)
	}
	if cfg.Projects["user"].ActiveProfile != "a" {
		t.Fatalf("first profile must become active, got %q", cfg.Projects["user"].ActiveProfile)
	}
	if _, err := cfg.AddProfile("user", "b", Profile{Address: "127.0.0.1:2"}, false); err != nil {
		t.Fatalf("add second profile: %v", err)
	}
	if cfg.Projects["user"].ActiveProfile != "a" {
		t.Fatalf("second profile must not steal active, got %q", cfg.Projects["user"].ActiveProfile)
	}
}
