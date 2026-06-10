package mcp

import (
	"io"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/diandian921/sofarpc-mcp/internal/app"
	"github.com/diandian921/sofarpc-mcp/internal/mcp/tools"
)

// serverInstructions is the server-level guidance returned to the client at
// initialize.
const serverInstructions = "Run sofarpc_resolve before sofarpc_invoke. When multiple servers exist, always pass `server`. " +
	"Use sofarpc_describe with query=... to find a service FQN before invoking, and sofarpc_invoke_plan to validate arguments without sending a request. " +
	"On failure, read structuredContent.error.nextTool and error.recovery, then follow that tool. " +
	"Use sofarpc_doctor or sofarpc_probe to diagnose config/connectivity issues."

// newSDKServer builds the MCP server backed by the official modelcontextprotocol
// go-sdk: the migration target that replaces the self-written proto / server
// framework. writeEnabled mirrors the legacy DisableConfigWrite gating — the four
// config-write tools are omitted (so they vanish from tools/list) when it is false.
//
// Registration order matches the legacy toolRegistry so tools/list is unchanged.
// Run/SelfTest cut over to this server in a later step, after which the old layers
// are removed.
func newSDKServer(appSvc *app.Service, version string, writeEnabled bool, stderr io.Writer) *mcpsdk.Server {
	// Identity must match the legacy serverInfo so the initialize response does not
	// change: same Name/Title/Version. The 2025-11-25 spec adds an optional
	// Implementation.description, but go-sdk v1.6.1 does not expose it yet, so the old
	// server description lives on in the serverInstructions guidance instead.
	srv := mcpsdk.NewServer(
		&mcpsdk.Implementation{Name: "sofarpc-mcp", Title: "SofaRPC Direct Invoker", Version: version},
		&mcpsdk.ServerOptions{Instructions: serverInstructions},
	)
	tools.AddResolve(srv, appSvc, stderr)
	tools.AddProbe(srv, appSvc, stderr)
	tools.AddDescribe(srv, stderr)
	tools.AddDoctor(srv, writeEnabled, stderr)
	tools.AddConfigList(srv, writeEnabled, stderr)
	tools.AddInvokePlan(srv, appSvc, stderr)
	tools.AddInvoke(srv, appSvc, stderr)
	if writeEnabled {
		tools.AddConfigSaveProject(srv, stderr)
		tools.AddConfigSaveServer(srv, stderr)
		tools.AddConfigRemoveProject(srv, stderr)
		tools.AddConfigRemoveServer(srv, stderr)
	}
	tools.AddInvokeWorkflowPrompt(srv)
	tools.AddCompatibilityResource(srv)
	return srv
}
