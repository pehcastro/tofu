package lint

import (
	"errors"
	"fmt"
	"io"
	"path/filepath"

	"tofu/apps/cli/output"
	cli "tofu/apps/cli/page"
	"tofu/apps/cli/wiring"
	"tofu/internal/platform/fs"
	golint "tofu/internal/tools/project/lint"
)

const Usage = "tofu lint comments [path] [--json]"

type lintFinding struct {
	File   string `json:"file"`
	Line   int    `json:"line"`
	Column int    `json:"column"`
	Text   string `json:"text"`
}

type lintReport struct {
	Files    int           `json:"files"`
	Findings []lintFinding `json:"findings"`
}

func Verb(args []string, out, errOut io.Writer) int {
	o := output.Verb{Name: "lint", UsageLine: Usage, AsJSON: output.JSONAsked(args), Out: out, ErrOut: errOut}
	if len(args) == 0 || args[0] != "comments" {
		return o.Usage(errors.New("usage: comments is the one check"))
	}
	operands := output.WithoutJSON(args[1:])
	if len(operands) > 1 {
		return o.Usage(fmt.Errorf("unknown argument %q", operands[1]))
	}
	targets := fs.GoRoots()
	if len(operands) == 1 {
		targets = operands
	}
	report, err := lintComments(targets)
	if err != nil {
		return wiring.Failed(o, err)
	}
	if len(report.Findings) == 0 {
		return o.Done(true, report, func(page cli.Page) []string {
			return page.Title("Lint comments", []string{output.Plural(report.Files, "file") + " read"}, cli.Verdict{Mark: cli.Done, Text: "no comments"})
		})
	}
	text := ""
	for _, f := range report.Findings {
		text += fmt.Sprintf("%s:%d:%d: %s\n", f.File, f.Line, f.Column, f.Text)
	}
	return output.Protocol(o, output.ExitVerdict, report, text)
}

func lintComments(targets []string) (lintReport, error) {
	report := lintReport{Findings: []lintFinding{}}
	for _, target := range targets {
		present, err := fs.Exists(target)
		if err != nil {
			return lintReport{}, err
		}
		if !present {
			continue
		}
		found, files, err := golint.TreeCommentViolations(target)
		if err != nil {
			return lintReport{}, err
		}
		for _, c := range found {
			report.Findings = append(report.Findings, lintFinding{File: filepath.ToSlash(c.File), Line: c.Line, Column: c.Column, Text: c.Text})
		}
		report.Files += files
	}
	return report, nil
}
