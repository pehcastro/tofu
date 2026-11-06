package engine

import (
	"cmp"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode"

	"tofu/internal/konst"
	"tofu/internal/platform/text"
)

func Closest(name string, declared []Definition) string {
	var names []string
	for _, definition := range declared {
		if definition.Kind != KindImport && !slices.Contains(names, definition.Symbol) {
			names = append(names, definition.Symbol)
		}
	}
	if len(names) == 0 {
		return "it declares no names at all"
	}
	asked := strings.ToLower(name)
	distance := func(symbol string) int {
		short := symbol[strings.LastIndex(symbol, ".")+1:]
		return min(text.EditDistance(asked, strings.ToLower(symbol)), text.EditDistance(asked, strings.ToLower(short)))
	}
	slices.SortStableFunc(names, func(a, b string) int { return cmp.Compare(distance(a), distance(b)) })
	return "the closest names it declares are " + strings.Join(names[:min(len(names), konst.ReadClosestSymbols)], ", ")
}

type Site struct {
	Path   string
	Line   int
	Within string
	Name   string
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
	Unreadable  int
	FirstFailed string
	Unread      []string
	Text        string
}

func Symbols(root string, files []string, name string) (Graph, error) {
	if !plainWord(name) || unicode.IsDigit(rune(name[0])) {
		return Graph{}, fmt.Errorf("search: %q is not an identifier, and the symbol graph is built over identifiers", name)
	}
	graph := Index(root, files, func(called string) bool { return called == name })
	graph.Name = name
	graph.Text = graph.render()
	return graph, nil
}

func Index(root string, files []string, wanted func(string) bool) Graph {
	return symbolGraph(files, wanted, func(rel string) ([]byte, error) {
		return os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	})
}

func symbolGraph(files []string, wanted func(string) bool, read func(rel string) ([]byte, error)) Graph {
	var graph Graph
	for _, rel := range files {
		extension := filepath.Ext(rel)
		if !slices.Contains([]string{".go", ".rs", ".ts", ".tsx", ".py"}, extension) {
			continue
		}
		body, err := read(rel)
		if err != nil {
			graph.Unread = append(graph.Unread, failedRead(rel, err))
			continue
		}
		var parsed bool
		switch extension {
		case ".go":
			parsed = graph.readGo(rel, body, wanted)
		case ".rs":
			parsed = graph.readOutline(rel, string(body), dialectRust, wanted)
		case ".py":
			parsed = graph.readOutline(rel, string(body), dialectPython, wanted)
		default:
			parsed = graph.readOutline(rel, string(body), dialectScript, wanted)
		}
		if !parsed {
			graph.Unparsed++
			continue
		}
		graph.Parsed++
	}
	if graph.Unreadable = len(graph.Unread); graph.Unreadable > 0 {
		graph.FirstFailed = graph.Unread[0]
	}
	slices.SortStableFunc(graph.Definitions, func(left, right Definition) int { return strings.Compare(left.Path, right.Path) })
	return graph
}

func (g *Graph) readGo(rel string, body []byte, wanted func(string) bool) bool {
	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, rel, body, parser.SkipObjectResolution)
	if err != nil {
		return false
	}
	parsed := fileSet.File(file.Pos())
	for _, decl := range file.Decls {
		g.readDecl(rel, parsed, decl, wanted)
	}
	return true
}

