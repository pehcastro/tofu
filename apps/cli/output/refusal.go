package output

import (
	"io"
	"os"
	"time"

	cli "tofu/apps/cli/page"
)

type Refusal struct {
	Problem cli.Problem
	Code    int
}

func UsageRefusal(what, hint string) *Refusal {
	return &Refusal{cli.Problem{What: what, Hint: hint}, ExitUsage}
}

func Failure(what, hint string) *Refusal {
	return &Refusal{cli.Problem{What: what, Hint: hint}, ExitVerdict}
}

func Refused(verb string, refusal *Refusal, asJSON bool, out, errOut io.Writer, now time.Time) int {
	if asJSON {
		_ = WriteJSON(out, cli.Envelope{Verb: verb, At: now, Problems: []cli.Problem{refusal.Problem}})
		return refusal.Code
	}
	page := cli.Detect(errOut, os.Environ())
	_ = page.Print(errOut, page.ErrorLine(refusal.Problem.What, refusal.Problem.Hint))
	return refusal.Code
}

func JSONExit(out io.Writer, envelope cli.Envelope) int {
	if WriteJSON(out, envelope) != nil {
		return ExitVerdict
	}
	return ExitOK
}
