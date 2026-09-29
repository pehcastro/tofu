package main

import (
	"errors"
	"fmt"
	"io"
	"path/filepath"

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

func lintVerb(args []string, out, errOut io.Writer) int {
	o := verbOutput{verb: "lint", usageLine: lintUsage, out: out, errOut: errOut}
	if len(args) == 0 || args[0] != "comments" {
		return o.usage(errors.New("usage: comments is the one check"))
	}
	path, asJSON, err := parseLintArgs(args[1:])
	if err != nil {
		return o.usage(err)
	}
	o.asJSON = asJSON
	targets := sourceRoots()
	if path != "" {
		targets = []string{path}
	}
	violations, err := commentViolations(targets)
	if err != nil {
		return failed(o, err)
	}
	text := ""
	findings := make([]lintFinding, len(violations))
	for i, c := range violations {
		findings[i] = lintFinding{File: filepath.ToSlash(c.File), Line: c.Line, Column: c.Column, Text: c.Text}
		text += fmt.Sprintf("%s:%d:%d: %s\n", findings[i].File, c.Line, c.Column, c.Text)
	}
	code := exitOK
	if len(violations) > 0 {
		code = exitVerdict
	}
	return protocol(o, code, struct {
		Findings []lintFinding `json:"findings"`
	}{findings}, text)
}

func commentViolations(targets []string) ([]sys.Comment, error) {
	var all []sys.Comment
	for _, target := range targets {
		present, err := sys.Exists(target)
		if err != nil {
			return nil, err
		}
		if !present {
			continue
		}
		found, err := sys.TreeCommentViolations(target)
		if err != nil {
			return nil, err
		}
		all = append(all, found...)
	}
	return all, nil
}

func parseLintArgs(args []string) (string, bool, error) {
	path := ""
	asJSON := false
	for _, arg := range args {
		switch {
		case arg == jsonFlag:
			asJSON = true
		case path == "":
			path = arg
		default:
			return "", false, fmt.Errorf("unknown argument %q", arg)
		}
	}
	return path, asJSON, nil
}
