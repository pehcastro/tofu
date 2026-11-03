package main

import (
	"fmt"
	"io"
	"os"
	"time"

	"tofu/apps/cli/output"
	"tofu/apps/cli/page"
	"tofu/apps/cli/sample"
)

func previewVerb(args []string, out, errOut io.Writer) int {
	asJSON := false
	for _, arg := range args {
		if arg != output.JSONFlag {
			_, _ = fmt.Fprintf(errOut, "tofu preview: unknown argument %q\n", arg)
			return output.ExitUsage
		}
		asJSON = true
	}
	page := cli.Detect(out, os.Environ())
	if asJSON {
		if err := output.WriteJSON(out, sample.Envelopes(page.Home, time.Now())); err != nil {
			return output.ExitVerdict
		}
		return output.ExitOK
	}
	for i, lines := range sample.Pages(page, time.Now()) {
		if i > 0 {
			lines = append([]string{"", ""}, lines...)
		}
		if err := page.Print(out, lines); err != nil {
			return output.ExitVerdict
		}
	}
	return output.ExitOK
}
