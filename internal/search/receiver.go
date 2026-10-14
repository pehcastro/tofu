package search

import (
	"go/ast"
	"strings"
)

func receiverName(field *ast.Field) string {
	return strings.TrimPrefix(receiverType(field.Type), "*")
}

func receiverType(expr ast.Expr) string {
	switch shape := expr.(type) {
	case *ast.Ident:
		return shape.Name
	case *ast.StarExpr:
		return "*" + receiverType(shape.X)
	case *ast.ParenExpr:
		return "(" + receiverType(shape.X) + ")"
	case *ast.SelectorExpr:
		return receiverType(shape.X) + "." + shape.Sel.Name
	case *ast.IndexExpr:
		return receiverType(shape.X) + "[" + receiverType(shape.Index) + "]"
	case *ast.IndexListExpr:
		names := make([]string, len(shape.Indices))
		for i, index := range shape.Indices {
			names[i] = receiverType(index)
		}
		return receiverType(shape.X) + "[" + strings.Join(names, ", ") + "]"
	}
	return "_"
}
