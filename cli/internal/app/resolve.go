package app

import (
	"context"
	"fmt"

	"github.com/diandian921/sofarpc-cli/cli/internal/appconfig"
)

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
	serverName, server, hasServer, err := resolveServer(cfg, input.Project, input.Server, false)
	if err != nil {
		return ResolveResult{}, err
	}
	if hasServer {
		projectName, project, err := resolveProject(cfg, server.Project, serverName)
		if err != nil {
			return ResolveResult{}, err
		}
		timeoutMS := input.TimeoutMS
		if timeoutMS <= 0 {
			timeoutMS = server.TimeoutMS
		}
		endpoint := endpointFromServer(serverName, server, timeoutMS)
		return ResolveResult{
			Project:     ProjectRef{Name: projectName, Info: project},
			Server:      serverName,
			Endpoint:    &endpoint,
			Network:     "not_probed",
			Diagnostics: resolutionDiagnostics(projectName, serverName, endpoint),
		}, nil
	}
	projectName, project, err := resolveProject(cfg, input.Project, "")
	if err != nil {
		return ResolveResult{}, err
	}
	return ResolveResult{
		Project: ProjectRef{Name: projectName, Info: project},
		Servers: boundServers(cfg, projectName),
		Network: "not_probed",
		Diagnostics: Diagnostics{Resolution: map[string]interface{}{
			"project": projectName,
			"server":  "",
		}},
	}, nil
}

func resolveServer(cfg appconfig.Config, project, explicit string, required bool) (string, appconfig.Server, bool, error) {
	if explicit != "" {
		server, ok := cfg.Servers[explicit]
		if !ok {
			return "", appconfig.Server{}, false, &DomainError{Kind: ErrServerNotFound, Message: fmt.Sprintf("server %q not found", explicit), Details: map[string]interface{}{"server": explicit}}
		}
		if project != "" && server.Project != project {
			return "", appconfig.Server{}, false, &DomainError{Kind: ErrServerNotFound, Message: fmt.Sprintf("server %q is bound to project %q, not %q", explicit, server.Project, project), Details: map[string]interface{}{"server": explicit, "project": project, "actualProject": server.Project}}
		}
		return explicit, server, true, nil
	}

	var names []string
	for _, name := range cfg.ServerNames() {
		server := cfg.Servers[name]
		if project == "" || server.Project == project {
			names = append(names, name)
		}
	}
	if len(names) == 1 {
		name := names[0]
		return name, cfg.Servers[name], true, nil
	}
	if !required {
		return "", appconfig.Server{}, false, nil
	}
	if project != "" {
		return "", appconfig.Server{}, false, &DomainError{Kind: ErrEndpointNotFound, Message: fmt.Sprintf("server is required because project %q has %d configured servers", project, len(names)), Details: map[string]interface{}{"project": project, "serverCount": len(names)}}
	}
	return "", appconfig.Server{}, false, &DomainError{Kind: ErrEndpointNotFound, Message: fmt.Sprintf("server is required because %d servers are configured", len(names)), Details: map[string]interface{}{"serverCount": len(names)}}
}

func resolveProject(cfg appconfig.Config, explicit, serverName string) (string, appconfig.Project, error) {
	if explicit != "" {
		if serverName != "" {
			server, ok := cfg.Servers[serverName]
			if !ok {
				return "", appconfig.Project{}, &DomainError{Kind: ErrServerNotFound, Message: fmt.Sprintf("server %q not found", serverName), Details: map[string]interface{}{"server": serverName}}
			}
			if server.Project != explicit {
				return "", appconfig.Project{}, &DomainError{Kind: ErrProjectNotFound, Message: fmt.Sprintf("server %q is bound to project %q, not %q", serverName, server.Project, explicit), Details: map[string]interface{}{"server": serverName, "project": explicit, "actualProject": server.Project}}
			}
		}
		project, ok := cfg.Projects[explicit]
		if !ok {
			return "", appconfig.Project{}, &DomainError{Kind: ErrProjectNotFound, Message: fmt.Sprintf("project %q not found", explicit), Details: map[string]interface{}{"project": explicit}}
		}
		return explicit, project, nil
	}
	if serverName != "" {
		server, ok := cfg.Servers[serverName]
		if !ok {
			return "", appconfig.Project{}, &DomainError{Kind: ErrServerNotFound, Message: fmt.Sprintf("server %q not found", serverName), Details: map[string]interface{}{"server": serverName}}
		}
		project, ok := cfg.Projects[server.Project]
		if !ok {
			return "", appconfig.Project{}, &DomainError{Kind: ErrProjectNotFound, Message: fmt.Sprintf("server %q references missing project %q", serverName, server.Project), Details: map[string]interface{}{"server": serverName, "project": server.Project}}
		}
		return server.Project, project, nil
	}
	if len(cfg.Projects) == 1 {
		for name, project := range cfg.Projects {
			return name, project, nil
		}
	}
	return "", appconfig.Project{}, &DomainError{Kind: ErrProjectNotFound, Message: "project is required"}
}

func endpointFromServer(name string, server appconfig.Server, timeoutMS int) Endpoint {
	if timeoutMS <= 0 {
		timeoutMS = server.TimeoutMS
	}
	return Endpoint{
		Server:      name,
		Project:     server.Project,
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
		"server":         server,
		"endpointSource": "configured-server",
		"address":        endpoint.Address,
	}}
}
