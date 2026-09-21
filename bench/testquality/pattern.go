package testquality

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
)

var comparisonOps = map[token.Token]bool{
	token.EQL: true, token.NEQ: true, token.LSS: true,
	token.GTR: true, token.LEQ: true, token.GEQ: true,
}

type PatternFinding struct {
	File     string
	Function string
	Line     int
}

func PatternFlagsTautologicalTests(dir string) ([]PatternFinding, error) {
	names, err := filepath.Glob(filepath.Join(dir, "*_test.go"))
	if err != nil {
		return nil, err
	}
	fset := token.NewFileSet()
	var findings []PatternFinding
	for _, path := range names {
		parsed, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return nil, err
		}
		for _, decl := range parsed.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || fn.Body == nil || !strings.HasPrefix(fn.Name.Name, "Test") {
				continue
			}
			if hasNoComparisonAndNoErrorCheck(fn.Body) {
				findings = append(findings, PatternFinding{
					File:     path,
					Function: fn.Name.Name,
					Line:     fset.Position(fn.Pos()).Line,
				})
			}
		}
	}
	return findings, nil
}

func hasNoComparisonAndNoErrorCheck(body *ast.BlockStmt) bool {
	sawComparison := false
	sawErrorCheck := false
	ast.Inspect(body, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.BinaryExpr:
			if comparisonOps[node.Op] {
				sawComparison = true
			}
		case *ast.Ident:
			if node.Name == "err" {
				sawErrorCheck = true
			}
		}
		return true
	})
	return !sawComparison && !sawErrorCheck
}
