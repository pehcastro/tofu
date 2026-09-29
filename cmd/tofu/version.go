package main

import (
	"errors"
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
	asJSON := jsonAsked(args)
	if unknown := withoutJSON(args); len(unknown) > 0 {
		return verbOutput{verb: "version", usageLine: "tofu version [--json]", asJSON: asJSON, out: out, errOut: errOut}.usage(errors.New("unknown argument " + strconv.Quote(unknown[0])))
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
