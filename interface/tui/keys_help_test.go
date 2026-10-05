package tui

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"tofu/internal/keymap"
)

func handledKeys(t *testing.T, file string) []string {
	parsed, err := parser.ParseFile(token.NewFileSet(), file, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	constants := map[string]string{}
	var found []string
	literal := func(expr ast.Expr) {
		switch value := expr.(type) {
		case *ast.BasicLit:
			if text, err := strconv.Unquote(value.Value); err == nil && value.Kind == token.STRING {
				found = append(found, text)
			}
		case *ast.Ident:
			if text, named := constants[value.Name]; named {
				found = append(found, text)
			}
		}
	}
	readsKey := func(expr ast.Expr) bool {
		if name, isName := expr.(*ast.Ident); isName {
			return name.Name == "key"
		}
		call, isCall := expr.(*ast.CallExpr)
		if !isCall {
			return false
		}
		selector, isSelector := call.Fun.(*ast.SelectorExpr)
		return isSelector && selector.Sel.Name == "String"
	}
	ast.Inspect(parsed, func(node ast.Node) bool {
		if spec, isSpec := node.(*ast.ValueSpec); isSpec {
			for at, name := range spec.Names {
				if at < len(spec.Values) {
					if lit, isLit := spec.Values[at].(*ast.BasicLit); isLit {
						constants[name.Name], _ = strconv.Unquote(lit.Value)
					}
				}
			}
		}
		switch node := node.(type) {
		case *ast.SwitchStmt:
			if node.Tag == nil || !readsKey(node.Tag) {
				return true
			}
			for _, clause := range node.Body.List {
				for _, expr := range clause.(*ast.CaseClause).List {
					literal(expr)
				}
			}
		case *ast.BinaryExpr:
			if node.Op == token.EQL && readsKey(node.X) {
				literal(node.Y)
			}
		case *ast.CallExpr:
			if len(node.Args) == 2 && readsKey(node.Args[1]) {
				if list, isList := node.Args[0].(*ast.CompositeLit); isList {
					for _, element := range list.Elts {
						literal(element)
					}
				}
			}
		}
		return true
	})
	return found
}

func TestEveryKeyTheHandlingReadsIsInTheKeysList(t *testing.T) {
	listed := map[string]bool{}
	for _, result := range newTestApp(Options{}).keyResults("") {
		for _, key := range strings.Split(strings.Fields(result.Label)[1], "/") {
			listed[key] = true
		}
	}
	for _, file := range []string{"keys.go", "command.go"} {
		handled := handledKeys(t, file)
		if len(handled) == 0 {
			t.Fatalf("%s: the scan found no handled key, so it proves nothing", file)
		}
		for _, key := range handled {
			if !listed[key] {
				t.Errorf("%s handles %q and /keys does not list it", file, key)
			}
		}
	}
}

func TestKeysShowsTheBindingInForceAndSaysUnbound(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keybindings.json")
	bindings := keymap.DefaultShortcuts()
	bindings[keymap.EditorAction] = "ctrl+e"
	if err := keymap.SaveShortcuts(path, bindings); err != nil {
		t.Fatal(err)
	}
	app := newTestApp(Options{})
	app.options.Keymap = path
	app.runCommand("reload")
	var labels []string
	for _, result := range app.keyResults("") {
		labels = append(labels, strings.Join(strings.Fields(result.Label), " "))
	}
	for _, want := range []string{"anywhere ctrl+e Edit in editor", "anywhere unbound Settings"} {
		if !slices.Contains(labels, want) {
			t.Errorf("no row %q in\n%s", want, strings.Join(labels, "\n"))
		}
	}
	if slices.Contains(labels, "anywhere ctrl+g Edit in editor") {
		t.Errorf("the default binding shows beside the rebound one")
	}
}

func TestSlashKeysOpensTheListAndEscClosesIt(t *testing.T) {
	app := newTestApp(Options{})
	app.Update(tea.WindowSizeMsg{Width: 60, Height: 30})
	app.runCommand("keys")
	if _, open := app.top().(*recordedDialog); !open {
		t.Fatalf("/keys opened %T", app.top())
	}
	app.busy = true
	app.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if app.top() != nil || !app.busy {
		t.Fatalf("esc left %T open, busy %v", app.top(), app.busy)
	}
}
