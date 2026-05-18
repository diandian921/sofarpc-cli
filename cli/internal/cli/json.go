package cli

import (
	"encoding/json"
	"fmt"
	"io"
)

func emitJSON(w io.Writer, v interface{}) {
	body, _ := json.Marshal(v)
	fmt.Fprintln(w, string(body))
}
