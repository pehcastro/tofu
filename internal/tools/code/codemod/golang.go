package codemod

import (
	"bytes"
	"errors"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"

	"golang.org/x/tools/imports"

	"tofu/internal/konst"
)

const oneMark, manyMark = "tofu_codemod_one_", "tofu_codemod_many_"

var metavariableText = regexp.MustCompile(`\$\$\$([A-Z_][A-Z0-9_]*)?|\$([A-Z_][A-Z0-9_]*)`)

type matcher struct {
	source []byte
	base   int
	bound  map[string]string
}

func goSites(pattern, rewrite string, roots []string) ([]fileSites, []string, error) {
	shape, err := goPattern(pattern)
	if err != nil {
		return nil, nil, err
	}
	captured := metavariables(pattern)
	for name := range metavariables(rewrite) {
		switch {
		case name == "_" || name == "":
			return nil, nil, errors.New("rewrite uses $_ or $$$, which capture nothing: give the node a name in pattern and in rewrite")
		case !captured[name]:
			return nil, nil, fmt.Errorf("rewrite uses $%s, which pattern does not capture", name)
		}
	}
	files, err := goFiles(roots)
	if err != nil {
		return nil, nil, err
	}
	var found []fileSites
	var unparsed []string
	for _, path := range files {
		source, err := os.ReadFile(path)
		if err != nil {
			return nil, nil, err
		}
		fileSet := token.NewFileSet()
		file, err := parser.ParseFile(fileSet, path, source, parser.SkipObjectResolution)
		if err != nil {
			unparsed = append(unparsed, path)
			continue
		}
		var sites []site
		base := fileSet.File(file.Pos()).Base()
		ast.Inspect(file, func(node ast.Node) bool {
			if node == nil {
				return false
			}
			m := matcher{source: source, base: base, bound: map[string]string{}}
			if !m.match(reflect.ValueOf(shape), reflect.ValueOf(node)) {
				return true
			}
			start, end := int(node.Pos())-m.base, int(node.End())-m.base
			after := metavariableText.ReplaceAllStringFunc(rewrite, func(name string) string { return m.bound[strings.TrimLeft(name, "$")] })
			sites = append(sites, site{start: start, end: end, line: fileSet.Position(node.Pos()).Line, before: string(source[start:end]), after: after})
			return false
		})
		if len(sites) > 0 {
			found = append(found, fileSites{path: path, sites: sites})
		}
	}
	return found, unparsed, nil
}

func goPattern(pattern string) (ast.Node, error) {
	marked := metavariableText.ReplaceAllStringFunc(pattern, func(name string) string {
		if rest, many := strings.CutPrefix(name, "$$$"); many {
			return manyMark + rest
		}
		return oneMark + strings.TrimPrefix(name, "$")
	})
	shape, err := goShape(marked)
	if err != nil {
		return nil, fmt.Errorf("pattern is not one Go expression, statement or declaration: %w", err)
	}
	if _, _, alone := metavariableIn(reflect.ValueOf(shape)); alone {
		return nil, errors.New("pattern is only a metavariable and would match every node: name the code around it")
	}
	return shape, nil
}

func goShape(marked string) (ast.Node, error) {
	expr, err := parser.ParseExprFrom(token.NewFileSet(), "", marked, parser.SkipObjectResolution)
	if err == nil {
		return expr, nil
	}
	if file, statementErr := parser.ParseFile(token.NewFileSet(), "", "package p\nfunc _() {\n"+marked+"\n}", parser.SkipObjectResolution); statementErr == nil {
		if body := file.Decls[0].(*ast.FuncDecl).Body.List; len(body) == 1 {
			return body[0], nil
		}
	}
	if file, declarationErr := parser.ParseFile(token.NewFileSet(), "", "package p\n"+marked, parser.SkipObjectResolution); declarationErr == nil && len(file.Decls) == 1 {
		return file.Decls[0], nil
	}
	return nil, err
}

func metavariables(text string) map[string]bool {
	names := map[string]bool{}
	for _, found := range metavariableText.FindAllString(text, -1) {
		names[strings.TrimLeft(found, "$")] = true
	}
	return names
}