func (g *Graph) readDecl(rel string, parsed *token.File, decl ast.Decl, wanted func(string) bool) {
	within := ""
	switch shape := decl.(type) {
	case *ast.FuncDecl:
		kind := KindFunc
		within = shape.Name.Name
		if shape.Recv != nil && len(shape.Recv.List) > 0 {
			kind = KindMethod
			within = receiverName(shape.Recv.List[0]) + "." + within
		}
		if wanted(shape.Name.Name) {
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
			if name := specName(spec); wanted(name) {
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
		if call, isCall := node.(*ast.CallExpr); isCall && wanted(calledName(call)) {
			g.Callers = append(g.Callers, Site{Path: rel, Line: parsed.Position(call.Pos()).Line, Within: within, Name: calledName(call)})
		}
		return true
	})
}

type dialect uint8

const (
	dialectScript dialect = iota
	dialectRust
	dialectPython
)

func (g *Graph) readOutline(rel, body string, language dialect, wanted func(string) bool) bool {
	var found outline
	var parsed bool
	switch language {
	case dialectScript:
		found, parsed = braceOutline(body, scriptSyntax())
	case dialectRust:
		found, parsed = braceOutline(body, rustSyntax())
	case dialectPython:
		found, parsed = pythonOutline(body)
	}
	if !parsed {
		return false
	}
	slices.SortFunc(found.blocks, func(outer, inner block) int {
		return cmp.Or(cmp.Compare(outer.first, inner.first), cmp.Compare(inner.last, outer.last))
	})
	for _, decl := range found.blocks {
		if !wanted(decl.symbol) || language == dialectRust && decl.container && implHead(body, found, decl) {
			continue
		}
		kind, symbol := decl.kind, decl.symbol
		for _, outer := range found.blocks {
			if decl.kind == KindFunc && outer.container && outer.first <= decl.first && decl.last <= outer.last {
				kind, symbol = KindMethod, outer.symbol+"."+decl.symbol
			}
		}
		g.Definitions = append(g.Definitions, Definition{Path: rel, Kind: kind, Symbol: symbol, FirstLine: decl.first, LastLine: decl.last})
	}
	for at := 0; at < len(body); {
		end := at
		for found.regions[at] == regionCode && end < len(body) && identByte(body[end]) {
			end++
		}
		if end == at {
			at++
			continue
		}
		if word := body[at:end]; !unicode.IsDigit(rune(word[0])) && wanted(word) && calledAt(body, found, at, end, language) {
			line := found.lineOf(at)
			within := ""
			if decl, inside := found.around(line); inside {
				within = decl.symbol
			}
			g.Callers = append(g.Callers, Site{Path: rel, Line: line, Within: within, Name: word})
		}
		at = end
	}
	return true
}

func implHead(body string, found outline, decl block) bool {
	for line := decl.first; line <= decl.last; line++ {
		text := strings.TrimSpace(body[found.lineStarts[line-1]:lineEnd(body, found.lineStarts[line-1])])
		if !strings.HasPrefix(text, "#[") {
			head := strings.Fields(strings.TrimPrefix(text, "unsafe "))
			return len(head) > 0 && (head[0] == "impl" || strings.HasPrefix(head[0], "impl<"))
		}
	}
	return false
}

func calledAt(body string, found outline, at, end int, language dialect) bool {
	before := strings.TrimRight(body[:at], " \t\r\n*")
	word := len(before)
	for word > 0 && identByte(before[word-1]) {
		word--
	}
	if slices.Contains([]string{"fn", "function", "def", "class"}, before[word:]) {
		return false
	}
	next := skipSpace(body, end)
	switch {
	case language == dialectRust && strings.HasPrefix(body[next:], "::<"):
		next = skipSpace(body, angleEnd(body, next+2))
	case language == dialectScript && byteAt(body, next) == '<':
		if closed := angleEnd(body, next); closed > 0 {
			next = skipSpace(body, closed)
		}
	}
	if byteAt(body, next) != '(' {
		return false
	}
	return language != dialectScript || !memberHead(body, found, at, next)
}

func skipSpace(body string, at int) int {
	if at < 0 {
		return -1
	}
	return len(body) - len(strings.TrimLeft(body[at:], " \t\r\n"))
}

func angleEnd(body string, open int) int {
	depth := 0
	for at := open; at < len(body); at++ {
		switch body[at] {
		case '<':
			depth++
		case '>':
			if depth--; depth == 0 {
				return at + 1
			}
		case ';', '{', '}':
			return -1
		}
	}
	return -1
}

func memberHead(body string, found outline, at, open int) bool {
	modifiers := []string{"public", "private", "protected", "static", "readonly", "async", "override", "abstract", "declare", "get", "set", "accessor"}
	for _, word := range strings.Fields(body[found.lineStarts[found.lineOf(at)-1]:at]) {
		if !slices.Contains(modifiers, word) {
			return false
		}
	}
	depth := 0
	for scan := open; scan < len(body); scan++ {
		if found.regions[scan] != regionCode {
			continue
		}
		switch body[scan] {
		case '(':
			depth++
		case ')':
			if depth--; depth == 0 {
				after := byteAt(body, skipSpace(body, scan+1))
				return after == '{' || after == ':'
			}
		}
	}
	return false
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
	fmt.Fprintf(&out, "%s: %d definitions and %d call sites in %d parsed go, rust, typescript and python files\n", g.Name, len(g.Definitions), len(g.Callers), g.Parsed)
	if g.Unparsed > 0 {
		fmt.Fprintf(&out, "%s\n", Note(Fallback, fmt.Sprintf("%s did not parse and hold no definition and no call site here", plural(g.Unparsed, "file"))))
	}
	if g.Unreadable > 0 {
		fmt.Fprintf(&out, "%s\n", Note(Partial, fmt.Sprintf("%s could not be read, the first %s", plural(g.Unreadable, "file"), g.FirstFailed)))
	}
	out.WriteString(Note(NameMatched, "a definition and a call site are matched on the identifier alone") + "\n")
	if len(g.Definitions) == 0 && len(g.Callers) == 0 {
		parsed := "the files were parsed"
		if g.Unreadable > 0 {
			parsed = "every readable source file was parsed, and the unreadable ones may declare or call " + g.Name
		}
		fmt.Fprintf(&out, "no go, rust, typescript or python source under this path declares or calls %s. %s: this is an answer, not a failure\n", g.Name, parsed)
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
		fmt.Fprintf(&out, "  %s:%d in %s\n", site.Path, site.Line, cmp.Or(site.Within, "the file scope"))
	}
	return out.String()
}
