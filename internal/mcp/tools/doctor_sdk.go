package tools

import (
	"context"
	"encoding/json"
	"io"
	"strings"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/diandian921/sofarpc-mcp/internal/app"
	"github.com/diandian921/sofarpc-mcp/internal/appconfig"
	"github.com/diandian921/sofarpc-mcp/internal/schema"
)

// AddDoctor registers sofarpc_doctor on the SDK server. SDK-native replacement for
// DoctorTool; writeEnabled reflects the server's config-write flag. Reads local
// config/source only, so it needs no app.Service. Handler body mirrors DoctorTool.Run.
func AddDoctor(srv *mcpsdk.Server, writeEnabled bool, stderr io.Writer) {
	srv.AddTool(&mcpsdk.Tool{
		Name:         "sofarpc_doctor",
		Title:        "SofaRPC Doctor",
		Description:  "Run structured diagnostics for config, project source schema, and invocation prerequisites.",
		Annotations:  &mcpsdk.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true, DestructiveHint: boolPtr(false), OpenWorldHint: boolPtr(false)},
		InputSchema:  doctorInputSchema,
		OutputSchema: doctorOutputSchema,
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
	var failed []string
	for _, c := range checks {
		if c["status"] == "failed" {
			if name, _ := c["name"].(string); name != "" {
				failed = append(failed, name)
			}
		}
	}
	body, _ := json.Marshal(map[string]interface{}{"checks": checks})
	if len(failed) == 0 {
		return app.Result{OK: true, Code: app.CodeSuccess, Data: body}, "Doctor completed."
	}
	// A failed check is a real isError result, so honor the universal recovery
	// contract: keep data.checks and attach an explicit nextTool + recovery. The
	// code-derived advice for INTERNAL_ERROR would point back at sofarpc_doctor
	// itself, so the recovery tool is chosen from the first failed check instead.
	return app.Result{
		OK:   false,
		Code: app.CodeInternalError,
		Data: body,
		Error: &app.ResultError{
			Message:  "doctor found failing checks: " + strings.Join(failed, ", "),
			NextTool: doctorRecoveryTool(failed[0]),
			Recovery: "Review data.checks; each failed check names the config or source problem to fix.",
			Details:  map[string]interface{}{"failedChecks": failed},
		},
	}, "Doctor found issues."
}

// doctorRecoveryTool maps the first failed doctor check to the tool that best helps fix
// it. Most doctor failures are config/source, so it defaults to sofarpc_config_list.
func doctorRecoveryTool(check string) string {
	switch check {
	case "server":
		return "sofarpc_resolve"
	case "source_schema", "describe":
		return "sofarpc_describe"
	default: // config, project
		return "sofarpc_config_list"
	}
}
