package search

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"strings"
)

type declaration struct {
	kind   Kind
	symbol string
	first  int
	last   int
}

func goUnits(rel, body string, lines []string, hits []hit) ([]Unit, bool) {
	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, rel, body, parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		return nil, false
	}
	parsed := fileSet.File(file.Pos())

	var units []Unit
	for _, where := range hits {
		if where.line > parsed.LineCount() {
			continue
		}
		at := parsed.LineStart(where.line) + token.Pos(where.column)
		placement := placementAt(file, at)
		decl, inside := declarationAt(file, parsed, at)
		if !inside {
			units = merge(units, frameUnit(rel, lines, where, placement, false), lines)
			continue
		}
		units = merge(units, Unit{
			Path:      rel,
			Kind:      decl.kind,
			Symbol:    decl.symbol,
			FirstLine: decl.first,
			LastLine:  decl.last,
			Matches:   []Match{{Line: where.line, Placement: placement}},
			Body:      strings.Join(lines[decl.first-1:decl.last], "\n"),
		}, lines)
	}
	return units, true
}

func placementAt(file *ast.File, at token.Pos) Placement {
	for _, group := range file.Comments {
		for _, comment := range group.List {
			if comment.Pos() <= at && at < comment.End() {
				return InComment
			}
		}
	}
	placement := InCode
	ast.Inspect(file, func(node ast.Node) bool {
		if node == nil || at < node.Pos() || at >= node.End() {
			return false
		}
		if literal, ok := node.(*ast.BasicLit); ok && (literal.Kind == token.STRING || literal.Kind == token.CHAR) {
			placement = InString
		}
		return true
	})
	return placement
}

func declarationAt(file *ast.File, parsed *token.File, at token.Pos) (declaration, bool) {
	for _, decl := range file.Decls {
		if at < decl.Pos() || at >= decl.End() {
			continue
		}
		switch shape := decl.(type) {
		case *ast.FuncDecl:
			kind, name := KindFunc, shape.Name.Name
			if shape.Recv != nil && len(shape.Recv.List) > 0 {
				kind = KindMethod
				name = strings.TrimPrefix(types.ExprString(shape.Recv.List[0].Type), "*") + "." + name
			}
			return span(parsed, kind, name, shape.Pos(), shape.End()), true
		case *ast.GenDecl:
			kind := valueKind(shape.Tok)
			for _, spec := range shape.Specs {
				if at >= spec.Pos() && at < spec.End() {
					return span(parsed, kind, specName(spec), spec.Pos(), spec.End()), true
				}
			}
			return span(parsed, kind, "", shape.Pos(), shape.End()), true
		}
	}
	return declaration{}, false
}

func span(parsed *token.File, kind Kind, symbol string, from, to token.Pos) declaration {
	return declaration{
		kind:   kind,
		symbol: symbol,
		first:  parsed.Position(from).Line,
		last:   parsed.Position(to - 1).Line,
	}
}

func valueKind(tok token.Token) Kind {
	switch tok {
	case token.IMPORT:
		return KindImport
	case token.TYPE:
		return KindType
	default:
		return KindValue
	}
}

func specName(spec ast.Spec) string {
	switch shape := spec.(type) {
	case *ast.TypeSpec:
		return shape.Name.Name
	case *ast.ValueSpec:
		if len(shape.Names) > 0 {
			return shape.Names[0].Name
		}
	case *ast.ImportSpec:
		return strings.Trim(shape.Path.Value, `"`)
	}
	return ""
}
