package tools

import (
	"context"
	"fmt"
	"io"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/diandian921/sofarpc-mcp/internal/app"
	"github.com/diandian921/sofarpc-mcp/internal/appconfig"
)

// configFailureResult preserves an appconfig error's stable code and path so
// the agent gets a consistent recovery hint; shared with the CLI via app.
func configFailureResult(err error) app.Result {
	return app.RenderConfigFailure(err)
}

// saveServerData assembles the save_server payload, surfacing activeProfile so
// a save can never silently decide project-level routing (the first saved
// profile becomes active per the config contract).
func saveServerData(cfg appconfig.Config, name string, saved appconfig.Server, prevActive string, dryRun bool) map[string]interface{} {
	data := map[string]interface{}{"name": name, "server": publicServer(saved)}
	if dryRun {
		data["dryRun"] = true
	}
	active, changed, warning := app.ProfileSaveAdvice(cfg, saved, prevActive)
	if saved.Profile != "" {
		data["activeProfile"] = active
	}
	if changed {
		data["activeProfileChanged"] = true
	}
	if warning != "" {
		data["warning"] = warning
	}
	return data
}

func errSetActiveNeedsProfile(name, project string) error {
	return fmt.Errorf("setActive requires a profile: server name %q infers none for project %q (use <project>-<profile> naming or pass profile)", name, project)
}

// AddConfigList registers sofarpc_config_list (read-only). SDK-native replacement
// for ConfigListTool.
func AddConfigList(srv *mcpsdk.Server, writeEnabled bool, stderr io.Writer) {
	srv.AddTool(&mcpsdk.Tool{
		Name:         "sofarpc_config_list",
		Title:        "SofaRPC Config: List",
		Description:  "List configured projects and servers from ~/.sofarpc/config.json.",
		Annotations:  &mcpsdk.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true, DestructiveHint: boolPtr(false), OpenWorldHint: boolPtr(false)},
		InputSchema:  configListInputSchema,
		OutputSchema: configListOutputSchema,
	}, adaptTool(stderr, func(_ context.Context, _ *mcpsdk.CallToolRequest, a ConfigListArgs) (app.Result, string) {
		cfg, err := loadConfig()
		if err != nil {
			return configFailureResult(err), ""
		}
		path, err := appconfig.DefaultPath()
		if err != nil {
			return app.RenderFailure(app.CodeInternalError, err.Error(), nil), ""
		}
		projects := make([]map[string]interface{}, 0, len(cfg.Projects))
		for _, name := range cfg.ProjectNames() {
			if a.Project != "" && name != a.Project {
				continue
			}
			projects = append(projects, map[string]interface{}{"name": name, "project": publicProject(cfg.Projects[name])})
		}
		servers := make([]map[string]interface{}, 0, len(cfg.Servers))
		for _, name := range cfg.ServerNames() {
			srv := cfg.Servers[name]
			if a.Project != "" && srv.Project != a.Project {
				continue
			}
			servers = append(servers, map[string]interface{}{"name": name, "server": publicServer(srv)})
		}
		return okResult(map[string]interface{}{
			"configPath":    path,
			"writeEnabled":  writeEnabled,
			"projects":      projects,
			"servers":       servers,
			"projectFilter": a.Project,
		}), "Config loaded."
	}))
}

