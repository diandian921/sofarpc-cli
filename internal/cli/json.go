package cli

import (
	"encoding/json"
	"fmt"
	"io"
)

func emitJSON(stdout, stderr io.Writer, v interface{}) {
	body, err := json.Marshal(v)
	if err != nil {
		fmt.Fprintln(stderr, "emit json:", err)
		return
	}
	fmt.Fprintln(stdout, string(body))
}
