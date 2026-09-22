package sys

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"maps"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const credentialMark = ".env"

var credentialRoots = []string{"bench", "library", "cmd", "interface", "internal"}

const credentialOpener = "sys.readCredentialFile"

var rawReaders = map[string]bool{"ReadFile": true, "Open": true, "OpenFile": true}

func TestOnlyOneFunctionOpensACredentialFile(t *testing.T) {
	root := SourceRoot()
	var found []string
	for _, name := range credentialRoots {
		dir := filepath.Join(root, name)
		if err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
			if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".go") {
				return err
			}
			rel, relErr := filepath.Rel(root, path)
			if relErr != nil {
				return relErr
			}
			readings, readErr := credentialReadsIn(path)
			if readErr != nil {
				return readErr
			}
			for _, line := range readings {
				found = append(found, rel+":"+line)
			}
			return nil
		}); err != nil {
			t.Fatalf("walking %s: %v", dir, err)
		}
	}
	if len(found) > 0 {
		t.Fatalf("%d credential reads happen outside %s, and every one of them is a way to the owner's key:\n%s",
			len(found), credentialOpener, strings.Join(found, "\n"))
	}
}

func credentialReadsIn(path string) ([]string, error) {
	fileSet := token.NewFileSet()
	parsed, err := parser.ParseFile(fileSet, path, nil, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	tainted := map[string]bool{}
	for _, decl := range parsed.Decls {
		general, ok := decl.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, spec := range general.Specs {
			taintSpec(spec, tainted)
		}
	}
	fileNamesACredential := len(tainted) > 0
	var lines []string
	for _, decl := range parsed.Decls {
		function, ok := decl.(*ast.FuncDecl)
		if !ok || function.Body == nil {
			continue
		}
		if parsed.Name.Name+"."+function.Name.Name == credentialOpener {
			continue
		}
		local := maps.Clone(tainted)
		if function.Type.Params != nil {
			for _, field := range function.Type.Params.List {
				for _, name := range field.Names {
					if strings.Contains(strings.ToLower(name.Name), "env") {
						local[name.Name] = true
					}
				}
			}
		}
		ast.Inspect(function.Body, func(node ast.Node) bool {
			switch typed := node.(type) {
			case *ast.AssignStmt:
				if mentionsCredential(local, typed.Rhs) {
					addIdents(typed.Lhs, local)
				}
			case *ast.RangeStmt:
				if mentionsCredential(local, []ast.Expr{typed.X}) {
					addIdents([]ast.Expr{typed.Key, typed.Value}, local)
				}
			case *ast.ValueSpec:
				taintSpec(typed, local)
			case *ast.CallExpr:
				if isRawReader(typed.Fun) && (fileNamesACredential || mentionsCredential(local, typed.Args)) {
					lines = append(lines, strconv.Itoa(fileSet.Position(typed.Pos()).Line))
				}
			}
			return true
		})
	}
	return lines, nil
}

func taintSpec(spec ast.Spec, tainted map[string]bool) {
	value, ok := spec.(*ast.ValueSpec)
	if !ok {
		return
	}
	if !mentionsCredential(tainted, value.Values) {
		return
	}
	for _, name := range value.Names {
		tainted[name.Name] = true
	}
}

func addIdents(exprs []ast.Expr, tainted map[string]bool) {
	for _, expr := range exprs {
		if name, ok := expr.(*ast.Ident); ok && name.Name != "_" {
			tainted[name.Name] = true
		}
	}
}

func mentionsCredential(tainted map[string]bool, exprs []ast.Expr) bool {
	seen := false
	for _, expr := range exprs {
		if expr == nil {
			continue
		}
		ast.Inspect(expr, func(node ast.Node) bool {
			switch typed := node.(type) {
			case *ast.BasicLit:
				if typed.Kind == token.STRING && strings.Contains(typed.Value, credentialMark) {
					seen = true
				}
			case *ast.Ident:
				if tainted[typed.Name] {
					seen = true
				}
			}
			return !seen
		})
	}
	return seen
}

func isRawReader(fun ast.Expr) bool {
	switch typed := fun.(type) {
	case *ast.Ident:
		return rawReaders[typed.Name]
	case *ast.SelectorExpr:
		pkg, ok := typed.X.(*ast.Ident)
		return ok && (pkg.Name == "os" || pkg.Name == "sys") && rawReaders[typed.Sel.Name]
	}
	return false
}