func goFiles(roots []string) ([]string, error) {
	var files []string
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			name := entry.Name()
			switch {
			case entry.IsDir() && path != root && (strings.HasPrefix(name, ".") || name == "vendor" || name == "testdata" || name == "node_modules"):
				return filepath.SkipDir
			case entry.IsDir() || filepath.Ext(name) != ".go":
				return nil
			case len(files) == konst.CodemodFilesScanned:
				return fmt.Errorf("more than %d Go files under paths: narrow paths", konst.CodemodFilesScanned)
			}
			files = append(files, path)
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return files, nil
}

func goFormatted(path string, before, after []byte) ([]byte, error) {
	if _, err := format.Source(after); err != nil {
		return nil, fmt.Errorf("the rewrite leaves %s as code that does not parse: %w", path, err)
	}
	if clean, err := format.Source(before); err != nil || !bytes.Equal(clean, before) {
		return after, nil
	}
	fixed, err := imports.Process(path, after, nil)
	if err != nil {
		return nil, fmt.Errorf("fixing the imports of %s after the rewrite: %w", path, err)
	}
	return fixed, nil
}

func metavariableIn(pattern reflect.Value) (name string, many, found bool) {
	if !pattern.IsValid() || !pattern.CanInterface() {
		return "", false, false
	}
	node := pattern.Interface()
	if statement, isStatement := node.(*ast.ExprStmt); isStatement && statement != nil {
		node = statement.X
	}
	ident, isIdent := node.(*ast.Ident)
	if !isIdent || ident == nil {
		return "", false, false
	}
	if name, cut := strings.CutPrefix(ident.Name, manyMark); cut {
		return name, true, true
	}
	name, cut := strings.CutPrefix(ident.Name, oneMark)
	return name, false, cut
}

func nodeIn(val reflect.Value) (ast.Node, bool) {
	if !val.IsValid() || (val.Kind() == reflect.Pointer || val.Kind() == reflect.Interface) && val.IsNil() {
		return nil, false
	}
	node, isNode := val.Interface().(ast.Node)
	return node, isNode
}

func (m *matcher) text(from, to token.Pos) string {
	return string(m.source[int(from)-m.base : int(to)-m.base])
}

func (m *matcher) bind(name, text string) bool {
	if name == "_" || name == "" {
		return true
	}
	if was, seen := m.bound[name]; seen {
		return was == text
	}
	m.bound[name] = text
	return true
}

func (m *matcher) match(pattern, val reflect.Value) bool {
	if name, many, found := metavariableIn(pattern); found && !many {
		node, isNode := nodeIn(val)
		return isNode && m.bind(name, m.text(node.Pos(), node.End()))
	}
	if !pattern.IsValid() || !val.IsValid() {
		return pattern.IsValid() == val.IsValid()
	}
	if pattern.Type() != val.Type() {
		return false
	}
	switch pattern.Type() {
	case reflect.TypeFor[token.Pos](), reflect.TypeFor[*ast.CommentGroup]():
		return true
	case reflect.TypeFor[*ast.CallExpr]():
		p, v := pattern.Interface().(*ast.CallExpr), val.Interface().(*ast.CallExpr)
		if p != nil && v != nil && p.Ellipsis.IsValid() != v.Ellipsis.IsValid() {
			return false
		}
	}
	p, v := reflect.Indirect(pattern), reflect.Indirect(val)
	if !p.IsValid() || !v.IsValid() {
		return p.IsValid() == v.IsValid()
	}
	switch p.Kind() {
	case reflect.Slice:
		return m.sequence(p, v, 0, 0)
	case reflect.Struct:
		for i := range p.NumField() {
			if !m.match(p.Field(i), v.Field(i)) {
				return false
			}
		}
		return true
	case reflect.Interface:
		return m.match(p.Elem(), v.Elem())
	}
	return p.Interface() == v.Interface()
}

func (m *matcher) sequence(p, v reflect.Value, i, j int) bool {
	if i == p.Len() {
		return j == v.Len()
	}
	name, many, _ := metavariableIn(p.Index(i))
	if !many {
		return j < v.Len() && m.match(p.Index(i), v.Index(j)) && m.sequence(p, v, i+1, j+1)
	}
	for end := j; end <= v.Len(); end++ {
		kept := maps.Clone(m.bound)
		run := ""
		if end > j {
			first, _ := nodeIn(v.Index(j))
			last, _ := nodeIn(v.Index(end - 1))
			run = m.text(first.Pos(), last.End())
		}
		if m.bind(name, run) && m.sequence(p, v, i+1, end) {
			return true
		}
		m.bound = kept
	}
	return false
}
