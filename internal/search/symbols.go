package search

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

type Site struct {
	Path   string
	Line   int
	Within string
}

type Definition struct {
	Path      string
	Kind      Kind
	Symbol    string
	FirstLine int
	LastLine  int
	Calls     []string
}

type Graph struct {
	Name        string
	Definitions []Definition
	Callers     []Site
	Parsed      int
	Unparsed    int
	Text        string
}

func Symbols(root string, files []string, name string) (Graph, error) {
	if !token.IsIdentifier(name) {
		return Graph{}, fmt.Errorf("search: %q is not a go identifier, and the symbol graph is built over identifiers", name)
	}
	graph := Graph{Name: name}
	for _, rel := range files {
		if !strings.HasSuffix(rel, ".go") {
			continue
		}
		body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			return Graph{}, fmt.Errorf("search: %w", err)
		}
		fileSet := token.NewFileSet()
		file, err := parser.ParseFile(fileSet, rel, body, parser.SkipObjectResolution)
		if err != nil {
			graph.Unparsed++
			continue
		}
		graph.Parsed++
		parsed := fileSet.File(file.Pos())
		for _, decl := range file.Decls {
			graph.read(rel, parsed, decl, name)
		}
	}
	slices.SortFunc(graph.Definitions, func(left, right Definition) int { return strings.Compare(left.Path, right.Path) })
	graph.Text = graph.render()
	return graph, nil
}

func (g *Graph) read(rel string, parsed *token.File, decl ast.Decl, name string) {
	within := ""
	switch shape := decl.(type) {
	case *ast.FuncDecl:
		kind := KindFunc
		within = shape.Name.Name
		if shape.Recv != nil && len(shape.Recv.List) > 0 {
			kind = KindMethod
			within = strings.TrimPrefix(types.ExprString(shape.Recv.List[0].Type), "*") + "." + within
		}
		if shape.Name.Name == name {
			g.Definitions = append(g.Definitions, Definition{
				Path:      rel,
				Kind:      kind,
				Symbol:    within,
				FirstLine: parsed.Position(shape.Pos()).Line,
				LastLine:  parsed.Position(shape.End() - 1).Line,
				Calls:     calledNames(shape),
			})
		}
	case *ast.GenDecl:
		for _, spec := range shape.Specs {
			if specName(spec) == name {
				g.Definitions = append(g.Definitions, Definition{
					Path:      rel,
					Kind:      valueKind(shape.Tok),
					Symbol:    name,
					FirstLine: parsed.Position(spec.Pos()).Line,
					LastLine:  parsed.Position(spec.End() - 1).Line,
				})
			}
		}
	}
	ast.Inspect(decl, func(node ast.Node) bool {
		if call, isCall := node.(*ast.CallExpr); isCall && calledName(call) == name {
			g.Callers = append(g.Callers, Site{Path: rel, Line: parsed.Position(call.Pos()).Line, Within: within})
		}
		return true
	})
}

func calledName(call *ast.CallExpr) string {
	switch shape := call.Fun.(type) {
	case *ast.Ident:
		return shape.Name
	case *ast.SelectorExpr:
		return shape.Sel.Name
	}
	return ""
}

func calledNames(function *ast.FuncDecl) []string {
	var names []string
	ast.Inspect(function, func(node ast.Node) bool {
		if call, isCall := node.(*ast.CallExpr); isCall {
			if name := calledName(call); name != "" && name != function.Name.Name {
				names = append(names, name)
			}
		}
		return true
	})
	slices.Sort(names)
	return slices.Compact(names)
}

func (g *Graph) render() string {
	var out strings.Builder
	fmt.Fprintf(&out, "%s: %d definitions and %d call sites in %d parsed go files\n", g.Name, len(g.Definitions), len(g.Callers), g.Parsed)
	if g.Unparsed > 0 {
		fmt.Fprintf(&out, "%s\n", Note(Fallback, fmt.Sprintf("%d go files did not parse and hold no definition and no call site here", g.Unparsed)))
	}
	out.WriteString(Note(NameMatched, "a definition and a call site are matched on the identifier alone") + "\n")
	if len(g.Definitions) == 0 && len(g.Callers) == 0 {
		fmt.Fprintf(&out, "no go file under this path declares or calls %s. the files were parsed: this is an answer, not a failure\n", g.Name)
		return out.String()
	}
	for _, definition := range g.Definitions {
		fmt.Fprintf(&out, "\ndefined %s:%d-%d %s %s\n", definition.Path, definition.FirstLine, definition.LastLine, definition.Kind, definition.Symbol)
		if len(definition.Calls) > 0 {
			fmt.Fprintf(&out, "  calls %s\n", strings.Join(definition.Calls, ", "))
		}
	}
	if len(g.Callers) == 0 {
		fmt.Fprintf(&out, "\nnothing under this path calls %s\n", g.Name)
		return out.String()
	}
	fmt.Fprintf(&out, "\ncalled from %s\n", plural(len(g.Callers), "place"))
	for _, site := range g.Callers {
		within := site.Within
		if within == "" {
			within = "the file scope"
		}
		fmt.Fprintf(&out, "  %s:%d in %s\n", site.Path, site.Line, within)
	}
	return out.String()
}
