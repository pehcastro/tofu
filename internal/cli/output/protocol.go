package output

import (
	"io"
	"time"

	"tofu/internal/cli/page"
)

func Protocol(o Verb, code int, data any, text string) int {
	var err error
	if o.AsJSON {
		err = WriteJSON(o.Out, cli.Envelope{Verb: o.Name, OK: code == ExitOK, At: time.Now(), Data: data})
	} else {
		_, err = io.WriteString(o.Out, text)
	}
	if err != nil {
		return ExitVerdict
	}
	return code
}
