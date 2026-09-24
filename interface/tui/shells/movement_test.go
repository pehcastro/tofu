package shells_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const interfaceRoot = ".."

const fewestKeyMethods = 6

type keyMethod struct {
	where     string
	bound     map[string]bool
	typesText bool
}

func TestEveryViewMovesWithTheSameKeys(t *testing.T) {
	methods := keyMethods(t)
	if len(methods) < fewestKeyMethods {
		t.Fatalf("found %d Key methods under %s, the walk is broken", len(methods), interfaceRoot)
	}
	for _, method := range methods {
		if method.typesText {
			continue
		}
		if method.bound["down"] && !method.bound["j"] {
			t.Errorf("%s moves on down without j", method.where)
		}
		if method.bound["up"] && !method.bound["k"] {
			t.Errorf("%s moves on up without k", method.where)
		}
	}
}

func keyMethods(t *testing.T) []keyMethod {
	t.Helper()
	positions := token.NewFileSet()
	var found []keyMethod
	err := filepath.WalkDir(interfaceRoot, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		file, parseErr := parser.ParseFile(positions, path, nil, 0)
		if parseErr != nil {
			return parseErr
		}
		for _, decl := range file.Decls {
			function, isFunction := decl.(*ast.FuncDecl)
			if !isFunction || function.Recv == nil || function.Name.Name != "Key" {
				continue
			}
			method, switches := switchOnKey(function)
			if !switches {
				continue
			}
			method.where = filepath.ToSlash(path) + ":" + strconv.Itoa(positions.Position(function.Pos()).Line)
			found = append(found, method)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return found
}

func switchOnKey(function *ast.FuncDecl) (keyMethod, bool) {
	method := keyMethod{bound: map[string]bool{}}
	for _, statement := range function.Body.List {
		choice, isSwitch := statement.(*ast.SwitchStmt)
		if !isSwitch {
			continue
		}
		if tag, named := choice.Tag.(*ast.Ident); !named || tag.Name != "key" {
			continue
		}
		for _, clause := range choice.Body.List {
			arm := clause.(*ast.CaseClause)
			if arm.List == nil {
				method.typesText = true
			}
			for _, value := range arm.List {
				literal, isLiteral := value.(*ast.BasicLit)
				if !isLiteral || literal.Kind != token.STRING {
					continue
				}
				if key, err := strconv.Unquote(literal.Value); err == nil {
					method.bound[key] = true
				}
			}
		}
		return method, true
	}
	return keyMethod{}, false
}
