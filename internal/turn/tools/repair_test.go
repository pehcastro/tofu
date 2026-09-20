package tools_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"tofu/internal/turn"
	"tofu/internal/turn/tools"
)

func editCall(t *testing.T, root, args string) (turn.Result, error) {
	t.Helper()
	tool, err := tools.NewEdit(root)
	if err != nil {
		t.Fatalf("building the tool: %v", err)
	}
	return tool.Run(context.Background(), json.RawMessage(args))
}

func held(t *testing.T, root, rel string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("reading %s: %v", rel, err)
	}
	return string(body)
}

func refusal(t *testing.T, err error, facts ...string) {
	t.Helper()
	if err == nil {
		t.Fatal("the call could not have succeeded and was not refused")
	}
	for _, fact := range facts {
		if !strings.Contains(err.Error(), fact) {
			t.Fatalf("the refusal does not name %q, it says: %v", fact, err)
		}
	}
	t.Logf("refusal: %v", err)
}

func TestAnEditOnAPathThatDoesNotExistRepairsOnlyWhenTheCandidateIsUnique(t *testing.T) {
	root := t.TempDir()
	seed(t, root, "internal/store.ts", "export const done = false\n")

	result, err := editCall(t, root, `{"path":"src/store.ts","old_string":"done","new_string":"completed"}`)
	if err != nil {
		t.Fatalf("one candidate must repair rather than refuse: %v", err)
	}
	if !strings.Contains(result.Content, "repaired") || !strings.Contains(result.Content, "internal/store.ts") {
		t.Fatalf("the result does not report what was repaired:\n%s", result.Content)
	}
	if got := held(t, root, "internal/store.ts"); !strings.Contains(got, "completed") {
		t.Fatalf("the repaired edit did not apply: %s", got)
	}

	seed(t, root, "other/store.ts", "export const done = false\n")
	seed(t, root, "third/store.ts", "export const done = false\n")
	_, err = editCall(t, root, `{"path":"src/store.ts","old_string":"done","new_string":"completed"}`)
	refusal(t, err, "other/store.ts", "third/store.ts", "only candidate")
	if got := held(t, root, "other/store.ts"); strings.Contains(got, "completed") {
		t.Fatalf("a refused edit changed a file: %s", got)
	}
}

func TestAnEditOnAPathWithNoCandidateAtAllNamesTheFact(t *testing.T) {
	root := t.TempDir()
	seed(t, root, "internal/store.ts", "export const done = false\n")

	_, err := editCall(t, root, `{"path":"src/absent.ts","old_string":"done","new_string":"completed"}`)
	refusal(t, err, "src/absent.ts is not a file under the working directory", "nothing there is named absent.ts")
}

func TestAnEditWhoseTargetAppearsMoreThanOnceIsRefusedWithEveryOccurrenceListed(t *testing.T) {
	root := t.TempDir()
	seed(t, root, "a.txt", "alpha\n  same\nbeta\nsame\ngamma\nsame\n")

	_, err := editCall(t, root, `{"path":"a.txt","old_string":"same","new_string":"other"}`)
	refusal(t, err, "appears 3 times", "line 2: same", "line 4: same", "line 6: same")
	if got := held(t, root, "a.txt"); strings.Contains(got, "other") {
		t.Fatalf("a refused edit changed the file: %s", got)
	}
}

func TestAnEditThatDiffersOnlyInWhitespaceRepairsWhenTheRunIsUnique(t *testing.T) {
	root := t.TempDir()
	seed(t, root, "a.go", "package a\n\nfunc One() int {\n\treturn 1\n}\n")

	result, err := editCall(t, root, `{"path":"a.go","old_string":"func One() int {\n    return 1","new_string":"func One() int {\n\treturn 2"}`)
	if err != nil {
		t.Fatalf("a unique run differing in whitespace alone must repair: %v", err)
	}
	if !strings.Contains(result.Content, "repaired") || !strings.Contains(result.Content, "lines 3 to 4") {
		t.Fatalf("the result does not report what was repaired:\n%s", result.Content)
	}
	if got := held(t, root, "a.go"); !strings.Contains(got, "return 2") || strings.Contains(got, "return 1") {
		t.Fatalf("the repaired edit did not apply:\n%s", got)
	}
}

func TestAnEditThatDiffersOnlyInWhitespaceInTwoPlacesIsRefusedWithBothNamed(t *testing.T) {
	root := t.TempDir()
	seed(t, root, "a.go", "package a\n\nfunc One() int {\n\treturn 1;\n}\n\nfunc Two() int {\n        return 1;\n}\n")

	_, err := editCall(t, root, `{"path":"a.go","old_string":"\t\treturn 1;","new_string":"\treturn 2;"}`)
	refusal(t, err, "2 runs of lines", "line 4: return 1;", "line 8: return 1;", "only candidate")
	if got := held(t, root, "a.go"); strings.Contains(got, "return 2") {
		t.Fatalf("a refused edit changed the file:\n%s", got)
	}
}

func TestAnEditWhoseTargetIsNowhereNearTheFileNamesTheFact(t *testing.T) {
	root := t.TempDir()
	seed(t, root, "a.txt", "alpha\nbeta\n")

	_, err := editCall(t, root, `{"path":"a.txt","old_string":"gamma","new_string":"delta"}`)
	refusal(t, err, "no text in a.txt matches old_string", "no run of 1 lines", "nothing was changed")
}

func TestAGlobUnderAPathThatDoesNotExistRepairsOnceAndRefusesTwice(t *testing.T) {
	root := t.TempDir()
	seed(t, root, "internal/pkg/a.go", "package pkg\n")
	globTool, err := tools.NewGlob(root)
	if err != nil {
		t.Fatalf("building the tool: %v", err)
	}

	result, err := globTool.Run(context.Background(), json.RawMessage(`{"pattern":"*.go","path":"pkg"}`))
	if err != nil {
		t.Fatalf("one candidate directory must repair rather than refuse: %v", err)
	}
	if !strings.Contains(result.Content, "repaired") || !strings.Contains(result.Content, "internal/pkg/a.go") {
		t.Fatalf("the result does not report the repair and its answer:\n%s", result.Content)
	}

	seed(t, root, "vendor/pkg/b.go", "package pkg\n")
	_, err = globTool.Run(context.Background(), json.RawMessage(`{"pattern":"*.go","path":"pkg"}`))
	refusal(t, err, "internal/pkg", "vendor/pkg", "only candidate")
}