// AddConfigSaveProject registers sofarpc_config_save_project. SDK-native
// replacement for ConfigSaveProjectTool.
func AddConfigSaveProject(srv *mcpsdk.Server, stderr io.Writer) {
	srv.AddTool(&mcpsdk.Tool{
		Name:         "sofarpc_config_save_project",
		Title:        "SofaRPC Config: Save Project",
		Description:  "Add or replace a local source project in config.json.",
		Annotations:  &mcpsdk.ToolAnnotations{DestructiveHint: boolPtr(true), OpenWorldHint: boolPtr(false)},
		InputSchema:  configSaveProjectInputSchema,
		OutputSchema: configSaveProjectOutputSchema,
	}, adaptTool(stderr, func(_ context.Context, _ *mcpsdk.CallToolRequest, a ConfigSaveProjectArgs) (app.Result, string) {
		if a.Name == "" || a.WorkspaceRoot == "" {
			return app.RenderFailureAdvised(app.CodeBadRequest, "name and workspaceRoot are required", nil,
				"", "Provide name and workspaceRoot, then call sofarpc_config_save_project again (dryRun=true to preview)."), ""
		}
		if a.DryRun {
			cfg, err := loadConfig()
			if err != nil {
				return configFailureResult(err), ""
			}
			project, err := cfg.AddProject(a.Name, a.WorkspaceRoot, a.ServicePrefixes, a.Overwrite)
			if err != nil {
				return configFailureResult(err), ""
			}
			return okResult(map[string]interface{}{"dryRun": true, "name": a.Name, "project": project}), "Dry run; config.json not modified."
		}
		path, lock, err := configPaths()
		if err != nil {
			return app.RenderFailure(app.CodeInternalError, err.Error(), nil), ""
		}
		var project appconfig.Project
		if _, err = appconfig.Update(path, lock, func(cfg *appconfig.Config) error {
			var addErr error
			project, addErr = cfg.AddProject(a.Name, a.WorkspaceRoot, a.ServicePrefixes, a.Overwrite)
			return addErr
		}); err != nil {
			return configFailureResult(err), ""
		}
		return okResult(map[string]interface{}{"name": a.Name, "project": project}), "Project saved to config.json."
	}))
}

// AddConfigSaveServer registers sofarpc_config_save_server. SDK-native replacement
// for ConfigSaveServerTool.
func AddConfigSaveServer(srv *mcpsdk.Server, stderr io.Writer) {
	srv.AddTool(&mcpsdk.Tool{
		Name:         "sofarpc_config_save_server",
		Title:        "SofaRPC Config: Save Server",
		Description:  "Add or replace a configured RPC server in config.json. The first profile saved for a project becomes its activeProfile (the target of project-level calls); pass setActive=true to switch it explicitly.",
		Annotations:  &mcpsdk.ToolAnnotations{DestructiveHint: boolPtr(true), OpenWorldHint: boolPtr(false)},
		InputSchema:  configSaveServerInputSchema,
		OutputSchema: configSaveServerOutputSchema,
	}, adaptTool(stderr, func(_ context.Context, _ *mcpsdk.CallToolRequest, a ConfigSaveServerArgs) (app.Result, string) {
		if a.Name == "" || a.Address == "" || a.Project == "" {
			return app.RenderFailureAdvised(app.CodeBadRequest, "name, address and project are required", nil,
				"sofarpc_config_list", "Provide name, address (host:port), and project (see sofarpc_config_list for configured projects), then call sofarpc_config_save_server again (dryRun=true to preview)."), ""
		}
		srv := appconfig.Server{
			Address:     a.Address,
			Project:     a.Project,
			Profile:     a.Profile,
			Protocol:    valueOr(a.Protocol, appconfig.DefaultServerProtocol),
			TimeoutMS:   intOr(a.TimeoutMS, appconfig.DefaultServerTimeoutMS),
			AppName:     valueOr(a.AppName, appconfig.DefaultServerAppName),
			Attachments: a.Attachments,
		}
		if a.DryRun {
			cfg, err := loadConfig()
			if err != nil {
				return configFailureResult(err), ""
			}
			prevActive := cfg.Projects[a.Project].ActiveProfile
			saved, err := cfg.AddServer(a.Name, srv, a.Overwrite)
			if err != nil {
				return configFailureResult(err), ""
			}
			if a.SetActive {
				if saved.Profile == "" {
					return configFailureResult(errSetActiveNeedsProfile(a.Name, a.Project)), ""
				}
				if _, err := cfg.SetActiveProfile(saved.Project, saved.Profile); err != nil {
					return configFailureResult(err), ""
				}
			}
			return okResult(saveServerData(cfg, a.Name, saved, prevActive, true)), "Dry run; config.json not modified."
		}
		path, lock, err := configPaths()
		if err != nil {
			return app.RenderFailure(app.CodeInternalError, err.Error(), nil), ""
		}
		var saved appconfig.Server
		var prevActive string
		updated, err := appconfig.Update(path, lock, func(cfg *appconfig.Config) error {
			prevActive = cfg.Projects[a.Project].ActiveProfile
			var addErr error
			saved, addErr = cfg.AddServer(a.Name, srv, a.Overwrite)
			if addErr != nil {
				return addErr
			}
			if a.SetActive {
				if saved.Profile == "" {
					return errSetActiveNeedsProfile(a.Name, a.Project)
				}
				_, setErr := cfg.SetActiveProfile(saved.Project, saved.Profile)
				return setErr
			}
			return nil
		})
		if err != nil {
			return configFailureResult(err), ""
		}
		return okResult(saveServerData(updated, a.Name, saved, prevActive, false)), "Server saved to config.json."
	}))
}

