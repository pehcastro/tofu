package output

import (
	"errors"
	"io"
	"os"
	"time"

	cli "tofu/apps/cli/page"
	"tofu/internal/host"
)

const (
	ExitOK      = 0
	ExitVerdict = 1
	ExitUsage   = 2
)

type ProblemError cli.Problem

func (e ProblemError) Error() string { return e.What }

type Verb struct {
	Name, UsageLine string
	AsJSON          bool
	Out, ErrOut     io.Writer
}

func (o Verb) Usage(err error) int {
	return o.Refuse(ExitUsage, cli.Problem{What: err.Error(), Hint: o.UsageLine})
}

func (o Verb) Fail(err error) int {
	problem := cli.Problem{What: err.Error()}
	var hinted ProblemError
	if errors.As(err, &hinted) {
		problem = cli.Problem(hinted)
	}
	return o.Refuse(ExitVerdict, problem)
}

func (o Verb) Refuse(code int, problem cli.Problem) int {
	if o.AsJSON {
		_ = WriteJSON(o.Out, cli.Envelope{Verb: o.Name, At: time.Now(), Problems: []cli.Problem{problem}})
		return code
	}
	o.ErrorLine(problem)
	return code
}

func (o Verb) ErrorLine(problem cli.Problem) {
	page := cli.Detect(o.ErrOut, os.Environ())
	_ = page.Print(o.ErrOut, page.ErrorLine("tofu "+o.Name+": "+problem.What, problem.Hint))
}

func (o Verb) Path(file string) string { return cli.Detect(o.ErrOut, os.Environ()).Path(file) }

func (o Verb) Done(ok bool, data any, lines func(cli.Page) []string) int {
	code := ExitOK
	if !ok {
		code = ExitVerdict
	}
	if o.AsJSON {
		if err := WriteJSON(o.Out, cli.Envelope{Verb: o.Name, OK: ok, At: time.Now(), Data: data}); err != nil {
			return ExitVerdict
		}
		return code
	}
	page := cli.Detect(o.Out, os.Environ())
	if err := page.Print(o.Out, lines(page)); err != nil {
		return ExitVerdict
	}
	return code
}

func changeMark(c host.ChangeKind) cli.Mark {
	switch c {
	case host.ChangeAdded:
		return cli.Added
	case host.ChangeChanged:
		return cli.Changed
	case host.ChangeRemoved:
		return cli.Removed
	}
	panic("tofu: unknown change " + string(c))
}

func (o Verb) Receipt(r host.WriteReceipt) int {
	if o.AsJSON {
		return o.Done(true, r, nil)
	}
	page := cli.Detect(o.Out, os.Environ())
	var lines []string
	for _, c := range r.Changes {
		lines = append(lines, page.Receipt(changeMark(c.Change), c.What, c.File))
	}
	if err := page.Print(o.Out, append(lines, cli.Indent(page.Hint("undo: "+r.Undo))...)); err != nil {
		return ExitVerdict
	}
	return ExitOK
}

func Show(out io.Writer, asJSON bool, envelope cli.Envelope, lines func(cli.Page) []string) int {
	var err error
	if asJSON {
		err = WriteJSON(out, envelope)
	} else {
		page := cli.Detect(out, os.Environ())
		err = page.Print(out, lines(page))
	}
	if err != nil || !envelope.OK {
		return ExitVerdict
	}
	return ExitOK
}
