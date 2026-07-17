package app

import (
	"context"
	"fmt"

	"github.com/diandian921/sofarpc-mcp/internal/appconfig"
)

type ProjectSelector struct {
	Project string
	Server  string
}

type ProjectSelection struct {
	Name    string
	Project appconfig.Project
}

type ServerSelector struct {
	Project  string
	Profile  string
	Server   string
	Required bool
}

type ServerSelection struct {
	Name   string
	Server appconfig.Server
	Found  bool
}

func (s *Service) Resolve(ctx context.Context, input ResolveInput) (ResolveResult, error) {
	_ = ctx
	if input.Address != "" {
		timeoutMS := input.TimeoutMS
		if timeoutMS <= 0 {
			timeoutMS = appconfig.DefaultServerTimeoutMS
		}
		endpoint := Endpoint{
			Address:     input.Address,
			Protocol:    appconfig.DefaultServerProtocol,
			TimeoutMS:   timeoutMS,
			AppName:     appconfig.DefaultServerAppName,
			Attachments: map[string]string{},
		}
		return ResolveResult{
			Endpoint: &endpoint,
			Network:  "not_probed",
			Diagnostics: Diagnostics{Resolution: map[string]interface{}{
				"endpointSource": "explicit-address",
				"address":        endpoint.Address,
			}},
		}, nil
	}
	cfg, err := s.loadConfig()
	if err != nil {
		return ResolveResult{}, err
	}
	serverSelection, err := SelectServer(cfg, ServerSelector{
		Project: input.Project, Profile: input.Profile, Server: input.Server,
	})
	if err != nil {
		return ResolveResult{}, err
	}
	if serverSelection.Found {
		projectSelection, err := SelectProject(cfg, ProjectSelector{
			Project: serverSelection.Server.Project,
			Server:  serverSelection.Name,
		})
		if err != nil {
			return ResolveResult{}, err
		}
		timeoutMS := input.TimeoutMS
		if timeoutMS <= 0 {
			timeoutMS = serverSelection.Server.TimeoutMS
		}
		endpoint := endpointFromServer(serverSelection.Name, serverSelection.Server, timeoutMS)
		return ResolveResult{
			Project:     ProjectRef{Name: projectSelection.Name, Info: projectSelection.Project},
			Profile:     serverSelection.Server.Profile,
			Server:      serverSelection.Name,
			Endpoint:    &endpoint,
			Network:     "not_probed",
			Diagnostics: resolutionDiagnostics(projectSelection.Name, serverSelection.Name, endpoint),
		}, nil
	}
	projectSelection, err := SelectProject(cfg, ProjectSelector{Project: input.Project})
	if err != nil {
		return ResolveResult{}, err
	}
	return ResolveResult{
		Project: ProjectRef{Name: projectSelection.Name, Info: projectSelection.Project},
		Servers: boundServers(cfg, projectSelection.Name),
		Network: "not_probed",
		Diagnostics: Diagnostics{Resolution: map[string]interface{}{
			"project": projectSelection.Name,
			"profile": input.Profile,
			"server":  "",
		}},
	}, nil
}

func SelectServer(cfg appconfig.Config, selector ServerSelector) (ServerSelection, error) {
	if selector.Server != "" {
		server, ok := cfg.Servers[selector.Server]
		if !ok {
			return ServerSelection{}, &DomainError{Kind: ErrServerNotFound, Message: fmt.Sprintf("server %q not found", selector.Server), Details: map[string]interface{}{"server": selector.Server}}
		}
		if selector.Project != "" && server.Project != selector.Project {
			return ServerSelection{}, &DomainError{Kind: ErrServerNotFound, Message: fmt.Sprintf("server %q is bound to project %q, not %q", selector.Server, server.Project, selector.Project), Details: map[string]interface{}{"server": selector.Server, "project": selector.Project, "actualProject": server.Project}}
		}
		if selector.Profile != "" && server.Profile != selector.Profile {
			return ServerSelection{}, &DomainError{Kind: ErrServerNotFound, Message: fmt.Sprintf("server %q is bound to profile %q, not %q", selector.Server, server.Profile, selector.Profile), Details: map[string]interface{}{"server": selector.Server, "profile": selector.Profile, "actualProfile": server.Profile}}
		}
		return ServerSelection{Name: selector.Server, Server: server, Found: true}, nil
	}

	if selector.Profile != "" {
		if selector.Project == "" {
			return ServerSelection{}, &DomainError{Kind: ErrProjectNotFound, Message: "project is required when profile is specified", Details: map[string]interface{}{"profile": selector.Profile}}
		}
		name := appconfig.ServerNameForProfile(selector.Project, selector.Profile)
		server, ok := cfg.Servers[name]
		if !ok {
			return ServerSelection{}, &DomainError{Kind: ErrEndpointNotFound, Message: fmt.Sprintf("profile %q for project %q not found", selector.Profile, selector.Project), Details: map[string]interface{}{"project": selector.Project, "profile": selector.Profile, "candidates": serverCandidates(cfg, serverNamesForProject(cfg, selector.Project))}}
		}
		return ServerSelection{Name: name, Server: server, Found: true}, nil
	}

	if selector.Project != "" {
		if p, ok := cfg.Projects[selector.Project]; ok && p.ActiveProfile != "" {
			name := appconfig.ServerNameForProfile(selector.Project, p.ActiveProfile)
			if server, ok := cfg.Servers[name]; ok {
				return ServerSelection{Name: name, Server: server, Found: true}, nil
			}
		}
	}

	var names []string
	for _, name := range cfg.ServerNames() {
		server := cfg.Servers[name]
		if selector.Project == "" || server.Project == selector.Project {
			names = append(names, name)
		}
	}
	if len(names) == 1 {
		name := names[0]
		return ServerSelection{Name: name, Server: cfg.Servers[name], Found: true}, nil
	}
	if !selector.Required {
		return ServerSelection{}, nil
	}
	candidates := serverCandidates(cfg, names)
	if selector.Project != "" {
		return ServerSelection{}, &DomainError{Kind: ErrEndpointNotFound, Message: fmt.Sprintf("server is required because project %q has %d configured servers", selector.Project, len(names)), Details: map[string]interface{}{"project": selector.Project, "serverCount": len(names), "candidates": candidates}}
	}
	return ServerSelection{}, &DomainError{Kind: ErrEndpointNotFound, Message: fmt.Sprintf("server is required because %d servers are configured", len(names)), Details: map[string]interface{}{"serverCount": len(names), "candidates": candidates}}
}

