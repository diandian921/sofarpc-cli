package tools

import (
	"context"
	"encoding/json"

	"github.com/diandian921/sofarpc-cli/internal/app"
	"github.com/diandian921/sofarpc-cli/internal/appconfig"
	"github.com/diandian921/sofarpc-cli/internal/mcp/server"
	"github.com/diandian921/sofarpc-cli/internal/schema"
)

// DoctorArgs are the arguments for sofarpc_doctor.
type DoctorArgs struct {
	Project string `json:"project,omitempty"`
	Server  string `json:"server,omitempty"`
	Service string `json:"service,omitempty"`
	Method  string `json:"method,omitempty"`
}

var doctorInputSchema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "project": {"type": "string", "description": "Optional project name."},
    "server": {"type": "string", "description": "Optional server name."},
    "service": {"type": "string", "description": "Optional service interface FQN."},
    "method": {"type": "string", "description": "Optional method filter."}
  }
}`)

// DoctorTool runs structured diagnostics for config, project source schema, and
// invocation prerequisites. writeEnabled reflects the server's config-write flag.
func DoctorTool(appSvc *app.Service, writeEnabled bool) server.Tool[DoctorArgs] {
	return server.Tool[DoctorArgs]{
		Spec: server.ToolSpec{
			Name:         "sofarpc_doctor",
			Title:        "SofaRPC Doctor",
			Description:  "Run structured diagnostics for config, project source schema, and invocation prerequisites.",
			Annotations:  server.Annotations{ReadOnlyHint: true, IdempotentHint: true},
			InputSchema:  doctorInputSchema,
			OutputSchema: resultOutputSchema,
			Async:        true,
		},
		Run: func(ctx context.Context, rt server.Runtime, a DoctorArgs) server.Result {
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
				return doctorResult(checks)
			}
			path, _ := appconfig.DefaultPath()
			addCheck("config", "ok", map[string]interface{}{"configPath": path, "projectCount": len(cfg.Projects), "serverCount": len(cfg.Servers), "writeEnabled": writeEnabled})

			rt.Progress(ctx, "checking project source schema", 0)
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
			return doctorResult(checks)
		},
	}
}

func doctorResult(checks []map[string]interface{}) server.Result {
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
	return rendered(app.Result{OK: ok, Code: code, Data: body}, text)
}
