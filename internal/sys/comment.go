package sys

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
)

type Comment struct {
	File   string
	Line   int
	Column int
	Text   string
}

func CommentAllowed(text string) bool {
	return strings.HasPrefix(text, "//go:") || text == "//nolint" || strings.HasPrefix(text, "//nolint:")
}

func parseComments(filename string, src any) ([]Comment, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filename, src, parser.ParseComments)
	if err != nil {
		return nil, err
	}
	var found []Comment
	for _, group := range file.Comments {
		for _, c := range group.List {
			pos := fset.Position(c.Pos())
			found = append(found, Comment{File: pos.Filename, Line: pos.Line, Column: pos.Column, Text: c.Text})
		}
	}
	return found, nil
}

func violationsOf(comments []Comment) []Comment {
	var bad []Comment
	for _, c := range comments {
		if !CommentAllowed(c.Text) {
			bad = append(bad, c)
		}
	}
	return bad
}

func FileCommentViolations(path string) ([]Comment, error) {
	comments, err := parseComments(path, nil)
	if err != nil {
		return nil, err
	}
	return violationsOf(comments), nil
}

func TreeCommentViolations(root string) ([]Comment, error) {
	var all []Comment
	err := WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || filepath.Ext(path) != ".go" {
			return nil
		}
		violations, ferr := FileCommentViolations(path)
		if ferr != nil {
			return ferr
		}
		all = append(all, violations...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return all, nil
}
