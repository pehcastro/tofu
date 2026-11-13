package codemod

import (
	"bytes"
	"context"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"maps"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"golang.org/x/tools/go/ast/astutil"

	"tofu/internal/konst"
	"tofu/internal/platform/outside"
)

type resolver struct {
	root      string
	paths     map[string][]string
	stdLoaded bool
}

func fixImports(ctx context.Context, root string, changes []change) string {
	r := resolver{root: root, paths: map[string][]string{}}
	for _, file := range changes {
		if filepath.Ext(file.path) != ".go" {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), file.path, file.before, parser.ImportsOnly|parser.SkipObjectResolution)
		if err != nil {
			continue
		}
		for _, spec := range f.Imports {
			name, path := importOf(spec)
			if !slices.Contains(r.paths[name], path) {
				r.paths[name] = append(r.paths[name], path)
			}
		}
	}
	missing := map[string][]string{}
	for i, file := range changes {
		after, unresolved := r.fix(ctx, file)
		changes[i].after = after
		for _, name := range unresolved {
			missing[name] = append(missing[name], file.relative)
		}
	}
	if len(missing) == 0 {
		return ""
	}
	var notes strings.Builder
	for _, name := range slices.Sorted(maps.Keys(missing)) {
		found := "no import path"
		if len(r.paths[name]) > 1 {
			found = "more than one import path: " + strings.Join(r.paths[name], " ")
		}
		fmt.Fprintf(&notes, "imports: %s has %s, so add its import by hand in %s\n", name, found, strings.Join(missing[name], " "))
	}
	return clipped(notes.String())
}

func (r *resolver) fix(ctx context.Context, file change) ([]byte, []string) {
	if filepath.Ext(file.path) != ".go" {
		return file.after, nil
	}
	crlf, lf := []byte("\r\n"), []byte("\n")
	windows := bytes.Contains(file.before, crlf)
	before, after := bytes.ReplaceAll(file.before, crlf, lf), bytes.ReplaceAll(file.after, crlf, lf)
	fset := token.NewFileSet()
	was, err := parser.ParseFile(fset, file.path, before, parser.SkipObjectResolution)
	if err != nil {
		return file.after, nil
	}
	now, err := parser.ParseFile(fset, file.path, after, parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		return file.after, nil
	}
	had, has := qualifiers(was), qualifiers(now)
	if maps.Equal(had, has) {
		return file.after, nil
	}
	if clean, err := format.Source(before); err != nil || !bytes.Equal(clean, before) {
		return file.after, nil
	}
	imported := map[string]bool{}
	for _, spec := range slices.Clone(now.Imports) {
		name, path := importOf(spec)
		if !had[name] || has[name] {
			imported[name] = true
			continue
		}
		written := ""
		if spec.Name != nil {
			written = spec.Name.Name
		}
		astutil.DeleteNamedImport(fset, now, written, path)
	}
	var missing []string
	for _, name := range slices.Sorted(maps.Keys(has)) {
		if had[name] || imported[name] {
			continue
		}
		paths := r.resolve(ctx, name)
		switch {
		case len(paths) != 1:
			missing = append(missing, name)
		case assumedName(paths[0]) == name:
			astutil.AddImport(fset, now, paths[0])
		default:
			astutil.AddNamedImport(fset, now, name, paths[0])
		}
	}
	var out bytes.Buffer
	if err := format.Node(&out, fset, now); err != nil {
		return file.after, missing
	}
	if windows {
		return bytes.ReplaceAll(out.Bytes(), lf, crlf), missing
	}
	return out.Bytes(), missing
}

func (r *resolver) resolve(ctx context.Context, name string) []string {
	if _, known := r.paths[name]; known || r.stdLoaded {
		return r.paths[name]
	}
	r.stdLoaded = true
	list := outside.Command(ctx, "go", "list", "std")
	list.Dir, list.WaitDelay = r.root, konst.CodemodRunMillis*time.Millisecond
	out, _ := list.Output()
	std := map[string][]string{}
	for _, path := range strings.Fields(string(out)) {
		if !strings.Contains(path, "internal") && !strings.HasPrefix(path, "vendor/") {
			std[assumedName(path)] = append(std[assumedName(path)], path)
		}
	}
	for stdName, paths := range std {
		if _, known := r.paths[stdName]; !known {
			r.paths[stdName] = paths
		}
	}
	return r.paths[name]
}

func qualifiers(f *ast.File) map[string]bool {
	names := map[string]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		if selector, ok := n.(*ast.SelectorExpr); ok {
			if x, ok := selector.X.(*ast.Ident); ok {
				names[x.Name] = true
			}
		}
		return true
	})
	return names
}

func importOf(spec *ast.ImportSpec) (string, string) {
	path, _ := strconv.Unquote(spec.Path.Value)
	if spec.Name != nil {
		return spec.Name.Name, path
	}
	return assumedName(path), path
}

func assumedName(path string) string {
	parts := strings.Split(path, "/")
	last := parts[len(parts)-1]
	if len(parts) > 1 && len(last) > 1 && last[0] == 'v' && strings.Trim(last[1:], "0123456789") == "" {
		last = parts[len(parts)-2]
	}
	last = strings.TrimPrefix(last, "go-")
	if cut := strings.IndexFunc(last, func(r rune) bool { return r != '_' && !unicode.IsLetter(r) && !unicode.IsDigit(r) }); cut >= 0 {
		last = last[:cut]
	}
	return last
}
