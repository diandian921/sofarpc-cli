package cli

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/diandian921/sofarpc-mcp/internal/app"
)

// writeResult emits one rendered result as a single JSON line.
func writeResult(w io.Writer, result app.Result) error {
	body, err := json.Marshal(result)
	if err != nil {
		return err
	}
	body = append(body, '\n')
	_, err = w.Write(body)
	return err
}

// emitResult renders an operation outcome in the unified envelope on stdout and
// maps it to the process exit code. Usage errors (bad flags/arity) stay on
// stderr with exit 2; this is for operation results only.
func emitResult(env Env, prefix string, result app.Result) int {
	result.RequestID = app.NewRequestID(prefix)
	if err := writeResult(env.Stdout, result); err != nil {
		fmt.Fprintln(env.Stderr, prefix+": write result:", err)
		return 1
	}
	if !result.OK {
		return 1
	}
	return 0
}
