package symbols

import (
	"context"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/tools/go/packages"

	"tofu/internal/tools/code/lsp"
)

type goSite struct {
	pkg   *packages.Package
	at    token.Position
	ident *ast.Ident
	stack []ast.Node
}

func loadGo(ctx context.Context, root string, overlay map[string][]byte) ([]*packages.Package, error) {
	loaded, err := packages.Load(&packages.Config{
		Context: ctx, Dir: root, Tests: true, Overlay: overlay,
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles | packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo,
	}, "./...")
	if err != nil {
		return nil, fmt.Errorf("go/packages could not load the module at %s: %w", root, err)
	}
	return loaded, nil
}

func goKey(object types.Object) string {
	if object == nil || object.Pkg() == nil {
		return ""
	}
	if function, isFunc := object.(*types.Func); isFunc && function.Signature().Recv() != nil {
		receiver := function.Signature().Recv().Type()
		if pointer, isPointer := receiver.(*types.Pointer); isPointer {
			receiver = pointer.Elem()
		}
		if named, isNamed := types.Unalias(receiver).(*types.Named); isNamed {
			return object.Pkg().Path() + "." + named.Origin().Obj().Name() + "." + object.Name()
		}
		return ""
	}
	if object.Parent() != object.Pkg().Scope() {
		return ""
	}
	return object.Pkg().Path() + "." + object.Name()
}

func goObject(loaded []*packages.Package, target declared) types.Object {
	owner, method, isMethod := strings.Cut(target.symbol, ".")
	owner, _, _ = strings.Cut(owner, "[")
	for _, pkg := range loaded {
		if pkg.Types == nil || !slices.ContainsFunc(pkg.CompiledGoFiles, func(file string) bool { return lsp.PathKey(file) == lsp.PathKey(target.at.Path) }) {
			continue
		}
		object := pkg.Types.Scope().Lookup(owner)
		if object != nil && isMethod {
			object, _, _ = types.LookupFieldOrMethod(object.Type(), true, pkg.Types, method)
		}
		if object != nil {
			return object
		}
	}
	return nil
}

func goWalk(loaded []*packages.Package, key string, each func(goSite)) {
	seen := map[token.Position]bool{}
	for _, pkg := range loaded {
		for _, file := range pkg.Syntax {
			ast.PreorderStack(file, nil, func(node ast.Node, stack []ast.Node) bool {
				ident, isIdent := node.(*ast.Ident)
				if !isIdent {
					return true
				}
				object := pkg.TypesInfo.Defs[ident]
				if object == nil {
					object = pkg.TypesInfo.Uses[ident]
				}
				at := pkg.Fset.Position(ident.Pos())
				if goKey(object) != key || seen[at] {
					return true
				}
				seen[at] = true
				each(goSite{pkg: pkg, at: at, ident: ident, stack: stack})
				return true
			})
		}
	}
}

func goErrors(loaded []*packages.Package) map[string][]lsp.Problem {
	placed := regexp.MustCompile(`^(.*):(\d+)(?::\d+)?$`)
	found := map[string][]lsp.Problem{}
	packages.Visit(loaded, nil, func(pkg *packages.Package) {
		for _, failure := range pkg.Errors {
			path, line := "", 0
			if at := placed.FindStringSubmatch(failure.Pos); at != nil {
				path = at[1]
				line, _ = strconv.Atoi(at[2])
			}
			problem := lsp.Problem{Line: line, Message: failure.Msg}
			if !slices.Contains(found[path], problem) {
				found[path] = append(found[path], problem)
			}
		}
	})
	return found
}

func (s Symbols) loadTarget(ctx context.Context, target declared) (string, []*packages.Package, types.Object, error) {
	server, _ := lsp.ServerFor(target.at.Path)
	root := server.RootOf(string(s.root), target.at.Path)
	loaded, err := loadGo(ctx, root, nil)
	if err != nil {
		return "", nil, nil, err
	}
	object := goObject(loaded, target)
	if goKey(object) == "" {
		return "", nil, nil, fmt.Errorf("go/types does not resolve %s at %s:%d to a package level declaration or a method", target.symbol, s.shown(target.at.Path), target.at.Line)
	}
	return root, loaded, object, nil
}

func (s Symbols) renameGo(ctx context.Context, target declared, to string) (renamed, error) {
	root, loaded, object, err := s.loadTarget(ctx, target)
	if err != nil {
		return renamed{}, err
	}
	plan := renamed{by: "go/types", before: map[string]string{}, after: map[string]string{}}
	offsets := map[string][]int{}
	var sites []goSite
	goWalk(loaded, goKey(object), func(site goSite) {
		sites = append(sites, site)
		offsets[site.at.Filename] = append(offsets[site.at.Filename], site.at.Offset)
	})
	plan.clash = s.goClash(loaded[0].Fset, object, to, sites)
	overlay := map[string][]byte{}
	for path, at := range offsets {
		body, err := os.ReadFile(path)
		if err != nil {
			return renamed{}, err
		}
		text := string(body)
		slices.Sort(at)
		for _, offset := range slices.Backward(at) {
			text = text[:offset] + to + text[offset+len(object.Name()):]
		}
		plan.before[path], plan.after[path], overlay[path] = string(body), text, []byte(text)
		plan.sites += len(at)
	}
	plan.errors[0] = goErrors(loaded)
	if plan.clash != "" {
		return plan, nil
	}
	after, err := loadGo(ctx, root, overlay)
	if err != nil {
		return renamed{}, err
	}
	plan.errors[1] = goErrors(after)
	return plan, nil
}

func (s Symbols) goClash(fset *token.FileSet, object types.Object, to string, sites []goSite) string {
	place := func(other types.Object) string {
		at := fset.Position(other.Pos())
		return fmt.Sprintf("%s:%d", s.shown(at.Filename), at.Line)
	}
	var receiver *types.Var
	if function, isFunc := object.(*types.Func); isFunc {
		receiver = function.Signature().Recv()
	}
	if receiver != nil {
		if other, _, _ := types.LookupFieldOrMethod(receiver.Type(), true, object.Pkg(), to); other != nil {
			return fmt.Sprintf("%s already has %s, declared at %s", types.TypeString(receiver.Type(), types.RelativeTo(object.Pkg())), to, place(other))
		}
	}
	for _, site := range sites {
		if site.pkg.Types.Path() != object.Pkg().Path() {
			if token.IsExported(object.Name()) && !token.IsExported(to) {
				return fmt.Sprintf("%s would stop being exported as %s, and %s:%d uses it from package %s", object.Name(), to, s.shown(site.at.Filename), site.at.Line, site.pkg.Types.Path())
			}
			continue
		}
		if receiver != nil {
			continue
		}
		if other := site.pkg.Types.Scope().Lookup(to); other != nil {
			return fmt.Sprintf("package %s already declares %s at %s", object.Pkg().Name(), to, place(other))
		}
		inner := site.pkg.Types.Scope().Innermost(site.ident.Pos())
		if inner == nil {
			continue
		}
		if _, other := inner.LookupParent(to, site.ident.Pos()); other != nil && other.Parent() != types.Universe {
			return fmt.Sprintf("%s declared at %s would capture the use at %s:%d", to, place(other), s.shown(site.at.Filename), site.at.Line)
		}
	}
	return ""
}
