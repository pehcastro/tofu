package rule

import (
	"fmt"
	"go/ast"
	"go/token"
)

var existentialAssertions = map[string]bool{
	"Nil": true, "NotNil": true, "Empty": true, "NotEmpty": true,
	"Zero": true, "NotZero": true, "IsType": true, "Implements": true,
	"Error": true, "NoError": true,
}

type claimKind int

const (
	notAClaim claimKind = iota
	existentialClaim
	substantiveClaim
)

func checkTestAssertion(_ Rule, a Artifact) ([]Finding, error) {
	sources, err := goPackageSources(a)
	if err != nil {
		return nil, err
	}
	var findings []Finding
	for _, file := range sources.files {
		for _, fn := range testFunctions(file.parse) {
			claims, substantive := countClaims(fn)
			if claims == 0 || substantive > 0 {
				continue
			}
			line := sources.fset.Position(fn.Pos()).Line
			findings = append(findings, Finding{
				Target: fmt.Sprintf("%s:%d", file.path, line),
				Detail: fmt.Sprintf("%s makes %d claims and every one of them only checks that a value is nil, zero, empty or of a type", fn.Name.Name, claims),
			})
		}
	}
	return findings, nil
}

func countClaims(fn *ast.FuncDecl) (claims, substantive int) {
	flags := commaOkFlags(fn.Body)
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		kind := claimOf(n, flags)
		if kind != notAClaim {
			claims++
		}
		if kind == substantiveClaim {
			substantive++
		}
		return true
	})
	return claims, substantive
}

func claimOf(n ast.Node, flags map[string]bool) claimKind {
	switch node := n.(type) {
	case *ast.IfStmt:
		if !failsTheTest(node.Body) {
			return notAClaim
		}
		return conditionClaim(node.Cond, flags)
	case *ast.CallExpr:
		return assertionLibraryClaim(node)
	}
	return notAClaim
}

func assertionLibraryClaim(call *ast.CallExpr) claimKind {
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return notAClaim
	}
	library, ok := selector.X.(*ast.Ident)
	if !ok || (library.Name != "assert" && library.Name != "require") {
		return notAClaim
	}
	if existentialAssertions[selector.Sel.Name] {
		return existentialClaim
	}
	return substantiveClaim
}

func failsTheTest(body *ast.BlockStmt) bool {
	failing := false
	ast.Inspect(body, func(n ast.Node) bool {
		if statement, ok := n.(*ast.ExprStmt); ok {
			switch calledMethod(statement.X) {
			case "Fatal", "Fatalf", "Error", "Errorf":
				failing = true
			}
		}
		return !failing
	})
	return failing
}

func commaOkFlags(body *ast.BlockStmt) map[string]bool {
	flags := map[string]bool{}
	ast.Inspect(body, func(n ast.Node) bool {
		assign, ok := n.(*ast.AssignStmt)
		if !ok || len(assign.Lhs) != 2 || len(assign.Rhs) != 1 {
			return true
		}
		switch assign.Rhs[0].(type) {
		case *ast.TypeAssertExpr, *ast.IndexExpr:
		default:
			return true
		}
		if name, ok := assign.Lhs[1].(*ast.Ident); ok {
			flags[name.Name] = true
		}
		return true
	})
	return flags
}

func conditionClaim(cond ast.Expr, flags map[string]bool) claimKind {
	switch expr := unparen(cond).(type) {
	case *ast.Ident:
		if flags[expr.Name] {
			return existentialClaim
		}
	case *ast.UnaryExpr:
		if expr.Op == token.NOT {
			return conditionClaim(expr.X, flags)
		}
	case *ast.BinaryExpr:
		switch expr.Op {
		case token.LAND, token.LOR:
			if conditionClaim(expr.X, flags) == substantiveClaim || conditionClaim(expr.Y, flags) == substantiveClaim {
				return substantiveClaim
			}
			return existentialClaim
		case token.EQL:
			if zeroValued(expr.X) || zeroValued(expr.Y) {
				return existentialClaim
			}
		case token.NEQ:
			if isNil(expr.X) || isNil(expr.Y) {
				return notAClaim
			}
		}
	}
	return substantiveClaim
}

func isNil(e ast.Expr) bool {
	name, ok := unparen(e).(*ast.Ident)
	return ok && name.Name == "nil"
}
