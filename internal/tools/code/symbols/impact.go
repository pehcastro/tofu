package symbols

import (
	"cmp"
	"context"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"golang.org/x/tools/go/packages"

	"tofu/internal/tools/code/lsp"
	"tofu/internal/tools/contract"
)

type caller struct {
	kind   string
	path   string
	line   int
	within string
}

type impacted struct {
	by       string
	callers  []caller
	tests    []string
	direct   bool
	packages []string
	affected []string
	note     string
}

func (s Symbols) impactGo(ctx context.Context, target declared) (contract.Result, error) {
	_, loaded, object, err := s.loadTarget(ctx, target)
	if err != nil {
		return contract.Result{}, fmt.Errorf("symbols: %w", err)
	}
	key := goKey(object)
	found := impacted{by: "go/types"}
	goWalk(loaded, key, func(site goSite) {
		if site.pkg.TypesInfo.Defs[site.ident] != nil {
			return
		}
		within := ""
		for _, outer := range site.stack {
			switch shape := outer.(type) {
			case *ast.FuncDecl:
				within = goFuncName(shape)
			case *ast.TypeSpec:
				if shape.Name.Name != object.Name() || site.pkg.Types.Path() != object.Pkg().Path() {
					found.affected = append(found.affected, site.pkg.Types.Path()+"."+shape.Name.Name)
				}
			}
		}
		found.callers = append(found.callers, caller{kind: goKind(site), path: s.shown(site.at.Filename), line: site.at.Line, within: within})
		found.packages = append(found.packages, site.pkg.PkgPath)
	})
	if name, isType := object.(*types.TypeName); isType && types.IsInterface(name.Type()) {
		s.implementers(loaded, name, &found)
	}
	found.tests = s.goTests(loaded, key)
	return s.impactResult(target, found), nil
}

func goFuncName(function *ast.FuncDecl) string {
	if function.Recv == nil || len(function.Recv.List) == 0 {
		return function.Name.Name
	}
	receiver := function.Recv.List[0].Type
	for {
		switch shape := receiver.(type) {
		case *ast.StarExpr:
			receiver = shape.X
		case *ast.IndexExpr:
			receiver = shape.X
		case *ast.IndexListExpr:
			receiver = shape.X
		case *ast.Ident:
			return shape.Name + "." + function.Name.Name
		default:
			return function.Name.Name
		}
	}
}

func goKind(site goSite) string {
	if strings.HasSuffix(site.at.Filename, "_test.go") {
		return "test"
	}
	var node ast.Node = site.ident
	parents := slices.Clone(site.stack)
	for len(parents) > 0 {
		parent := parents[len(parents)-1]
		switch shape := parent.(type) {
		case *ast.SelectorExpr:
			if shape.Sel != node {
				return "ref"
			}
		case *ast.StarExpr, *ast.IndexExpr, *ast.IndexListExpr:
		case *ast.Field:
			if len(shape.Names) == 0 {
				return "embed"
			}
			return "ref"
		case *ast.CallExpr:
			if shape.Fun == node {
				return "call"
			}
			return "ref"
		default:
			return "ref"
		}
		node, parents = parent, parents[:len(parents)-1]
	}
	return "ref"
}

func (s Symbols) implementers(loaded []*packages.Package, iface *types.TypeName, found *impacted) {
	want := iface.Type().Underlying().(*types.Interface)
	seen := map[token.Position]bool{}
	for _, pkg := range loaded {
		for _, name := range pkg.Types.Scope().Names() {
			concrete, isType := pkg.Types.Scope().Lookup(name).(*types.TypeName)
			if !isType || types.IsInterface(concrete.Type()) || !types.Implements(concrete.Type(), want) && !types.Implements(types.NewPointer(concrete.Type()), want) {
				continue
			}
			if at := pkg.Fset.Position(concrete.Pos()); !seen[at] {
				seen[at] = true
				found.callers = append(found.callers, caller{kind: "impl", path: s.shown(at.Filename), line: at.Line, within: "type " + name})
				found.affected = append(found.affected, pkg.Types.Path()+"."+name)
			}
		}
	}
}

func (s Symbols) goTests(loaded []*packages.Package, key string) []string {
	type function struct {
		at   string
		name string
		test bool
		uses []string
	}
	graph := map[string]function{}
	for _, pkg := range loaded {
		for _, file := range pkg.Syntax {
			for _, decl := range file.Decls {
				declared, isFunc := decl.(*ast.FuncDecl)
				if !isFunc || declared.Body == nil {
					continue
				}
				name := goKey(pkg.TypesInfo.Defs[declared.Name])
				at := pkg.Fset.Position(declared.Pos())
				node := function{at: fmt.Sprintf("%s:%d", s.shown(at.Filename), at.Line), name: goFuncName(declared),
					test: strings.HasSuffix(at.Filename, "_test.go") && declared.Recv == nil && regexp.MustCompile(`^(Test|Benchmark|Fuzz|Example)`).MatchString(declared.Name.Name)}
				ast.Inspect(declared.Body, func(inner ast.Node) bool {
					if ident, isIdent := inner.(*ast.Ident); isIdent {
						used := pkg.TypesInfo.Uses[ident]
						node.uses = append(node.uses, goKey(used))
						if method, isFunc := used.(*types.Func); isFunc && method.Signature().Recv() != nil {
							node.uses = append(node.uses, strings.TrimSuffix(goKey(used), "."+used.Name()))
						}
					}
					return true
				})
				graph[name] = node
			}
		}
	}
	reached := map[string]bool{key: true}
	for grown := true; grown; {
		grown = false
		for name, node := range graph {
			if !reached[name] && slices.ContainsFunc(node.uses, func(used string) bool { return reached[used] }) {
				reached[name], grown = true, true
			}
		}
	}
	var tests []string
	for name, node := range graph {
		if reached[name] && node.test {
			tests = append(tests, node.at+" "+node.name)
		}
	}
	return tests
}

