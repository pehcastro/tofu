package rule

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"

	"tofu/internal/sys"
)

var callAssertions = map[string]bool{
	"AssertCalled": true, "AssertNotCalled": true, "AssertNumberOfCalls": true,
	"AssertExpectations": true, "EXPECT": true, "InOrder": true,
}

type testFile struct {
	path  string
	parse *ast.File
}

type testSources struct {
	fset  *token.FileSet
	files []testFile
	stubs map[string]bool
}

func goPackageSources(a Artifact) (testSources, error) {
	pkg, ok := a.(GoPackage)
	if !ok {
		return testSources{}, artifactMismatch(SubjectGoPackage, a)
	}
	return readTestSources(pkg.Dir)
}

func readTestSources(dir string) (testSources, error) {
	names, err := sys.ListFiles(dir, ".go")
	if err != nil {
		return testSources{}, err
	}
	sources := testSources{fset: token.NewFileSet(), stubs: map[string]bool{}}
	for _, name := range names {
		path := filepath.Join(dir, name)
		parsed, err := parser.ParseFile(sources.fset, path, nil, 0)
		if err != nil {
			return testSources{}, err
		}
		if strings.HasSuffix(name, "_test.go") {
			sources.files = append(sources.files, testFile{path: path, parse: parsed})
			continue
		}
		for _, held := range packageFunctionVariables(parsed) {
			sources.stubs[held] = true
		}
	}
	return sources, nil
}

func packageFunctionVariables(file *ast.File) []string {
	var names []string
	for _, decl := range file.Decls {
		declared, ok := decl.(*ast.GenDecl)
		if !ok || declared.Tok != token.VAR {
			continue
		}
		for _, spec := range declared.Specs {
			value, ok := spec.(*ast.ValueSpec)
			if !ok || !functionValued(value) {
				continue
			}
			for _, name := range value.Names {
				names = append(names, name.Name)
			}
		}
	}
	return names
}

func functionValued(value *ast.ValueSpec) bool {
	if _, ok := value.Type.(*ast.FuncType); ok {
		return true
	}
	for _, assigned := range value.Values {
		if _, ok := assigned.(*ast.FuncLit); ok {
			return true
		}
	}
	return false
}

func testFunctions(file *ast.File) []*ast.FuncDecl {
	var found []*ast.FuncDecl
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if ok && fn.Recv == nil && fn.Body != nil && strings.HasPrefix(fn.Name.Name, "Test") {
			found = append(found, fn)
		}
	}
	return found
}

func calledMethod(e ast.Expr) string {
	call, ok := e.(*ast.CallExpr)
	if !ok {
		return ""
	}
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	return selector.Sel.Name
}

func unparen(e ast.Expr) ast.Expr {
	for {
		inner, ok := e.(*ast.ParenExpr)
		if !ok {
			return e
		}
		e = inner.X
	}
}

func zeroValued(e ast.Expr) bool {
	switch value := unparen(e).(type) {
	case *ast.Ident:
		return value.Name == "nil"
	case *ast.BasicLit:
		return value.Value == "0" || value.Value == `""` || value.Value == "0.0"
	case *ast.CompositeLit:
		return len(value.Elts) == 0
	}
	return false
}

func checkTestMockBoundary(_ Rule, a Artifact) ([]Finding, error) {
	sources, err := goPackageSources(a)
	if err != nil {
		return nil, err
	}
	var findings []Finding
	for _, file := range sources.files {
		record := func(n ast.Node, detail string) {
			line := sources.fset.Position(n.Pos()).Line
			findings = append(findings, Finding{Target: fmt.Sprintf("%s:%d", file.path, line), Detail: detail})
		}
		ast.Inspect(file.parse, func(n ast.Node) bool {
			switch node := n.(type) {
			case *ast.AssignStmt:
				for _, target := range node.Lhs {
					name, ok := target.(*ast.Ident)
					if ok && sources.stubs[name.Name] {
						record(n, "the test replaces "+name.Name+", a function variable of the package under test, which is not a boundary")
					}
				}
			case *ast.CallExpr:
				if method := calledMethod(node); callAssertions[method] {
					record(n, method+" claims a call happened rather than that an output changed")
				}
			}
			return true
		})
	}
	return findings, nil
}

func checkTestBoundaryCases(_ Rule, a Artifact) ([]Finding, error) {
	sources, err := goPackageSources(a)
	if err != nil {
		return nil, err
	}
	var findings []Finding
	for _, file := range sources.files {
		tests := testFunctions(file.parse)
		if len(tests) == 0 || coversABoundary(tests) {
			continue
		}
		findings = append(findings, Finding{
			Target: file.path,
			Detail: fmt.Sprintf("%d tests and not one of them passes nil, zero, an empty value or a limit", len(tests)),
		})
	}
	return findings, nil
}

func coversABoundary(tests []*ast.FuncDecl) bool {
	found := false
	boundary := func(e ast.Expr) {
		if keyed, ok := e.(*ast.KeyValueExpr); ok {
			e = keyed.Value
		}
		if zeroValued(e) || namedLimit(e) {
			found = true
		}
	}
	for _, fn := range tests {
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			switch node := n.(type) {
			case *ast.CallExpr:
				for _, arg := range node.Args {
					boundary(arg)
				}
			case *ast.CompositeLit:
				for _, element := range node.Elts {
					boundary(element)
				}
			case *ast.AssignStmt:
				for _, assigned := range node.Rhs {
					boundary(assigned)
				}
			case *ast.ValueSpec:
				for _, assigned := range node.Values {
					boundary(assigned)
				}
			}
			return !found
		})
	}
	return found
}

func namedLimit(e ast.Expr) bool {
	selector, ok := e.(*ast.SelectorExpr)
	return ok && (strings.HasPrefix(selector.Sel.Name, "Max") || strings.HasPrefix(selector.Sel.Name, "Min"))
}
