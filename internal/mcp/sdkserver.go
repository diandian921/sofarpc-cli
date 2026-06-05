package mcp

import (
	"io"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/diandian921/sofarpc-mcp/internal/app"
	"github.com/diandian921/sofarpc-mcp/internal/mcp/tools"
)

// newSDKServer builds the MCP server backed by the official modelcontextprotocol
// go-sdk. This is the migration target that replaces the self-written proto / server
// framework. During migration only the piloted tools are registered here; the
// remaining tools move over in later steps, after which Run/SelfTest cut over to
// this server and the old layers are removed.
func newSDKServer(appSvc *app.Service, version string, stderr io.Writer) *mcpsdk.Server {
	srv := mcpsdk.NewServer(
		&mcpsdk.Implementation{Name: "sofarpc", Version: version},
		&mcpsdk.ServerOptions{Instructions: serverInstructions},
	)
	tools.AddProbe(srv, appSvc, stderr)
	return srv
}
