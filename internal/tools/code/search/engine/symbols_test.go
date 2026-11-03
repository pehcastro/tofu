package engine

import (
	"cmp"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

func TestFindsSymbolPassParsesTheBodiesTheScanHeldWithoutReadingAgain(t *testing.T) {
	matched := map[string]string{
		"a.go":   "package a\n\nfunc Target() {}\n",
		"b.go":   "package a\n\nfunc use() { Target() }\n",
		"bad.go": "package a\n\nfunc broken( {\n",
		"c.txt":  "Target\n",
	}
	attempt := goSymbolAttempt("Target", matched)
	if attempt.Lines != 2 || attempt.Files != 2 || attempt.NotRun != "" {
		t.Fatalf("%d lines in %d files, not run %q, want 2 lines in 2 files from the held bodies", attempt.Lines, attempt.Files, attempt.NotRun)
	}
}

func TestSymbolsAnswersRustAndTypeScriptFromCodeAlone(t *testing.T) {
	root := t.TempDir()
	comments := strings.NewReplacer("LINE", "/"+"/", "OPEN", "/"+"*", "CLOSE", "*"+"/")
	for rel, body := range map[string]string{
		"lib.rs": `pub fn target(x: u8) -> u8 { x }
pub struct Holder { id: u8 }
impl Holder {
    pub fn target(&self) -> u8 { 1 }
    fn uses(&self) -> u8 { target(2) + self.target() }
}
fn plain() {
    LINE target(1)
    OPEN target(2) CLOSE
    LINE/ target(3)
    let s = "target(4)";
    let r = r#"target(5)"#;
    let a = target::<u8>(6);
    let b = crate::lib::target(7);
    let v = vec![target(8)];
    println!("{}", target(9));
    let c = target_more(1) + pretarget(2);
    let d = Holder::default();
}
`,
		"broken.rs": "fn broken( {\n    target(1);\n",
		"app.ts": "export function target(a: number): number { return a }\n" +
			"class Box {\n" +
			"  target(a: number): number { return a }\n" +
			"  run(): number { return this.target(1) + target(2) }\n" +
			"}\n" +
			"LINE target(3)\n" +
			"const s = `target(4) ${target(5)}`\n" +
			"const t = \"target(6)\"\n" +
			"const u = flag ? target(7) : 0\n" +
			"const g = target<number>(8)\n",
	} {
		if err := os.WriteFile(filepath.Join(root, rel), []byte(comments.Replace(body)), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	files := []string{"app.ts", "broken.rs", "lib.rs"}
	for _, row := range []struct {
		name        string
		definitions []string
		callers     []string
	}{
		{
			name:        "target",
			definitions: []string{"app.ts:1 func target", "app.ts:3 method Box.target", "lib.rs:1 func target", "lib.rs:4 method Holder.target"},
			callers: []string{
				"app.ts:4 in Box.run", "app.ts:4 in Box.run", "app.ts:7 in the file scope", "app.ts:9 in the file scope", "app.ts:10 in g",
				"lib.rs:5 in Holder.uses", "lib.rs:5 in Holder.uses", "lib.rs:13 in plain", "lib.rs:14 in plain", "lib.rs:15 in plain", "lib.rs:16 in plain",
			},
		},
		{name: "default", callers: []string{"lib.rs:18 in plain"}},
		{name: "Holder", definitions: []string{"lib.rs:2 type Holder"}},
	} {
		graph, err := Symbols(root, files, row.name)
		if err != nil {
			t.Fatalf("%s: %v", row.name, err)
		}
		var definitions, callers []string
		for _, definition := range graph.Definitions {
			definitions = append(definitions, fmt.Sprintf("%s:%d %s %s", definition.Path, definition.FirstLine, definition.Kind, definition.Symbol))
		}
		for _, site := range graph.Callers {
			callers = append(callers, fmt.Sprintf("%s:%d in %s", site.Path, site.Line, cmp.Or(site.Within, "the file scope")))
		}
		if !slices.Equal(definitions, row.definitions) || !slices.Equal(callers, row.callers) {
			t.Errorf("%s:\ndefinitions %q\nwant        %q\ncallers %q\nwant    %q\n%s", row.name, definitions, row.definitions, callers, row.callers, graph.Text)
		}
		if graph.Parsed != 2 || graph.Unparsed != 1 || strings.Contains(graph.Text, "go file") {
			t.Errorf("%s: %d parsed and %d unparsed, want 2 and 1, and no line about go files over a rust and typescript tree:\n%s", row.name, graph.Parsed, graph.Unparsed, graph.Text)
		}
	}
}

func TestSymbolPassSkipsAFileGoneUnreadableAfterTheScan(t *testing.T) {
	root := t.TempDir()
	for rel, body := range map[string]string{
		"a.go":      "package a\n\nfunc Target() {}\n",
		"b.go":      "package a\n\nfunc use() { Target() }\n",
		"gone.go":   "package a\n\nfunc other() { Target() }\n",
		"folder.go": "package a\n\nfunc more() { Target() }\n",
	} {
		if err := os.WriteFile(filepath.Join(root, rel), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	files := []string{"a.go", "gone.go", "b.go", "folder.go"}
	result, err := Find(Request{Root: root, Files: files, Pattern: regexp.MustCompile("Target")})
	if err != nil || len(result.Units) != 4 {
		t.Fatalf("the scan does not match all four files before they go unreadable: %v\n%s", err, result.Text)
	}
	if err := os.Remove(filepath.Join(root, "gone.go")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "folder.go")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "folder.go"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, row := range []struct {
		name        string
		definitions int
		callers     int
		absence     string
	}{
		{name: "Target", definitions: 1, callers: 1},
		{name: "Absent", absence: "every readable source file was parsed, and the unreadable ones may declare or call Absent"},
	} {
		graph, err := Symbols(root, files, row.name)
		if err != nil {
			t.Fatalf("%s: one unreadable file failed the whole symbol pass: %v", row.name, err)
		}
		if len(graph.Definitions) != row.definitions || len(graph.Callers) != row.callers || graph.Parsed != 2 || graph.Unparsed != 0 {
			t.Fatalf("%s: %d definitions, %d callers, %d parsed, %d unparsed, want %d, %d, 2, 0:\n%s",
				row.name, len(graph.Definitions), len(graph.Callers), graph.Parsed, graph.Unparsed, row.definitions, row.callers, graph.Text)
		}
		if !strings.Contains(graph.Text, "2 files could not be read, the first gone.go: ") {
			t.Fatalf("%s: no line counting each unreadable file once and naming the first in list order by its project path:\n%s", row.name, graph.Text)
		}
		if strings.Contains(graph.Text, root) {
			t.Fatalf("%s: the reason carries the absolute path:\n%s", row.name, graph.Text)
		}
		if strings.Contains(graph.Text, "the files were parsed") || (row.absence != "" && !strings.Contains(graph.Text, row.absence)) {
			t.Fatalf("%s: the absence claims every file was parsed:\n%s", row.name, graph.Text)
		}
	}
}
