package api

import (
	"bytes"
	"cmp"
	"fmt"
	"go/ast"
	"go/build"
	"go/doc"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/mod/modfile"
	"golang.org/x/mod/module"
)

func lookupGo(from, pkg string, segments []string) (answer, error) {
	inst, err := goInstalled(from, pkg)
	if err != nil {
		return answer{}, err
	}
	built, err := build.Default.ImportDir(inst.dir, 0)
	if err != nil {
		return answer{}, fmt.Errorf("%s %s at %s: %w", pkg, inst.version, inst.dir, err)
	}
	fset := token.NewFileSet()
	var files []*ast.File
	for _, name := range append(built.GoFiles, built.CgoFiles...) {
		file, err := parser.ParseFile(fset, filepath.Join(inst.dir, name), nil, parser.ParseComments)
		if err != nil {
			return answer{}, err
		}
		files = append(files, file)
	}
	docs, err := doc.NewFromFiles(fset, files, pkg)
	if err != nil {
		return answer{}, err
	}
	found := answer{installed: inst}
	wanted := strings.Join(segments, ".")
	declare := func(name, text string, node ast.Node) {
		if !strings.Contains(name, ".") {
			found.names = append(found.names, name)
		}
		if name != wanted {
			return
		}
		if decl, ok := node.(*ast.FuncDecl); ok {
			header := *decl
			header.Body, header.Doc = nil, nil
			node = &header
		}
		var signature bytes.Buffer
		_ = printer.Fprint(&signature, fset, node)
		at := fset.Position(node.Pos())
		rel, _ := filepath.Rel(inst.dir, at.Filename)
		found.decls = append(found.decls, declaration{file: filepath.ToSlash(rel), line: at.Line, signature: signature.String(), doc: strings.TrimSpace(text)})
	}
	values := func(group []*doc.Value) {
		for _, value := range group {
			for _, name := range value.Names {
				declare(name, value.Doc, value.Decl)
			}
		}
	}
	values(docs.Consts)
	values(docs.Vars)
	for _, fn := range docs.Funcs {
		declare(fn.Name, fn.Doc, fn.Decl)
	}
	for _, t := range docs.Types {
		declare(t.Name, t.Doc, t.Decl)
		values(t.Consts)
		values(t.Vars)
		for _, fn := range t.Funcs {
			declare(fn.Name, fn.Doc, fn.Decl)
		}
		for _, method := range t.Methods {
			declare(t.Name+"."+method.Name, method.Doc, method.Decl)
		}
	}
	if len(segments) == 0 {
		found.doc = strings.TrimSpace(docs.Doc)
	}
	return found, nil
}

func goInstalled(from, pkg string) (installed, error) {
	root, ok := upward(from, "go.mod")
	if !ok {
		return installed{}, fmt.Errorf("no go.mod in %s or above it. go mod init makes one", from)
	}
	first, _, _ := strings.Cut(pkg, "/")
	if !strings.Contains(first, ".") {
		return installed{}, fmt.Errorf("%s is in the standard library, not a dependency: go doc %s reads it", pkg, pkg)
	}
	text, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return installed{}, err
	}
	mod, err := modfile.ParseLax(filepath.Join(root, "go.mod"), text, nil)
	if err != nil {
		return installed{}, err
	}
	var required module.Version
	for _, req := range mod.Require {
		if (pkg == req.Mod.Path || strings.HasPrefix(pkg, req.Mod.Path+"/")) && len(req.Mod.Path) > len(required.Path) {
			required = req.Mod
		}
	}
	if required.Path == "" {
		return installed{}, fmt.Errorf("%s is not required by %s. install it with: go get %s", pkg, filepath.Join(root, "go.mod"), pkg)
	}
	inside := strings.TrimPrefix(strings.TrimPrefix(pkg, required.Path), "/")
	for _, replace := range mod.Replace {
		if replace.Old.Path != required.Path || (replace.Old.Version != "" && replace.Old.Version != required.Version) {
			continue
		}
		if replace.New.Version == "" {
			local := filepath.FromSlash(replace.New.Path)
			if !filepath.IsAbs(local) {
				local = filepath.Join(root, local)
			}
			return installed{version: required.Version + " replaced by " + replace.New.Path, source: "replace", dir: filepath.Join(local, inside)}, nil
		}
		required = replace.New
	}
	escapedPath, err := module.EscapePath(required.Path)
	if err != nil {
		return installed{}, err
	}
	escapedVersion, err := module.EscapeVersion(required.Version)
	if err != nil {
		return installed{}, err
	}
	cache := cmp.Or(os.Getenv("GOMODCACHE"), filepath.Join(filepath.SplitList(build.Default.GOPATH)[0], "pkg", "mod"))
	dir := filepath.Join(cache, filepath.FromSlash(escapedPath)+"@"+escapedVersion, filepath.FromSlash(inside))
	if _, err := os.Stat(dir); err != nil {
		return installed{}, fmt.Errorf("%s %s is required but not in the module cache %s. install it with: go mod download %s", required.Path, required.Version, cache, required.Path)
	}
	return installed{version: required.Version, source: "module cache", dir: dir}, nil
}
