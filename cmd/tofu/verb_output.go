package main

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"time"

	"tofu/interface/cli"
)

const (
	reportIndent = "  "
	jsonFlag     = "--json"
)

func relativeToRoot(root, text string) string {
	return strings.ReplaceAll(text, root+string(os.PathSeparator), "")
}

func writeJSON(out io.Writer, value any) error {
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(false)
	return encoder.Encode(value)
}

type problemError cli.Problem

func (e problemError) Error() string { return e.What }

type verbOutput struct {
	verb, usageLine string
	asJSON          bool
	out, errOut     io.Writer
}

func (o verbOutput) usage(err error) int {
	o.errorLine(cli.Problem{What: err.Error(), Hint: o.usageLine})
	return exitUsage
}

func (o verbOutput) fail(err error) int {
	problem := cli.Problem{What: err.Error()}
	var hinted problemError
	if errors.As(err, &hinted) {
		problem = cli.Problem(hinted)
	}
	if o.asJSON {
		_ = writeJSON(o.out, cli.Envelope{Verb: o.verb, At: time.Now(), Problems: []cli.Problem{problem}})
		return exitVerdict
	}
	o.errorLine(problem)
	return exitVerdict
}

func (o verbOutput) errorLine(problem cli.Problem) {
	page := cli.Detect(o.errOut, os.Environ())
	_ = page.Print(o.errOut, page.ErrorLine("tofu "+o.verb+": "+problem.What, problem.Hint))
}

func (o verbOutput) path(file string) string { return cli.Detect(o.errOut, os.Environ()).Path(file) }

func (o verbOutput) done(ok bool, data any, lines func(cli.Page) []string) int {
	code := exitOK
	if !ok {
		code = exitVerdict
	}
	if o.asJSON {
		if err := writeJSON(o.out, cli.Envelope{Verb: o.verb, OK: ok, At: time.Now(), Data: data}); err != nil {
			return exitVerdict
		}
		return code
	}
	page := cli.Detect(o.out, os.Environ())
	if err := page.Print(o.out, lines(page)); err != nil {
		return exitVerdict
	}
	return code
}

type changeKind string

const (
	changeAdded   changeKind = "added"
	changeChanged changeKind = "changed"
	changeRemoved changeKind = "removed"
)

func (c changeKind) mark() cli.Mark {
	switch c {
	case changeAdded:
		return cli.Added
	case changeChanged:
		return cli.Changed
	case changeRemoved:
		return cli.Removed
	}
	panic("tofu: unknown change " + string(c))
}

type fileChange struct {
	Change changeKind `json:"change"`
	What   string     `json:"what"`
	File   string     `json:"file"`
}

type writeReceipt struct {
	Changes []fileChange `json:"changes"`
	Undo    string       `json:"undo"`
}

func (o verbOutput) receipt(r writeReceipt) int {
	if o.asJSON {
		return o.done(true, r, nil)
	}
	page := cli.Detect(o.out, os.Environ())
	var lines []string
	for _, c := range r.Changes {
		lines = append(lines, page.Receipt(c.Change.mark(), c.What, c.File))
	}
	if err := page.Print(o.out, append(lines, cli.Indent(page.Hint("undo: "+r.Undo))...)); err != nil {
		return exitVerdict
	}
	return exitOK
}
