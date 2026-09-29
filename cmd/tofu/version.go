package main

import (
	"io"
	"os"
	"strconv"
	"time"

	"tofu/interface/cli"
	"tofu/interface/tui/frame"
	"tofu/internal/sys"
)

type versionReport struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
	Go      string `json:"go"`
}

func version(args []string, out, errOut io.Writer) int {
	asJSON := false
	for _, arg := range args {
		if arg != jsonFlag {
			return printFailure(errOut, exitUsage, "tofu version: unknown argument "+strconv.Quote(arg), "tofu version [--json]")
		}
		asJSON = true
	}
	report := versionReport{Version: frame.Release(sys.Version(), sys.BuildRevision()), Commit: sys.BuildRevision(), Go: sys.GoVersion()}
	var err error
	if asJSON {
		err = writeJSON(out, cli.Envelope{Verb: "version", OK: true, At: time.Now(), Data: report})
	} else {
		page := cli.Detect(out, os.Environ())
		err = page.Print(out, page.Facts([]cli.Fact{{Label: "version", Text: report.Version}, {Label: "commit", Text: report.Commit}, {Label: "go", Text: report.Go}}))
	}
	if err != nil {
		return printFailure(errOut, exitVerdict, "tofu version: "+err.Error(), "")
	}
	return exitOK
}
