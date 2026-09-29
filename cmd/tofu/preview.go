package main

import (
	"fmt"
	"io"
	"os"
	"time"

	"tofu/interface/cli"
	"tofu/interface/cli/sample"
)

func previewVerb(args []string, out, errOut io.Writer) int {
	asJSON := false
	for _, arg := range args {
		if arg != jsonFlag {
			_, _ = fmt.Fprintf(errOut, "tofu preview: unknown argument %q\n", arg)
			return exitUsage
		}
		asJSON = true
	}
	page := cli.Detect(out, os.Environ())
	if asJSON {
		if err := writeJSON(out, sample.Envelopes(page.Home, time.Now())); err != nil {
			return exitVerdict
		}
		return exitOK
	}
	for i, lines := range sample.Pages(page, time.Now()) {
		if i > 0 {
			lines = append([]string{"", ""}, lines...)
		}
		if err := page.Print(out, lines); err != nil {
			return exitVerdict
		}
	}
	return exitOK
}
