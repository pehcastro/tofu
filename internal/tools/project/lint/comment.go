package lint

import (
	iofs "io/fs"
	"slices"
	"strings"

	"github.com/odvcencio/gotreesitter"

	"tofu/internal/platform/fs"
	"tofu/internal/tools/code/catgrep/lang"
)

type Comment struct {
	File   string
	Line   int
	Column int
	Text   string
}

func commentedLanguage(path string) (lang.Lang, bool) {
	l, ok := lang.FromPath(path)
	switch l {
	case lang.Markdown, lang.Yaml, lang.Json, lang.Toml:
		return 0, false
	}
	return l, ok
}

func directives(l lang.Lang) []string {
	switch l {
	case lang.Go:
		return []string{"//go:", "//nolint:"}
	case lang.TypeScript, lang.Tsx, lang.JavaScript:
		return []string{"// @ts-", "//@ts-", "/// <reference", "// eslint-", "/* eslint-", "// prettier-ignore", "// biome-ignore"}
	case lang.Python:
		return []string{"# noqa", "# type:", "# pyright:", "# mypy:", "# pylint:", "# fmt:", "# -*- coding", "# coding"}
	case lang.Bash:
		return []string{"# shellcheck "}
	case lang.PowerShell:
		return []string{"#Requires"}
	case lang.Ruby:
		return []string{"# frozen_string_literal:", "# encoding:", "# rubocop:"}
	case lang.C, lang.Cpp, lang.ObjC:
		return []string{"// NOLINT", "/* NOLINT"}
	case lang.Css, lang.Scss, lang.Less:
		return []string{"/* stylelint-"}
	case lang.Swift:
		return []string{"// swiftlint:"}
	case lang.Dockerfile:
		return []string{"# syntax=", "# escape="}
	}
	return nil
}

func allowed(l lang.Lang, c Comment) bool {
	if c.Line == 1 && strings.HasPrefix(c.Text, "#!") || l == lang.Go && c.Text == "//nolint" {
		return true
	}
	return slices.ContainsFunc(directives(l), func(d string) bool { return strings.HasPrefix(c.Text, d) })
}

func sourceCommentViolations(path string, src []byte) ([]Comment, error) {
	l, ok := commentedLanguage(path)
	if !ok {
		return nil, nil
	}
	tree, err := lang.Parse(src, l)
	if err != nil {
		return nil, err
	}
	defer tree.Release()
	grammar := tree.Language()
	var bad []Comment
	pending := []*gotreesitter.Node{tree.RootNode()}
	for len(pending) > 0 {
		node := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if kind := node.Type(grammar); kind == "comment" || strings.HasSuffix(kind, "_comment") {
			at := node.StartPoint()
			c := Comment{File: path, Line: int(at.Row) + 1, Column: int(at.Column) + 1, Text: strings.TrimRight(node.Text(src), "\r\n")}
			if !allowed(l, c) {
				bad = append(bad, c)
			}
			continue
		}
		for i := node.ChildCount() - 1; i >= 0; i-- {
			pending = append(pending, node.Child(i))
		}
	}
	return bad, nil
}

func FileCommentViolations(path string) ([]Comment, error) {
	if _, ok := commentedLanguage(path); !ok {
		return nil, nil
	}
	src, err := fs.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return sourceCommentViolations(path, src)
}

func TreeCommentViolations(root string) ([]Comment, int, error) {
	var all []Comment
	files := 0
	err := fs.WalkDir(root, func(path string, d iofs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != root && (d.Name() == "vendor" || d.Name() == "node_modules" || d.Name() == ".git") {
				return iofs.SkipDir
			}
			return nil
		}
		if _, ok := commentedLanguage(path); !ok {
			return nil
		}
		violations, ferr := FileCommentViolations(path)
		if ferr != nil {
			return ferr
		}
		all = append(all, violations...)
		files++
		return nil
	})
	if err != nil {
		return nil, 0, err
	}
	return all, files, nil
}
