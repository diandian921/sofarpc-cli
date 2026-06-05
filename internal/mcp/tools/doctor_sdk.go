package tools

import (
	"context"
	"encoding/json"
	"io"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/diandian921/sofarpc-mcp/internal/app"
	"github.com/diandian921/sofarpc-mcp/internal/appconfig"
	"github.com/diandian921/sofarpc-mcp/internal/schema"
)

// AddDoctor registers sofarpc_doctor on the SDK server. SDK-native replacement for
// DoctorTool; writeEnabled reflects the server's config-write flag. Reads local
// config/source only, so it needs no app.Service. Handler body mirrors DoctorTool.Run.
func AddDoctor(srv *mcpsdk.Server, writeEnabled bool, stderr io.Writer) {
	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:         "sofarpc_doctor",
		Title:        "SofaRPC Doctor",
		Description:  "Run structured diagnostics for config, project source schema, and invocation prerequisites.",
		Annotations:  &mcpsdk.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true},
		InputSchema:  doctorInputSchema,
		OutputSchema: resultOutputSchema,
	}, adaptTool(stderr, func(ctx context.Context, req *mcpsdk.CallToolRequest, a DoctorArgs) (app.Result, string) {
		var checks []map[string]interface{}
		addCheck := func(name, status string, details map[string]interface{}) {
			if details == nil {
				details = map[string]interface{}{}
			}
			details["name"] = name
			details["status"] = status
			checks = append(checks, details)
		}
		cfg, err := loadConfig()
		if err != nil {
			addCheck("config", "failed", map[string]interface{}{"error": err.Error()})
			return doctorResultSDK(checks)
		}
		path, _ := appconfig.DefaultPath()
		addCheck("config", "ok", map[string]interface{}{"configPath": path, "projectCount": len(cfg.Projects), "serverCount": len(cfg.Servers), "writeEnabled": writeEnabled})

		notifyProgress(ctx, req, "checking project source schema", 0)
		serverName, srv, hasServer, err := resolveServer(cfg, a.Project, a.Server, false)
		if err != nil {
			addCheck("server", "failed", map[string]interface{}{"error": err.Error()})
		} else if hasServer {
			addCheck("server", "ok", map[string]interface{}{"server": serverName, "endpoint": endpointData(srv, srv.TimeoutMS)})
		} else {
			addCheck("server", "skipped", map[string]interface{}{"reason": "no single server resolved"})
		}

		projectName := ""
		var project appconfig.Project
		if hasServer {
			projectName, project, err = resolveProject(cfg, a.Project, serverName)
		} else {
			projectName, project, err = resolveProject(cfg, a.Project, "")
		}
		if err != nil {
			addCheck("project", "failed", map[string]interface{}{"error": err.Error()})
		} else {
			addCheck("project", "ok", map[string]interface{}{"project": projectName, "workspaceRoot": project.WorkspaceRoot, "servicePrefixes": project.ServicePrefixes})
			idx, idxErr := schema.LoadOrBuildIndex(schema.Project{Name: projectName, WorkspaceRoot: project.WorkspaceRoot, ServicePrefixes: project.ServicePrefixes})
			if idxErr != nil {
				addCheck("source_schema", "failed", map[string]interface{}{"error": idxErr.Error()})
			} else {
				addCheck("source_schema", "ok", map[string]interface{}{"methodCount": len(idx.Methods), "typeCount": len(idx.Types)})
				if a.Service != "" {
					desc, descErr := schema.Describe(idx, a.Service, a.Method)
					if descErr != nil {
						addCheck("describe", "failed", map[string]interface{}{"service": a.Service, "method": a.Method, "error": descErr.Error()})
					} else {
						addCheck("describe", "ok", map[string]interface{}{"service": a.Service, "method": a.Method, "methodCount": len(desc.Methods)})
					}
				}
			}
		}
		return doctorResultSDK(checks)
	}))
}

// doctorResultSDK mirrors doctorResult but returns the bare app.Result + summary
// for the SDK adapter (the legacy doctorResult returns a server.Result).
func doctorResultSDK(checks []map[string]interface{}) (app.Result, string) {
	ok := true
	for _, c := range checks {
		if c["status"] == "failed" {
			ok = false
			break
		}
	}
	text := "Doctor completed."
	code := app.CodeSuccess
	if !ok {
		text = "Doctor found issues."
		code = app.CodeInternalError
	}
	body, _ := json.Marshal(map[string]interface{}{"checks": checks})
	return app.Result{OK: ok, Code: code, Data: body}, text
}
