package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/diandian921/sofarpc-cli/internal/app"
	"github.com/diandian921/sofarpc-cli/internal/mcp/server"
	"github.com/diandian921/sofarpc-cli/internal/schema"
)

// DescribeArgs are the arguments for sofarpc_describe.
type DescribeArgs struct {
	Project            string `json:"project,omitempty"`
	Server             string `json:"server,omitempty"`
	Query              string `json:"query,omitempty"`
	Service            string `json:"service,omitempty"`
	Method             string `json:"method,omitempty"`
	Limit              int    `json:"limit,omitempty"`
	IncludeOutOfPrefix bool   `json:"includeOutOfPrefix,omitempty"`
}

var describeInputSchema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "project": {"type": "string", "description": "Optional project name. Required when multiple projects are configured and server is omitted."},
    "server": {"type": "string", "description": "Optional server name used to infer the bound project."},
    "query": {"type": "string", "description": "Natural language or identifier query for search mode."},
    "service": {"type": "string", "description": "Service interface FQN for describe mode."},
    "method": {"type": "string", "description": "Optional method filter for describe mode."},
    "limit": {"type": "integer", "description": "Max search candidates; default 5, max 20."},
    "includeOutOfPrefix": {"type": "boolean", "description": "Include services outside configured servicePrefixes."}
  }
}`)

// DescribeTool searches local Java source or describes methods and DTO fields.
// It runs async and reports progress because the first call may build the source
// index over the whole workspace.
func DescribeTool(appSvc *app.Service) server.Tool[DescribeArgs] {
	return server.Tool[DescribeArgs]{
		Spec: server.ToolSpec{
			Name:        "sofarpc_describe",
			Title:       "SofaRPC Describe",
			Description: "Search local Java source or describe methods and DTO fields for a service FQN.",
			Annotations: server.Annotations{ReadOnlyHint: true, IdempotentHint: true},
			InputSchema: describeInputSchema,
			Async:       true,
		},
		Run: func(ctx context.Context, rt server.Runtime, a DescribeArgs) server.Result {
			if a.Query == "" && a.Service == "" {
				return failure(app.CodeBadRequest, "query or service is required", nil)
			}
			cfg, err := loadConfig()
			if err != nil {
				return failure(app.CodeInternalError, err.Error(), nil)
			}
			projectName, project, err := resolveProject(cfg, a.Project, a.Server)
			if err != nil {
				return failure(app.CodeBadRequest, err.Error(), nil)
			}
			rt.Progress(ctx, "building source index", 0)
			idx, err := schema.LoadOrBuildIndex(schema.Project{
				Name:            projectName,
				WorkspaceRoot:   project.WorkspaceRoot,
				ServicePrefixes: project.ServicePrefixes,
			})
			if err != nil {
				return failure(app.CodeInternalError, err.Error(), nil)
			}
			data := map[string]interface{}{"project": projectName}
			var summary []string
			if a.Query != "" {
				limit := a.Limit
				if limit <= 0 {
					limit = 5
				}
				results := schema.Search(idx, a.Query, limit, a.IncludeOutOfPrefix)
				data["query"] = a.Query
				data["candidates"] = publicMethods(results)
				summary = append(summary, fmt.Sprintf("%d candidate(s) found", len(results)))
			}
			if a.Service != "" {
				desc, err := schema.Describe(idx, a.Service, a.Method)
				if err != nil {
					return failure(app.CodeBadRequest, err.Error(), nil)
				}
				data["description"] = publicDescription(desc)
				summary = append(summary, fmt.Sprintf("%d method(s) described", len(desc.Methods)))
			}
			return success(strings.Join(summary, "; ")+".", data)
		},
	}
}