// AddConfigRemoveProject registers sofarpc_config_remove_project (destructive).
// SDK-native replacement for ConfigRemoveProjectTool.
func AddConfigRemoveProject(srv *mcpsdk.Server, stderr io.Writer) {
	srv.AddTool(&mcpsdk.Tool{
		Name:         "sofarpc_config_remove_project",
		Title:        "SofaRPC Config: Remove Project",
		Description:  "Remove a project from config.json. Requires confirm=true.",
		Annotations:  &mcpsdk.ToolAnnotations{DestructiveHint: boolPtr(true), OpenWorldHint: boolPtr(false)},
		InputSchema:  configRemoveProjectInputSchema,
		OutputSchema: configRemoveOutputSchema,
	}, adaptTool(stderr, func(_ context.Context, _ *mcpsdk.CallToolRequest, a ConfigRemoveProjectArgs) (app.Result, string) {
		if a.Name == "" {
			return app.RenderFailureAdvised(app.CodeBadRequest, "name is required", nil,
				"sofarpc_config_list", "Call sofarpc_config_list to see configured projects, then retry with name and confirm=true."), ""
		}
		path, lock, err := configPaths()
		if err != nil {
			return app.RenderFailure(app.CodeInternalError, err.Error(), nil), ""
		}
		if _, err = appconfig.Update(path, lock, func(cfg *appconfig.Config) error {
			return cfg.RemoveProject(a.Name, a.Confirm, a.Cascade)
		}); err != nil {
			return configFailureResult(err), ""
		}
		return okResult(map[string]interface{}{"removed": a.Name}), "Project removed from config.json."
	}))
}

// AddConfigRemoveServer registers sofarpc_config_remove_server (destructive).
// SDK-native replacement for ConfigRemoveServerTool.
func AddConfigRemoveServer(srv *mcpsdk.Server, stderr io.Writer) {
	srv.AddTool(&mcpsdk.Tool{
		Name:         "sofarpc_config_remove_server",
		Title:        "SofaRPC Config: Remove Server",
		Description:  "Remove a server from config.json. Requires confirm=true.",
		Annotations:  &mcpsdk.ToolAnnotations{DestructiveHint: boolPtr(true), OpenWorldHint: boolPtr(false)},
		InputSchema:  configRemoveServerInputSchema,
		OutputSchema: configRemoveOutputSchema,
	}, adaptTool(stderr, func(_ context.Context, _ *mcpsdk.CallToolRequest, a ConfigRemoveServerArgs) (app.Result, string) {
		if a.Name == "" {
			return app.RenderFailureAdvised(app.CodeBadRequest, "name is required", nil,
				"sofarpc_config_list", "Call sofarpc_config_list to see configured servers, then retry with name and confirm=true."), ""
		}
		path, lock, err := configPaths()
		if err != nil {
			return app.RenderFailure(app.CodeInternalError, err.Error(), nil), ""
		}
		if _, err = appconfig.Update(path, lock, func(cfg *appconfig.Config) error {
			return cfg.RemoveServer(a.Name, a.Confirm)
		}); err != nil {
			return configFailureResult(err), ""
		}
		return okResult(map[string]interface{}{"removed": a.Name}), "Server removed from config.json."
	}))
}
