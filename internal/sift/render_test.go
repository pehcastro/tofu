package sift

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func shapes() map[string]string {
	return map[string]string{
		"one paragraph":          "the answer is four",
		"trailing newline":       "the answer is four\n",
		"two paragraphs":         "the answer is four\n\nand here is why\n",
		"windows line endings":   "the answer is four\r\n\r\nand here is why\r\n",
		"leading blank lines":    "\n\nthe answer is four\n",
		"wide separator":         "one\n\n\n\n\ntwo\n",
		"whitespace separator":   "one\n   \n\t\ntwo\n",
		"fence holds a blank":    "intro\n\n```go\nfunc a() {\n\n}\n```\n\nafter\n",
		"list stays one part":    "- a\n- b\n- c\n\nafter\n",
		"only blank lines":       "\n\n\n",
		"empty":                  "",
		"no trailing newline x2": "one\n\ntwo",
	}
}

func TestSplitAndJoinAreExactInverses(t *testing.T) {
	for name, text := range shapes() {
		t.Run(name, func(t *testing.T) {
			parts, err := Split(text)
			if err != nil {
				t.Fatalf("Split: %v", err)
			}
			if got := Join(parts); got != text {
				t.Fatalf("Join(Split(x)) != x\n want %q\n  got %q", text, got)
			}
		})
	}
}

func TestElisionIsReversible(t *testing.T) {
	for name, text := range shapes() {
		for _, keep := range []string{"none", "odd", "all"} {
			t.Run(name+"/"+keep, func(t *testing.T) {
				parts, err := Split(text)
				if err != nil {
					t.Fatalf("Split: %v", err)
				}
				marks := make([]Mark, len(parts))
				for i := range marks {
					switch keep {
					case "all":
						marks[i] = Mark{Keep: true}
					case "odd":
						marks[i] = Mark{Keep: i%2 == 1, Reason: "padding 3.00"}
					default:
						marks[i] = Mark{Reason: "padding 3.00"}
					}
				}
				restored, err := Restore(Render(parts, marks))
				if err != nil {
					t.Fatalf("Restore: %v", err)
				}
				if restored != text {
					t.Fatalf("the original is not recoverable\n want %q\n  got %q", text, restored)
				}
			})
		}
	}
}

func TestElisionIsReversibleOverTheCorpus(t *testing.T) {
	for _, path := range corpusFiles(t) {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		text := string(body)
		parts, err := Split(text)
		if err != nil {
			t.Fatalf("%s: Split: %v", path, err)
		}
		marks := make([]Mark, len(parts))
		for i := range marks {
			marks[i] = Cheap(parts[i])
		}
		rendered := Render(parts, marks)
		restored, err := Restore(rendered)
		if err != nil {
			t.Fatalf("%s: Restore: %v", path, err)
		}
		if restored != text {
			t.Fatalf("%s: the original is not recoverable from what sift emits", path)
		}
	}
}

func TestSplitRefusesTextThatAlreadyCarriesAMarker(t *testing.T) {
	for _, text := range []string{"before\n[sift:2 preamble 0.90]\nafter\n", "before\n" + fenceLine + "\nkept 1/2 ends 1\n"} {
		if _, err := Split(text); err != ErrAlreadySifted {
			t.Fatalf("Split(%q) = %v, want ErrAlreadySifted", text, err)
		}
	}
}

func TestRenderPrintsTheKeptFraction(t *testing.T) {
	parts, err := Split("one\n\ntwo\n\nthree\n")
	if err != nil {
		t.Fatal(err)
	}
	rendered := Render(parts, []Mark{{Keep: true}, {Reason: "preamble 3.00"}, {Keep: true}})
	if !strings.Contains(rendered, "kept 2/3 ends 1") {
		t.Fatalf("the render carries no kept fraction:\n%s", rendered)
	}
}

func corpusFiles(t *testing.T) []string {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join("testdata", "corpus", "*.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) == 0 {
		t.Fatal("the corpus is empty, so nothing is proved")
	}
	return paths
}
