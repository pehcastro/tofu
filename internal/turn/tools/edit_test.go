package tools

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"tofu/internal/judge/state"
	"tofu/internal/turn"
)

const slowTsc = `const fs = require('fs');
const say = (line) => process.stdout.write(line + '\n');
const check = () => {
  const broken = fs.readdirSync('.').filter((name) => name.endsWith('.ts') && fs.readFileSync(name, 'utf8').includes('"many"'));
  broken.forEach((name) => say(name + "(1,14): error TS2322: Type 'string' is not assignable to type 'number'."));
  say('Found ' + broken.length + ' errors. Watching for file changes.');
};
let pending;
fs.watch('.', () => {
  clearTimeout(pending);
  pending = setTimeout(() => {
    say('File change detected. Starting incremental compilation...');
    setTimeout(check, 3000);
  }, 100);
});
say('Starting compilation in watch mode...');
check();
`

func warmEditor(t *testing.T, files map[string]string) (string, *turn.Typecheckers, func(oldString, newString string) (string, time.Duration)) {
	dir := t.TempDir()
	files["tsconfig.json"], files["package.json"], files["bun.lock"] = `{"compilerOptions":{"strict":true}}`, `{"name":"edited"}`, "{}"
	files["count.ts"] = "export const count: number = 3;\n"
	for name, body := range files {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	checkers := turn.NewTypecheckers()
	t.Cleanup(checkers.Close)
	checkers.Warm(dir)
	if warm, err := checkers.Typecheck(context.Background(), dir); err != nil || !strings.Contains(warm, "errors in .: 0") {
		t.Fatalf("the watcher did not finish its first check: %v\n%s", err, warm)
	}
	editor, err := NewEdit(dir)
	if err != nil {
		t.Fatal(err)
	}
	editor = editor.Checking(checkers)
	return dir, checkers, func(oldString, newString string) (string, time.Duration) {
		raw, _ := json.Marshal(map[string]string{"path": "count.ts", "old_string": oldString, "new_string": newString})
		started := time.Now()
		result, err := editor.Run(context.Background(), raw)
		if err != nil {
			t.Fatal(err)
		}
		return result.Content, time.Since(started)
	}
}

func TestAnEditOnASlowProjectReturnsAtOnceAndTypecheckReportsItAfter(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is not on PATH")
	}
	dir, checkers, edit := warmEditor(t, map[string]string{
		"node_modules/typescript/package.json": `{"name":"typescript","version":"5.9.0"}`,
		"node_modules/typescript/bin/tsc":      slowTsc,
	})
	edited, took := edit("3", `"many"`)
	if took >= time.Second || !strings.Contains(edited, "in the background") {
		t.Fatalf("the edit took %s and read:\n%s", took, edited)
	}
	after, err := checkers.Typecheck(context.Background(), dir)
	if err != nil || !strings.Contains(after, "count.ts(1,14): error TS2322") {
		t.Fatalf("the typecheck after the edit did not report its error: %v\n%s", err, after)
	}
	again, took := edit("count: number", "count:  number")
	if took >= time.Second || !strings.Contains(again, "in the background") || !strings.Contains(again, "count.ts(1,14): error TS2322") {
		t.Fatalf("the next edit took %s and did not carry the error the last check found:\n%s", took, again)
	}
	t.Logf("edit, %s:\n%s\ntypecheck after:\n%s\nnext edit:\n%s", took, edited, after, again)
}

func TestReplaceAllRenamesEveryExactMatchAndSaysHowMany(t *testing.T) {
	const cart = "package shop\n\nfunc (c Cart) CalcTotal() int { return 0 }\n\nfunc Use(c Cart) int { return c.CalcTotal() + c.CalcTotal() }\n"
	for _, c := range []struct {
		name, old, symbol string
		all               bool
		refused           string
		want              string
	}{
		{name: "every match replaced and counted", old: "CalcTotal", all: true, want: "3 occurrences"},
		{name: "a match nowhere writes nothing", old: "Missing", all: true, refused: "matches old_string"},
		{name: "without it three matches are refused", old: "CalcTotal", refused: "appears 3 times"},
		{name: "symbol and replace_all together are refused", symbol: "Use", all: true, refused: "replace_all"},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "cart.go"), []byte(cart), 0o600); err != nil {
				t.Fatal(err)
			}
			editor, err := NewEdit(dir)
			if err != nil {
				t.Fatal(err)
			}
			raw, _ := json.Marshal(map[string]any{"path": "cart.go", "old_string": c.old, "symbol": c.symbol, "new_string": "Subtotal", "replace_all": c.all})
			result, err := editor.Run(context.Background(), raw)
			got, _ := os.ReadFile(filepath.Join(dir, "cart.go"))
			if c.refused != "" {
				if err == nil || !strings.Contains(err.Error(), c.refused) || string(got) != cart {
					t.Fatalf("want a refusal naming %q and the file untouched, got %v and:\n%s", c.refused, err, got)
				}
				return
			}
			if err != nil || strings.Contains(string(got), "CalcTotal") || strings.Count(string(got), "Subtotal") != 3 || !strings.Contains(result.Content, c.want) {
				t.Fatalf("err %v, result:\n%s\nfile:\n%s", err, result.Content, got)
			}
		})
	}
}