func (s Symbols) impactServed(ctx context.Context, target declared, files []string) (contract.Result, error) {
	server, _ := lsp.ServerFor(target.at.Path)
	asked, err := s.ask(ctx, server, []declared{target}, files)
	if err != nil {
		return contract.Result{}, fmt.Errorf("symbols: %s: %w", server.Program, err)
	}
	used := asked.definitions[0].used
	found := impacted{by: server.Program, direct: true, note: strings.Join(asked.statuses, "; ")}
	add := func(at lsp.Location, within string, opens int, kind string) {
		text := lsp.Lines(at.Path)
		line := ""
		if at.Line <= len(text) {
			line = strings.TrimSpace(text[at.Line-1])
		}
		switch {
		case servedTest(text, at, opens):
			kind = "test"
			if within != "" {
				found.tests = append(found.tests, s.shown(at.Path)+" "+within)
			}
		case regexp.MustCompile(`^(pub(\([^)]*\))?\s+)?(unsafe\s+)?impl\b|\b(implements|extends)\b|^class\s+\w+\s*\(`).MatchString(line):
			kind = "impl"
		}
		found.callers = append(found.callers, caller{kind: kind, path: s.shown(at.Path), line: at.Line, within: within})
		found.packages = append(found.packages, path.Dir(s.shown(at.Path)))
	}
	for _, call := range used.Calls {
		add(call.Location, call.Within, call.Opens, "call")
	}
	for _, other := range used.Others {
		add(other, "", 0, "ref")
	}
	return s.impactResult(target, found), nil
}

func servedTest(text []string, at lsp.Location, opens int) bool {
	base := filepath.Base(at.Path)
	if regexp.MustCompile(`(\.|_)(test|spec)\.[jt]sx?$|^test_.*\.py$|_test\.py$`).MatchString(base) || slices.Contains(strings.Split(filepath.ToSlash(at.Path), "/"), "tests") {
		return true
	}
	if filepath.Ext(at.Path) != ".rs" {
		return false
	}
	attribute := regexp.MustCompile(`^\s*#\[(\w+::)?test\]`)
	return slices.ContainsFunc(text[:min(at.Line, len(text))], func(line string) bool { return strings.Contains(line, "#[cfg(test)]") }) ||
		slices.ContainsFunc(text[max(0, opens-2):min(max(0, opens), len(text))], attribute.MatchString)
}

func (s Symbols) impactResult(target declared, found impacted) contract.Result {
	slices.SortFunc(found.callers, func(a, b caller) int {
		return cmp.Or(strings.Compare(a.kind, b.kind), strings.Compare(a.path, b.path), cmp.Compare(a.line, b.line))
	})
	for _, list := range []*[]string{&found.tests, &found.packages, &found.affected} {
		slices.Sort(*list)
		*list = slices.Compact(*list)
	}
	kinds := map[string]int{}
	for _, one := range found.callers {
		kinds[one.kind]++
	}
	var counts []string
	for _, kind := range []string{"call", "impl", "embed", "test", "ref"} {
		if kinds[kind] > 0 {
			counts = append(counts, fmt.Sprintf("%d %s", kinds[kind], kind))
		}
	}
	var out strings.Builder
	fmt.Fprintf(&out, "impact of %s, resolved by %s: %s (%s), %s reaching it, %s\n", target.symbol, found.by,
		counted(len(found.callers), "caller"), cmp.Or(strings.Join(counts, ", "), "none"), counted(len(found.tests), "test"), counted(len(found.packages), "package"))
	if found.note != "" {
		fmt.Fprintf(&out, "%s says: %s\n", found.by, found.note)
	}
	fmt.Fprintf(&out, "defined %s:%d %s\n", s.shown(target.at.Path), target.at.Line, target.label())
	if len(found.callers) > 0 {
		out.WriteString("\ncallers\n")
	}
	for _, one := range found.callers {
		fmt.Fprintf(&out, "  %-5s %s:%d in %s\n", one.kind, one.path, one.line, cmp.Or(one.within, "the file scope"))
	}
	sections := []struct {
		head string
		list []string
	}{{"tests that reach it", found.tests}, {"packages", found.packages}, {"types affected", found.affected}}
	if found.direct {
		sections[0].head = "tests that call it directly, since a language server answers one level of callers"
	}
	for _, section := range sections {
		if len(section.list) > 0 {
			fmt.Fprintf(&out, "\n%s\n  %s\n", section.head, strings.Join(section.list, "\n  "))
		}
	}
	return contract.Result{Content: out.String(), Command: "impact " + target.symbol}
}
