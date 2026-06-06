package tools

import (
	"context"
	"errors"
	"io"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/diandian921/sofarpc-mcp/internal/app"
	"github.com/diandian921/sofarpc-mcp/internal/appconfig"
)

// configFailureResult is the app.Result form of configFailure: it preserves an
// appconfig error's stable code and path so the agent gets a consistent recovery
// hint.
func configFailureResult(err error) app.Result {
	code := app.CodeBadRequest
	var details map[string]interface{}
	var cfgErr *appconfig.ConfigError
	if errors.As(err, &cfgErr) {
		code = cfgErr.Code
		details = map[string]interface{}{"configPath": cfgErr.Path}
	}
	return app.RenderFailure(code, err.Error(), details)
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
			projects = append(projects, map[string]interface{}{"name": name, "project": cfg.Projects[name]})
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
		Annotations:  &mcpsdk.ToolAnnotations{DestructiveHint: boolPtr(false), OpenWorldHint: boolPtr(false)},
		InputSchema:  configSaveProjectInputSchema,
		OutputSchema: configSaveProjectOutputSchema,
	}, adaptTool(stderr, func(_ context.Context, _ *mcpsdk.CallToolRequest, a ConfigSaveProjectArgs) (app.Result, string) {
		if a.Name == "" || a.WorkspaceRoot == "" {
			return app.RenderFailure(app.CodeBadRequest, "name and workspaceRoot are required", nil), ""
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
		Description:  "Add or replace a configured RPC server in config.json.",
		Annotations:  &mcpsdk.ToolAnnotations{DestructiveHint: boolPtr(false), OpenWorldHint: boolPtr(false)},
		InputSchema:  configSaveServerInputSchema,
		OutputSchema: configSaveServerOutputSchema,
	}, adaptTool(stderr, func(_ context.Context, _ *mcpsdk.CallToolRequest, a ConfigSaveServerArgs) (app.Result, string) {
		if a.Name == "" || a.Address == "" || a.Project == "" {
			return app.RenderFailure(app.CodeBadRequest, "name, address and project are required", nil), ""
		}
		srv := appconfig.Server{
			Address:     a.Address,
			Project:     a.Project,
			Protocol:    valueOr(a.Protocol, appconfig.DefaultServerProtocol),
			TimeoutMS:   intOr(a.TimeoutMS, appconfig.DefaultServerTimeoutMS),
			AppName:     valueOr(a.AppName, appconfig.DefaultServerAppName),
			Attachments: a.Attachments,
		}
		path, lock, err := configPaths()
		if err != nil {
			return app.RenderFailure(app.CodeInternalError, err.Error(), nil), ""
		}
		var saved appconfig.Server
		if _, err = appconfig.Update(path, lock, func(cfg *appconfig.Config) error {
			var addErr error
			saved, addErr = cfg.AddServer(a.Name, srv, a.Overwrite)
			return addErr
		}); err != nil {
			return configFailureResult(err), ""
		}
		return okResult(map[string]interface{}{"name": a.Name, "server": publicServer(saved)}), "Server saved to config.json."
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
			return app.RenderFailure(app.CodeBadRequest, "name is required", nil), ""
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
			return app.RenderFailure(app.CodeBadRequest, "name is required", nil), ""
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