func TestAnEditOnAFastProjectKeepsItsInlineTypecheck(t *testing.T) {
	if _, err := exec.LookPath("bun"); err != nil {
		t.Skip("bun is not on PATH")
	}
	_, _, edit := warmEditor(t, map[string]string{})
	edited, took := edit("3", `"many"`)
	if !strings.Contains(edited, "count.ts(1,14): error TS2322") || !strings.Contains(edited, "errors in count.ts: 1") || strings.Contains(edited, "in the background") {
		t.Fatalf("the edit on a fast project lost its inline typecheck after %s:\n%s", took, edited)
	}
	t.Logf("edit, %s:\n%s", took, edited)
}

func severalFiles(t *testing.T) (string, *turn.ReadLedger, map[string]string) {
	dir, ledger := t.TempDir(), turn.NewReadLedger()
	files := map[string]string{
		"a.go":       "package a\n\nconst A = 1\n",
		"b.go":       "package a\n\nconst B = 2\n",
		"c.txt":      "three\n",
		"d.go":       "package a\n\n" + strings.Repeat("var unreadLongEnoughToRefuse = 0\n", 200),
		"sub/e.go":   "package sub\n",
		"keep/f.go":  "package keep\n\nfunc F() int { return 1 }\n",
		"keep/g.txt": "g\n",
	}
	for name, body := range files {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if name != "d.go" {
			ledger.Mark(name, []byte(body))
		}
	}
	return dir, ledger, files
}

func change(path, old, becomes string) map[string]any {
	return map[string]any{"path": path, "old_string": old, "new_string": becomes}
}

func editCall(first map[string]any, more ...map[string]any) json.RawMessage {
	args := map[string]any{}
	for key, value := range first {
		args[key] = value
	}
	if len(more) > 0 {
		args["edits"] = more
	}
	raw, _ := json.Marshal(args)
	return raw
}

