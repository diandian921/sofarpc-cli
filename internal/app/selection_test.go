package app

import (
	"errors"
	"strings"
	"testing"

	"github.com/diandian921/sofarpc-mcp/internal/appconfig"
)

func selectionTestConfig() appconfig.Config {
	return appconfig.Config{
		Projects: map[string]appconfig.Project{
			"user":  {WorkspaceRoot: "/ws/user"},
			"other": {WorkspaceRoot: "/ws/other"},
		},
		Servers: map[string]appconfig.Server{
			"user-dev":  {Address: "127.0.0.1:1", Project: "user", Profile: "dev"},
			"user-test": {Address: "127.0.0.1:2", Project: "user", Profile: "test"},
			"solo":      {Address: "127.0.0.1:3", Project: "other"},
		},
	}
}

func TestSelectProject(t *testing.T) {
	cfg := selectionTestConfig()
	tests := []struct {
		name       string
		selector   ProjectSelector
		want       string
		wantKind   ErrorKind
		wantDetail string
	}{
		{name: "explicit", selector: ProjectSelector{Project: "user"}, want: "user"},
		{name: "explicit missing", selector: ProjectSelector{Project: "ghost"}, wantKind: ErrProjectNotFound, wantDetail: "project"},
		{name: "matching server", selector: ProjectSelector{Project: "user", Server: "user-dev"}, want: "user"},
		{name: "mismatched server", selector: ProjectSelector{Project: "other", Server: "user-dev"}, wantKind: ErrProjectNotFound, wantDetail: "actualProject"},
		{name: "missing server", selector: ProjectSelector{Project: "user", Server: "ghost"}, wantKind: ErrServerNotFound, wantDetail: "server"},
		{name: "server derived", selector: ProjectSelector{Server: "solo"}, want: "other"},
		{name: "server missing", selector: ProjectSelector{Server: "ghost"}, wantKind: ErrServerNotFound, wantDetail: "server"},
		{name: "ambiguous", selector: ProjectSelector{}, wantKind: ErrProjectNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := SelectProject(cfg, tt.selector)
			if tt.wantKind == "" {
				if err != nil || got.Name != tt.want {
					t.Fatalf("SelectProject() = %+v, %v; want %q", got, err, tt.want)
				}
				return
			}
			assertSelectionError(t, err, tt.wantKind, tt.wantDetail)
		})
	}
}

func TestSelectProjectSingleAndDanglingServer(t *testing.T) {
	cfg := appconfig.Config{Projects: map[string]appconfig.Project{"only": {WorkspaceRoot: "/ws"}}}
	got, err := SelectProject(cfg, ProjectSelector{})
	if err != nil || got.Name != "only" || got.Project.WorkspaceRoot != "/ws" {
		t.Fatalf("single project = %+v, %v", got, err)
	}

	cfg.Servers = map[string]appconfig.Server{"orphan": {Project: "gone"}}
	_, err = SelectProject(cfg, ProjectSelector{Server: "orphan"})
	assertSelectionError(t, err, ErrProjectNotFound, "project")
	if !strings.Contains(err.Error(), "missing project") {
		t.Fatalf("dangling error = %v", err)
	}
}

func TestSelectServer(t *testing.T) {
	cfg := selectionTestConfig()
	tests := []struct {
		name       string
		selector   ServerSelector
		want       string
		wantFound  bool
		wantKind   ErrorKind
		wantDetail string
	}{
		{name: "explicit", selector: ServerSelector{Server: "solo"}, want: "solo", wantFound: true},
		{name: "explicit missing", selector: ServerSelector{Server: "ghost"}, wantKind: ErrServerNotFound, wantDetail: "server"},
		{name: "project mismatch", selector: ServerSelector{Server: "solo", Project: "user"}, wantKind: ErrServerNotFound, wantDetail: "actualProject"},
		{name: "profile mismatch", selector: ServerSelector{Server: "user-dev", Profile: "test"}, wantKind: ErrServerNotFound, wantDetail: "actualProfile"},
		{name: "profile needs project", selector: ServerSelector{Profile: "dev"}, wantKind: ErrProjectNotFound, wantDetail: "profile"},
		{name: "project profile", selector: ServerSelector{Project: "user", Profile: "dev"}, want: "user-dev", wantFound: true},
		{name: "profile missing", selector: ServerSelector{Project: "user", Profile: "prod"}, wantKind: ErrEndpointNotFound, wantDetail: "candidates"},
		{name: "single project match", selector: ServerSelector{Project: "other"}, want: "solo", wantFound: true},
		{name: "ambiguous optional", selector: ServerSelector{Project: "user"}},
		{name: "ambiguous required", selector: ServerSelector{Project: "user", Required: true}, wantKind: ErrEndpointNotFound, wantDetail: "candidates"},
		{name: "ambiguous global", selector: ServerSelector{Required: true}, wantKind: ErrEndpointNotFound, wantDetail: "serverCount"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := SelectServer(cfg, tt.selector)
			if tt.wantKind == "" {
				if err != nil || got.Name != tt.want || got.Found != tt.wantFound {
					t.Fatalf("SelectServer() = %+v, %v; want name=%q found=%v", got, err, tt.want, tt.wantFound)
				}
				return
			}
			assertSelectionError(t, err, tt.wantKind, tt.wantDetail)
		})
	}
}

func TestSelectServerUsesActiveProfile(t *testing.T) {
	cfg := selectionTestConfig()
	project := cfg.Projects["user"]
	project.ActiveProfile = "test"
	cfg.Projects["user"] = project
	got, err := SelectServer(cfg, ServerSelector{Project: "user", Required: true})
	if err != nil || !got.Found || got.Name != "user-test" || got.Server.Address != "127.0.0.1:2" {
		t.Fatalf("active profile selection = %+v, %v", got, err)
	}
}

func assertSelectionError(t *testing.T, err error, kind ErrorKind, detail string) {
	t.Helper()
	var domain *DomainError
	if !errors.As(err, &domain) || domain.Kind != kind {
		t.Fatalf("error = %v; want DomainError kind %s", err, kind)
	}
	if detail != "" {
		if _, ok := domain.Details[detail]; !ok {
			t.Fatalf("error details = %#v; missing %q", domain.Details, detail)
		}
	}
}
