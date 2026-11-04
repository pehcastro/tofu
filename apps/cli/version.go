package main

import (
	"errors"
	"io"
	"os"
	"strconv"
	"time"

	"tofu/apps/cli/output"
	cli "tofu/apps/cli/page"
	"tofu/internal/platform/build"
	"tofu/internal/ui/tui/frame/frame"
)

type versionReport struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
	Go      string `json:"go"`
}

func version(args []string, out, errOut io.Writer) int {
	asJSON := output.JSONAsked(args)
	if unknown := output.WithoutJSON(args); len(unknown) > 0 {
		return output.Verb{Name: "version", UsageLine: "tofu version [--json]", AsJSON: asJSON, Out: out, ErrOut: errOut}.Usage(errors.New("unknown argument " + strconv.Quote(unknown[0])))
	}
	report := versionReport{Version: frame.Release(build.Version(), build.BuildRevision()), Commit: build.BuildRevision(), Go: build.GoVersion()}
	var err error
	if asJSON {
		err = output.WriteJSON(out, cli.Envelope{Verb: "version", OK: true, At: time.Now(), Data: report})
	} else {
		page := cli.Detect(out, os.Environ())
		err = page.Print(out, page.Facts([]cli.Fact{{Label: "version", Text: report.Version}, {Label: "commit", Text: report.Commit}, {Label: "go", Text: report.Go}}))
	}
	if err != nil {
		return output.PrintFailure(errOut, output.ExitVerdict, "tofu version: "+err.Error(), "")
	}
	return output.ExitOK
}