func TestOneEditCallChangesEveryFileItNamesOrNone(t *testing.T) {
	for _, c := range []struct {
		name    string
		call    json.RawMessage
		refused []string
		want    map[string]string
		shows   []string
	}{
		{name: "three files that all match are all written, each with its own diff",
			call:  editCall(change("a.go", "A = 1", "A = 10"), change("b.go", "B = 2", "B = 20"), change("c.txt", "three", "four")),
			want:  map[string]string{"a.go": "package a\n\nconst A = 10\n", "b.go": "package a\n\nconst B = 20\n", "c.txt": "four\n"},
			shows: []string{"--- a.go", "+const A = 10", "--- b.go", "+const B = 20", "--- c.txt", "+four"}},
		{name: "file two matching nothing writes none and names file two",
			call:    editCall(change("a.go", "A = 1", "A = 10"), change("b.go", "B = 9", "B = 20"), change("c.txt", "three", "four")),
			refused: []string{"b.go", "matches old_string", "no file in this call was written"}},
		{name: "file two never read writes none and names file two",
			call:    editCall(change("a.go", "A = 1", "A = 10"), change("d.go", "var unreadLongEnoughToRefuse = 0\n\nvar", "x")),
			refused: []string{"d.go has not been read"}},
		{name: "file two missing is not repaired to a lookalike, since no check read that path",
			call:    editCall(change("a.go", "A = 1", "A = 10"), change("e.go", "package sub", "package top")),
			refused: []string{"e.go does not exist", "no file in this call was written"}},
		{name: "file two left unable to parse writes none",
			call:    editCall(change("a.go", "A = 1", "A = 10"), change("b.go", "B = 2", "B = (")),
			refused: []string{"b.go unable to parse"}},
		{name: "two changes to one file apply in order to one text",
			call:  editCall(change("a.go", "A = 1", "A = 10"), change("a.go", "A = 10", "A = 100")),
			want:  map[string]string{"a.go": "package a\n\nconst A = 100\n"},
			shows: []string{"+const A = 100"}},
		{name: "one file named two ways is one file",
			call: editCall(change("a.go", "A = 1", "A = 10"), change("./a.go", "package a", "package b")),
			want: map[string]string{"a.go": "package b\n\nconst A = 10\n"}},
		{name: "a symbol beside another change to its file is refused",
			call:    editCall(map[string]any{"path": "keep/f.go", "symbol": "F", "new_string": "func F() int { return 2 }"}, change("keep/f.go", "package keep", "package kept")),
			refused: []string{"symbol", "keep/f.go"}},
		{name: "edits alone, with no change at the top, apply",
			call: editCall(map[string]any{}, change("keep/g.txt", "g", "h"), change("c.txt", "three", "four")),
			want: map[string]string{"keep/g.txt": "h\n", "c.txt": "four\n"}},
		{name: "a top that replaces nothing beside edits is not a change, as live sonnet sent it",
			call: editCall(map[string]any{"path": "a.go", "new_string": "x"}, change("keep/g.txt", "g", "h"), change("c.txt", "three", "four")),
			want: map[string]string{"keep/g.txt": "h\n", "c.txt": "four\n"}},
		{name: "an empty old_string appends new_string to the end of its file, as a live gpt run sent it",
			call:  editCall(change("a.go", "A = 1", "A = 10"), change("b.go", "", "\nconst C = 3\n")),
			want:  map[string]string{"a.go": "package a\n\nconst A = 10\n", "b.go": "package a\n\nconst B = 2\n\nconst C = 3\n"},
			shows: []string{"appended", "+const C = 3"}},
		{name: "a single edit with an empty old_string appends",
			call: editCall(change("c.txt", "", "four\n")),
			want: map[string]string{"c.txt": "three\nfour\n"}},
		{name: "an append to a missing file writes none and says write creates it",
			call:    editCall(change("a.go", "A = 1", "A = 10"), change("new.go", "", "package a\n")),
			refused: []string{"new.go does not exist", "write", "no file in this call was written"}},
		{name: "an append that leaves file two unable to parse writes none",
			call:    editCall(change("a.go", "A = 1", "A = 10"), change("b.go", "", "func (")),
			refused: []string{"b.go unable to parse", "no file in this call was written"}},
		{name: "a change that changes nothing among several is skipped and named, as live runs sent it",
			call:  editCall(change("a.go", "A = 1", "A = 10"), change("b.go", "package a", "package a"), change("c.txt", "three", "four")),
			want:  map[string]string{"a.go": "package a\n\nconst A = 10\n", "c.txt": "four\n"},
			shows: []string{"skipped", "b.go", "+four"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir, ledger, files := severalFiles(t)
			editor, err := NewEdit(dir)
			if err != nil {
				t.Fatal(err)
			}
			result, err := editor.Reading(ledger).Run(context.Background(), c.call)
			for name, body := range files {
				got, _ := os.ReadFile(filepath.Join(dir, name))
				if want, changed := c.want[name]; changed && err == nil {
					body = want
				}
				if string(got) != body {
					t.Errorf("%s holds:\n%s\nwant:\n%s", name, got, body)
				}
			}
			if c.refused != nil {
				for _, says := range c.refused {
					if err == nil || !strings.Contains(err.Error(), says) {
						t.Fatalf("want a refusal saying %q, got %v", says, err)
					}
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			for _, says := range c.shows {
				if !strings.Contains(result.Content, says) {
					t.Fatalf("the result does not show %q:\n%s", says, result.Content)
				}
			}
		})
	}
}

func TestAnEditBesideARepeatedLineIsWrittenAsMatched(t *testing.T) {
	const body = "package transfer\n\nimport (\n\t\"testing\"\n)\n\nfunc seeded() *store.Store {\n\ts := store.New()\n\ts.Put(account.New(\"a1\", \"Ada\"))\n\ts.Put(account.New(\"b2\", \"Bo\"))\n\treturn s\n}\n\n" +
		"func TestMoveBalances(t *testing.T) {\n\ts := seeded()\n\tif err := Move(s, \"a1\", \"b2\", 2500); err != nil {\n\t\tt.Fatal(err)\n\t}\n\ta, _ := s.Get(\"a1\")\n\tb, _ := s.Get(\"b2\")\n\tif a.Balance != 7500 || b.Balance != 2500 {\n\t\tt.Fatalf(\"a %d, b %d\", a.Balance, b.Balance)\n\t}\n}\n\n" +
		"func TestMoveAllCollectsFailures(t *testing.T) {\n\ts := seeded()\n\tif len(MoveAll(s, nil)) != 1 {\n\t\tt.Fatal(\"failed\")\n\t}\n}\n"
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "transfer_test.go"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	editor, err := NewEdit(dir)
	if err != nil {
		t.Fatal(err)
	}
	_, err = editor.Run(context.Background(), editCall(map[string]any{},
		change("transfer_test.go", "account.New(\"a1\", \"Ada\")", "account.New(\"a1\", \"Ada\", \"EUR\")"),
		change("transfer_test.go", "account.New(\"b2\", \"Bo\")", "account.New(\"b2\", \"Bo\", \"EUR\")"),
		change("transfer_test.go", "\ta, _ := s.Get(\"a1\")\n\tb, _ := s.Get(\"b2\")", "\ta, err := s.Get(\"a1\")\n\tif err != nil {\n\t\tt.Fatal(err)\n\t}\n\tb, err := s.Get(\"b2\")\n\tif err != nil {\n\t\tt.Fatal(err)\n\t}"),
		change("transfer_test.go", "import (\n\t\"testing\"", "import (\n\t\"errors\"\n\t\"testing\""),
		change("transfer_test.go", "func TestMoveAllCollectsFailures", "func TestMoveRejectsDifferentCurrencies(t *testing.T) {\n\ts := seeded()\n\tif err := Move(s, \"a1\", \"u1\", 100); !errors.Is(err, ErrCurrency) {\n\t\tt.Fatalf(\"err %v\", err)\n\t}\n\ta, _ := s.Get(\"a1\")\n\tif a.Balance != 10000 {\n\t\tt.Fatal(a.Balance)\n\t}\n}\n\nfunc TestMoveAllCollectsFailures")))
	got, _ := os.ReadFile(filepath.Join(dir, "transfer_test.go"))
	if err != nil || strings.Count(string(got), "a, _ := s.Get(\"a1\")") != 1 || !strings.Contains(string(got), "\"errors\"") {
		t.Fatalf("five changes to one file, each matching once as a live gpt run sent them, were refused or written wrong: %v\n%s", err, got)
	}
}

func TestEveryCheckOnAnEditCallReadsFileTwo(t *testing.T) {
	project := t.TempDir()
	harness := editCall(change("a.go", "A = 1", "A = 10"), change(".tofu/settings.json", "{}", `{"x":1}`))
	if !turn.PersonOnly(project, turn.GateRequest{Tool: "edit", Args: harness}) {
		t.Error("a harness file as file two is not person only")
	}
	if place := turn.CallPlace(turn.GateRequest{Tool: "edit", Args: editCall(change("a.go", "x", "y"), change("b.go", "x", "y"))}, project); !strings.Contains(place, "b.go") {
		t.Errorf("the place of a call that writes b.go is %q", place)
	}
	for _, key := range []string{"edits", "Edits"} {
		targets := state.TargetsOf(state.ToolGateInput{Tool: "edit", Cwd: project, ProjectDir: project,
			Input: map[string]any{"path": "a.go", "old_string": "x", "new_string": "y", key: []any{map[string]any{"path": "b.go", "old_string": "x", "new_string": "y"}}}})
		if len(targets.Targets) != 2 || !strings.HasSuffix(targets.Targets[1].Path, "/b.go") {
			t.Errorf("jev's write targets for an edit whose file two is under %q: %+v", key, targets)
		}
	}

	dir, ledger, files := severalFiles(t)
	editor, err := NewEdit(dir)
	if err != nil {
		t.Fatal(err)
	}
	budgeted := turn.WithSourceBudget([]turn.Tool{editor.Reading(ledger)}, nil)[0]
	_, err = budgeted.Run(context.Background(), editCall(change("a.go", "A = 1", "A = 10"), change("b.go", "B = 2", strings.Repeat("// x\n", 250))))
	var over turn.SourceBudgetError
	if !errors.As(err, &over) || over.Path != "b.go" {
		t.Fatalf("file two over the budget: want a refusal naming b.go, got %v", err)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "a.go")); string(got) != files["a.go"] {
		t.Fatalf("file one was written by a call the budget refused:\n%s", got)
	}
}
