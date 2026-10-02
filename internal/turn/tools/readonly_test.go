package tools_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"slices"
	"strconv"
	"testing"

	"tofu/internal/turn/tools"
)

func caseNames(t *testing.T, file, function string) []string {
	t.Helper()
	parsed, err := parser.ParseFile(token.NewFileSet(), file, nil, 0)
	if err != nil {
		t.Fatalf("parsing %s: %v", file, err)
	}
	var names []string
	for _, declared := range parsed.Decls {
		found, ok := declared.(*ast.FuncDecl)
		if !ok || found.Name.Name != function {
			continue
		}
		ast.Inspect(found, func(node ast.Node) bool {
			clause, ok := node.(*ast.CaseClause)
			if !ok {
				return true
			}
			for _, value := range clause.List {
				literal, ok := value.(*ast.BasicLit)
				if !ok || literal.Kind != token.STRING {
					continue
				}
				name, err := strconv.Unquote(literal.Value)
				if err != nil {
					t.Fatalf("%s: %s is not a tool name: %v", file, literal.Value, err)
				}
				names = append(names, name)
			}
			return true
		})
	}
	if len(names) == 0 {
		t.Fatalf("%s names no tools in %s, so this test proves nothing", file, function)
	}
	slices.Sort(names)
	return names
}

func TestTheTwoListsOfReadOnlyToolsSayTheSameThing(t *testing.T) {
	batched := caseNames(t, filepath.Join("..", "tool.go"), "readOnly")
	cached := caseNames(t, "memo.go", "SideEffectFree")
	batchedAndRerunBecauseTheAnswerMovesWithoutAWrite := []string{"test", "typecheck", "subagents"}
	t.Logf("turn.readOnly batches %v", batched)
	t.Logf("tools.SideEffectFree caches %v", cached)

	for _, name := range batchedAndRerunBecauseTheAnswerMovesWithoutAWrite {
		if slices.Contains(cached, name) {
			t.Fatalf("%s is cached, so a typecheck that answered still warming, or a test rerun for a flake, returns its earlier answer", name)
		}
	}
	if accounted := slices.Sorted(slices.Values(slices.Concat(cached, batchedAndRerunBecauseTheAnswerMovesWithoutAWrite))); !slices.Equal(batched, accounted) {
		t.Fatalf("a tool in one list and not the other is batched and not cached, or cached and not batched, and nothing else catches it.\n"+
			"turn/tool.go readOnly: %v\ntools/memo.go SideEffectFree plus the rerun list: %v", batched, accounted)
	}
	for _, name := range cached {
		if !tools.SideEffectFree(name) {
			t.Fatalf("%s is in the switch but SideEffectFree refuses it, so this test is reading the wrong function", name)
		}
	}
	if tools.SideEffectFree("write") || tools.SideEffectFree("bash") {
		t.Fatal("a tool that changes the tree reads as side effect free")
	}
}