func serverNamesForProject(cfg appconfig.Config, project string) []string {
	var names []string
	for _, name := range cfg.ServerNames() {
		server := cfg.Servers[name]
		if project == "" || server.Project == project {
			names = append(names, name)
		}
	}
	return names
}

// serverCandidates lists the ambiguous-match servers (name/project/address) so the
// ENDPOINT_NOT_FOUND recovery can name which servers to choose between. Attachments are
// deliberately excluded — only the non-secret routing fields are surfaced.
func serverCandidates(cfg appconfig.Config, names []string) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(names))
	for _, name := range names {
		s := cfg.Servers[name]
		out = append(out, map[string]interface{}{
			"server":  name,
			"project": s.Project,
			"profile": s.Profile,
			"address": s.Address,
		})
	}
	return out
}

func SelectProject(cfg appconfig.Config, selector ProjectSelector) (ProjectSelection, error) {
	if selector.Project != "" {
		if selector.Server != "" {
			server, ok := cfg.Servers[selector.Server]
			if !ok {
				return ProjectSelection{}, &DomainError{Kind: ErrServerNotFound, Message: fmt.Sprintf("server %q not found", selector.Server), Details: map[string]interface{}{"server": selector.Server}}
			}
			if server.Project != selector.Project {
				return ProjectSelection{}, &DomainError{Kind: ErrProjectNotFound, Message: fmt.Sprintf("server %q is bound to project %q, not %q", selector.Server, server.Project, selector.Project), Details: map[string]interface{}{"server": selector.Server, "project": selector.Project, "actualProject": server.Project}}
			}
		}
		project, ok := cfg.Projects[selector.Project]
		if !ok {
			return ProjectSelection{}, &DomainError{Kind: ErrProjectNotFound, Message: fmt.Sprintf("project %q not found", selector.Project), Details: map[string]interface{}{"project": selector.Project}}
		}
		return ProjectSelection{Name: selector.Project, Project: project}, nil
	}
	if selector.Server != "" {
		server, ok := cfg.Servers[selector.Server]
		if !ok {
			return ProjectSelection{}, &DomainError{Kind: ErrServerNotFound, Message: fmt.Sprintf("server %q not found", selector.Server), Details: map[string]interface{}{"server": selector.Server}}
		}
		project, ok := cfg.Projects[server.Project]
		if !ok {
			return ProjectSelection{}, &DomainError{Kind: ErrProjectNotFound, Message: fmt.Sprintf("server %q references missing project %q", selector.Server, server.Project), Details: map[string]interface{}{"server": selector.Server, "project": server.Project}}
		}
		return ProjectSelection{Name: server.Project, Project: project}, nil
	}
	if len(cfg.Projects) == 1 {
		for name, project := range cfg.Projects {
			return ProjectSelection{Name: name, Project: project}, nil
		}
	}
	return ProjectSelection{}, &DomainError{Kind: ErrProjectNotFound, Message: "project is required"}
}

func endpointFromServer(name string, server appconfig.Server, timeoutMS int) Endpoint {
	if timeoutMS <= 0 {
		timeoutMS = server.TimeoutMS
	}
	return Endpoint{
		Server:      name,
		Project:     server.Project,
		Profile:     server.Profile,
		Address:     server.Address,
		Protocol:    server.Protocol,
		TimeoutMS:   timeoutMS,
		AppName:     server.AppName,
		Attachments: server.Attachments,
	}
}

func boundServers(cfg appconfig.Config, project string) []map[string]interface{} {
	servers := []map[string]interface{}{}
	for _, name := range cfg.ServerNames() {
		server := cfg.Servers[name]
		if server.Project != project {
			continue
		}
		servers = append(servers, map[string]interface{}{"name": name, "server": server})
	}
	return servers
}

func resolutionDiagnostics(project, server string, endpoint Endpoint) Diagnostics {
	return Diagnostics{Resolution: map[string]interface{}{
		"project":        project,
		"profile":        endpoint.Profile,
		"server":         server,
		"endpointSource": "configured-server",
		"address":        endpoint.Address,
	}}
}
