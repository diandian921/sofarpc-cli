package app

import (
	"context"
	"testing"

	"github.com/diandian921/sofarpc-mcp/internal/appconfig"
)

func profileTestConfig(t *testing.T) appconfig.Config {
	t.Helper()
	cfg := appconfig.DefaultConfig()
	if _, err := cfg.AddProject("salesfundmp", t.TempDir(), []string{"com.example.facade"}, false); err != nil {
		t.Fatalf("AddProject: %v", err)
	}
	if _, err := cfg.AddProfile("salesfundmp", "local", appconfig.Profile{Address: "127.0.0.1:12300"}, false); err != nil {
		t.Fatalf("AddProfile local: %v", err)
	}
	if _, err := cfg.AddProfile("salesfundmp", "test", appconfig.Profile{Address: "10.74.194.40:12200"}, false); err != nil {
		t.Fatalf("AddProfile test: %v", err)
	}
	project := cfg.Projects["salesfundmp"]
	project.ActiveProfile = "test"
	cfg.Projects["salesfundmp"] = project
	return cfg
}

func TestResolveUsesProjectActiveProfile(t *testing.T) {
	service := New(fakeStore{cfg: profileTestConfig(t)})

	resolved, err := service.Resolve(context.Background(), ResolveInput{Project: "salesfundmp"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if resolved.Profile != "test" || resolved.Server != "salesfundmp-test" {
		t.Fatalf("resolved profile/server = %q/%q", resolved.Profile, resolved.Server)
	}
	if resolved.Endpoint == nil || resolved.Endpoint.Address != "10.74.194.40:12200" || resolved.Endpoint.Profile != "test" {
		t.Fatalf("endpoint = %+v", resolved.Endpoint)
	}
}

func TestResolveAcceptsExplicitProjectProfile(t *testing.T) {
	service := New(fakeStore{cfg: profileTestConfig(t)})

	resolved, err := service.Resolve(context.Background(), ResolveInput{Project: "salesfundmp", Profile: "local"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if resolved.Profile != "local" || resolved.Server != "salesfundmp-local" {
		t.Fatalf("resolved profile/server = %q/%q", resolved.Profile, resolved.Server)
	}
	if resolved.Endpoint == nil || resolved.Endpoint.Address != "127.0.0.1:12300" || resolved.Endpoint.Profile != "local" {
		t.Fatalf("endpoint = %+v", resolved.Endpoint)
	}
}

func TestPlanInvocationAcceptsProjectProfile(t *testing.T) {
	service := New(fakeStore{cfg: profileTestConfig(t)})

	plan, err := service.PlanInvocation(context.Background(), InvocationInput{
		Project:             "salesfundmp",
		Profile:             "local",
		Service:             "com.example.facade.UserFacade",
		Method:              "getUser",
		ParamTypes:          []string{"java.lang.String"},
		OrderedArguments:    []interface{}{"u001"},
		HasOrderedArguments: true,
	})
	if err != nil {
		t.Fatalf("PlanInvocation: %v", err)
	}
	if plan.Profile != "local" || plan.Server != "salesfundmp-local" || plan.Endpoint.Profile != "local" {
		t.Fatalf("plan profile/server/endpoint = %q/%q/%+v", plan.Profile, plan.Server, plan.Endpoint)
	}
}
