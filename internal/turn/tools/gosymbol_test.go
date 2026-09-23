package tools_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const twoNamesOneFile = `package a

func Resolve() string { return "package level" }

type Root string

func (r Root) Resolve() string { return "method" }
`

func TestAnEditBySymbolReplacesAWholeDeclarationInARealFileFromThisTree(t *testing.T) {
	body, err := os.ReadFile(filepath.Join(repositoryRoot(t), "internal", "search", "symbols.go"))
	if err != nil {
		t.Fatalf("reading a real file to copy: %v", err)
	}
	root := t.TempDir()
	seed(t, root, "symbols.go", string(body))

	result, err := editCall(t, root, `{"path":"symbols.go","symbol":"calledName","new_string":"func calledName(call *ast.CallExpr) string {\n\treturn \"\"\n}"}`)
	if err != nil {
		t.Fatalf("a symbol that resolves to one declaration must apply: %v", err)
	}
	after := held(t, root, "symbols.go")
	if strings.Contains(after, "case *ast.SelectorExpr:") {
		t.Fatalf("the old body of calledName survived the replacement:\n%s", after)
	}
	if !strings.Contains(after, "func calledNames(function *ast.FuncDecl) []string {") {
		t.Fatalf("the edit took a neighbouring declaration with it:\n%s", after)
	}
	if !strings.Contains(result.Content, "-	switch shape := call.Fun.(type) {") {
		t.Fatalf("the diff is not the declaration that was replaced:\n%s", result.Content)
	}
}

func TestAnEditBySymbolRefusesASymbolDeclaredTwiceNamingBoth(t *testing.T) {
	root := t.TempDir()
	seed(t, root, "a.go", "package a\n\nfunc twice() int { return 1 }\n\nfunc other() {}\n\nfunc twice() int { return 2 }\n")

	_, err := editCall(t, root, `{"path":"a.go","symbol":"twice","new_string":"func twice() int { return 3 }"}`)
	refusal(t, err, "declares twice 2 times", "lines 3 to 3", "lines 7 to 7", "nothing was changed")
	if got := held(t, root, "a.go"); strings.Contains(got, "return 3") {
		t.Fatalf("a refused edit changed the file:\n%s", got)
	}
}

func TestAnEditBySymbolRefusesASymbolThatFileDoesNotDeclareNamingTheFile(t *testing.T) {
	root := t.TempDir()
	seed(t, root, "a.go", twoNamesOneFile)

	_, err := editCall(t, root, `{"path":"a.go","symbol":"Absent","new_string":"func Absent() {}"}`)
	refusal(t, err, "a.go declares no Absent")
}

func TestAnEditBySymbolRefusesReplacementTextThatDoesNotParseAndWritesNothing(t *testing.T) {
	root := t.TempDir()
	seed(t, root, "a.go", twoNamesOneFile)

	_, err := editCall(t, root, `{"path":"a.go","symbol":"Resolve","new_string":"func Resolve() string { return \"unbalanced\" "}`)
	refusal(t, err, "unable to parse as go", "nothing was written")
	if got := held(t, root, "a.go"); got != twoNamesOneFile {
		t.Fatalf("a refused edit changed the file on disk:\n%s", got)
	}
}

func TestAnEditBySymbolRefusesAPathThatIsNotGo(t *testing.T) {
	root := t.TempDir()
	seed(t, root, "store.ts", "export const done = false\n")

	_, err := editCall(t, root, `{"path":"store.ts","symbol":"done","new_string":"export const done = true"}`)
	refusal(t, err, "store.ts is not a go file", "old_string")
	if got := held(t, root, "store.ts"); strings.Contains(got, "true") {
		t.Fatalf("a refused edit changed the file:\n%s", got)
	}
}

func TestAMethodAndAFunctionOfTheSameNameDoNotCollide(t *testing.T) {
	root := t.TempDir()
	seed(t, root, "a.go", twoNamesOneFile)

	if _, err := editCall(t, root, `{"path":"a.go","symbol":"Resolve","new_string":"func Resolve() string { return \"rewritten\" }"}`); err != nil {
		t.Fatalf("a package level function named like a method must resolve to one declaration: %v", err)
	}
	after := held(t, root, "a.go")
	if !strings.Contains(after, `func Resolve() string { return "rewritten" }`) || !strings.Contains(after, `func (r Root) Resolve() string { return "method" }`) {
		t.Fatalf("the package level function and the method did not stay apart:\n%s", after)
	}

	if _, err := editCall(t, root, `{"path":"a.go","symbol":"Root.Resolve","new_string":"func (r Root) Resolve() string { return \"also rewritten\" }"}`); err != nil {
		t.Fatalf("a method named by its receiver must resolve to one declaration: %v", err)
	}
	after = held(t, root, "a.go")
	if !strings.Contains(after, `func (r Root) Resolve() string { return "also rewritten" }`) || !strings.Contains(after, `func Resolve() string { return "rewritten" }`) {
		t.Fatalf("naming the method replaced the wrong declaration:\n%s", after)
	}
}

func TestAnEditByTextThatWouldBreakAGoFileIsRefused(t *testing.T) {
	root := t.TempDir()
	seed(t, root, "a.go", twoNamesOneFile)

	_, err := editCall(t, root, `{"path":"a.go","old_string":"func Resolve() string { return \"package level\" }","new_string":"func Resolve() string { return \"package level\" "}`)
	refusal(t, err, "unable to parse as go", "nothing was written")
	if got := held(t, root, "a.go"); got != twoNamesOneFile {
		t.Fatalf("a refused edit changed the file on disk:\n%s", got)
	}
}
