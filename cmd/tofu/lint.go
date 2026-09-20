package main

import (
	"encoding/json"
	"fmt"
	"io"

	"tofu/internal/sys"
)

var lintRoots = []string{"bench", "cmd/tofu", "interface", "catalog", "internal"}

type lintFinding struct {
	File   string `json:"file"`
	Line   int    `json:"line"`
	Column int    `json:"column"`
	Text   string `json:"text"`
}

func lintVerb(args []string, out, errOut io.Writer) int {
	if len(args) == 0 || args[0] != "comments" {
		_, _ = fmt.Fprintln(errOut, "tofu lint: usage: tofu lint comments [path] [--json]")
		return exitUsage
	}
	path, asJSON, err := parseLintArgs(args[1:])
	if err != nil {
		return lintFail(errOut, err)
	}

	violations, err := commentViolations(path)
	if err != nil {
		return lintFail(errOut, err)
	}

	if asJSON {
		findings := make([]lintFinding, len(violations))
		for i, c := range violations {
			findings[i] = lintFinding{File: c.File, Line: c.Line, Column: c.Column, Text: c.Text}
		}
		body, err := json.Marshal(findings)
		if err != nil {
			return lintFail(errOut, err)
		}
		_, _ = fmt.Fprintln(out, string(body))
	} else {
		for _, c := range violations {
			_, _ = fmt.Fprintf(out, "%s:%d:%d: %s\n", c.File, c.Line, c.Column, c.Text)
		}
	}

	if len(violations) > 0 {
		return exitVerdict
	}
	return exitOK
}

func commentViolations(path string) ([]sys.Comment, error) {
	targets := lintRoots
	if path != "" {
		targets = []string{path}
	}
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
		case arg == "--json":
			asJSON = true
		case path == "":
			path = arg
		default:
			return "", false, fmt.Errorf("unknown argument %q", arg)
		}
	}
	return path, asJSON, nil
}

func lintFail(errOut io.Writer, err error) int {
	_, _ = fmt.Fprintf(errOut, "tofu lint: %v\n", err)
	return exitUsage
}
