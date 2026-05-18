package cli

import (
	"encoding/json"
	"io"

	"github.com/sofarpc/cli/internal/app"
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
