package tools

import (
	"context"
	"fmt"
	"io"
	"strings"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/diandian921/sofarpc-mcp/internal/app"
	"github.com/diandian921/sofarpc-mcp/internal/schema"
)

// AddDescribe registers sofarpc_describe on the SDK server. SDK-native replacement
// for DescribeTool; reports progress because the first call may build the source
// index over the whole workspace. Reads local config/source only, so it needs no
// app.Service. Handler body mirrors DescribeTool.Run.
func AddDescribe(srv *mcpsdk.Server, stderr io.Writer) {
	srv.AddTool(&mcpsdk.Tool{
		Name:         "sofarpc_describe",
		Title:        "SofaRPC Describe",
		Description:  "Search local Java source or describe methods and DTO fields for a service FQN.",
		Annotations:  &mcpsdk.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true, DestructiveHint: boolPtr(false), OpenWorldHint: boolPtr(false)},
		InputSchema:  describeInputSchema,
		OutputSchema: resultOutputSchema,
	}, adaptTool(stderr, func(ctx context.Context, req *mcpsdk.CallToolRequest, a DescribeArgs) (app.Result, string) {
		if a.Query == "" && a.Service == "" {
			return app.RenderFailure(app.CodeBadRequest, "query or service is required", nil), ""
		}
		cfg, err := loadConfig()
		if err != nil {
			return app.RenderFailure(app.CodeInternalError, err.Error(), nil), ""
		}
		projectName, project, err := resolveProject(cfg, a.Project, a.Server)
		if err != nil {
			return app.RenderFailure(app.CodeBadRequest, err.Error(), nil), ""
		}
		notifyProgress(ctx, req, "building source index", 0)
		idx, err := schema.LoadOrBuildIndex(schema.Project{
			Name:            projectName,
			WorkspaceRoot:   project.WorkspaceRoot,
			ServicePrefixes: project.ServicePrefixes,
		})
		if err != nil {
			return app.RenderFailure(app.CodeInternalError, err.Error(), nil), ""
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
				return app.RenderFailure(app.CodeBadRequest, err.Error(), nil), ""
			}
			data["description"] = publicDescription(desc)
			summary = append(summary, fmt.Sprintf("%d method(s) described", len(desc.Methods)))
		}
		return okResult(data), strings.Join(summary, "; ") + "."
	}))
}
