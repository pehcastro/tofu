package main

import (
	"errors"
	"fmt"
	"io"
	"path/filepath"

	"tofu/interface/cli"
	"tofu/internal/sys"
)

const lintUsage = "tofu lint comments [path] [--json]"

func sourceRoots() []string { return []string{"bench", "cmd/tofu", "interface", "library", "internal"} }

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

func lintVerb(args []string, out, errOut io.Writer) int {
	o := verbOutput{verb: "lint", usageLine: lintUsage, asJSON: jsonAsked(args), out: out, errOut: errOut}
	if len(args) == 0 || args[0] != "comments" {
		return o.usage(errors.New("usage: comments is the one check"))
	}
	operands := withoutJSON(args[1:])
	if len(operands) > 1 {
		return o.usage(fmt.Errorf("unknown argument %q", operands[1]))
	}
	targets := sourceRoots()
	if len(operands) == 1 {
		targets = operands
	}
	report, err := lintComments(targets)
	if err != nil {
		return failed(o, err)
	}
	if len(report.Findings) == 0 {
		return o.done(true, report, func(page cli.Page) []string {
			return page.Title("Lint comments", []string{plural(report.Files, "file") + " read"}, cli.Verdict{Mark: cli.Done, Text: "no comments"})
		})
	}
	text := ""
	for _, f := range report.Findings {
		text += fmt.Sprintf("%s:%d:%d: %s\n", f.File, f.Line, f.Column, f.Text)
	}
	return protocol(o, exitVerdict, report, text)
}

func lintComments(targets []string) (lintReport, error) {
	report := lintReport{Findings: []lintFinding{}}
	for _, target := range targets {
		present, err := sys.Exists(target)
		if err != nil {
			return lintReport{}, err
		}
		if !present {
			continue
		}
		found, files, err := sys.TreeCommentViolations(target)
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
