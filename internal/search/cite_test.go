package search_test

import (
	"strings"
	"testing"

	"tofu/internal/search"
)

func tree(t testing.TB, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, body := range files {
		write(t, root, rel, body)
	}
	return root
}

const citedFile = "package loop\n\nfunc runToolCall() int {\n\treturn 7\n}\n"

func TestACitationThatResolves(t *testing.T) {
	root := tree(t, map[string]string{"internal/turn/loop.go": citedFile})
	for _, one := range []struct {
		name string
		text string
	}{
		{"path_and_line", "see internal/turn/loop.go:3 for the call"},
		{"path_and_range", "see internal/turn/loop.go:3-5 for the call"},
		{"path_and_symbol", "see internal/turn/loop.go:3:runToolCall for the call"},
		{"path_range_and_symbol", "see internal/turn/loop.go:1-5:runToolCall for the call"},
	} {
		t.Run(one.name, func(t *testing.T) {
			cited := search.Cite(root, one.text)
			if len(cited.Found) != 1 {
				t.Fatalf("found %d citations in %q, wanted 1: %+v", len(cited.Found), one.text, cited.Found)
			}
			if cited.Found[0].Claim != search.ClaimResolved {
				t.Fatalf("%s came back %s: %s", cited.Found[0].Text, cited.Found[0].Claim, cited.Found[0].Detail)
			}
			if cited.Refused != 0 || cited.Resolved != 1 {
				t.Fatalf("counted %d resolved and %d refused, wanted 1 and 0", cited.Resolved, cited.Refused)
			}
			if cited.Text != "citations: 1 found, 1 resolved" {
				t.Fatalf("the model would read %q", cited.Text)
			}
		})
	}
}

func TestACitationThatDoesNotResolveNamesWhichOfTheThreeItWas(t *testing.T) {
	root := tree(t, map[string]string{"internal/turn/loop.go": citedFile})
	for _, one := range []struct {
		name   string
		text   string
		claim  search.Claim
		detail string
	}{
		{"the_path_does_not_exist", "internal/turn/gone.go:3", search.ClaimNoFile, "no file is at that path under the working directory"},
		{"the_line_is_past_the_end", "internal/turn/loop.go:900", search.ClaimNoLine, "the file has 5 lines"},
		{"the_range_ends_past_the_end", "internal/turn/loop.go:3-900", search.ClaimNoLine, "the file has 5 lines"},
		{"the_range_ends_before_it_begins", "internal/turn/loop.go:5-3", search.ClaimNoLine, "the range ends before it begins"},
		{"the_symbol_is_not_on_the_named_line", "internal/turn/loop.go:4:runToolCall", search.ClaimNoSymbol, "the lines it names do not hold runToolCall"},
	} {
		t.Run(one.name, func(t *testing.T) {
			cited := search.Cite(root, one.text)
			if len(cited.Found) != 1 {
				t.Fatalf("found %d citations in %q, wanted 1", len(cited.Found), one.text)
			}
			if cited.Found[0].Claim != one.claim || cited.Found[0].Detail != one.detail {
				t.Fatalf("%s came back %s %q, wanted %s %q", one.text, cited.Found[0].Claim, cited.Found[0].Detail, one.claim, one.detail)
			}
			if cited.Refused != 1 {
				t.Fatalf("counted %d refused, wanted 1", cited.Refused)
			}
			if !strings.Contains(cited.Text, "refused "+one.text+" "+string(one.claim)+": "+one.detail) {
				t.Fatalf("the model would read %q, which does not name which of the three it was", cited.Text)
			}
		})
	}
}

func TestAFabricatedCitationIsRefusedRatherThanWarnedAbout(t *testing.T) {
	root := tree(t, map[string]string{"internal/turn/loop.go": citedFile})
	cited := search.Cite(root, "the cost row is written at internal/turn/loop.go:3 and read at internal/judge/ledger/write.go:28")

	if cited.Resolved != 1 || cited.Refused != 1 {
		t.Fatalf("counted %d resolved and %d refused, wanted 1 and 1", cited.Resolved, cited.Refused)
	}
	want := "citations: 2 found, 1 resolved, 1 refused. a refused citation is not evidence and must not be repeated as one\n" +
		"refused internal/judge/ledger/write.go:28 no_file: no file is at that path under the working directory"
	if cited.Text != want {
		t.Fatalf("the model would read:\n%s\nwanted:\n%s", cited.Text, want)
	}
	for _, soft := range []string{"warn", "may", "might", "possibly", "likely"} {
		if strings.Contains(cited.Text, soft) {
			t.Fatalf("the refusal hedges with %q: %s", soft, cited.Text)
		}
	}
}

func TestACitationToAFileThatHasSinceChangedDoesNotResolve(t *testing.T) {
	root := tree(t, map[string]string{"internal/turn/loop.go": citedFile})
	text := "internal/turn/loop.go:3:runToolCall"
	if claim := search.Cite(root, text).Found[0].Claim; claim != search.ClaimResolved {
		t.Fatalf("before the change the citation came back %s, so the test proves nothing about the change", claim)
	}

	write(t, root, "internal/turn/loop.go", "package loop\n\nfunc renamed() int {\n\treturn 7\n}\n")
	if renamed := search.Cite(root, text).Found[0]; renamed.Claim != search.ClaimNoSymbol {
		t.Fatalf("after the rename the citation came back %s, wanted %s", renamed.Claim, search.ClaimNoSymbol)
	}

	write(t, root, "internal/turn/loop.go", "package loop\n")
	if truncated := search.Cite(root, text).Found[0]; truncated.Claim != search.ClaimNoLine {
		t.Fatalf("after the truncation the citation came back %s, wanted %s", truncated.Claim, search.ClaimNoLine)
	}
}

func TestWhatIsNotACitation(t *testing.T) {
	root := tree(t, map[string]string{"internal/turn/loop.go": citedFile})
	for _, text := range []string{
		"F:\\localhost\\bob\\internal\\turn\\loop.go:3",
		"/absolute/internal/turn/loop.go:3",
		"../internal/turn/loop.go:3",
		"internal/turn/loop.go:0",
		"internal/turn/loop.go",
		"the ratio moved from 1.2:3 to 1.2:4",
	} {
		if found := search.Cite(root, text).Found; len(found) != 0 {
			t.Fatalf("%q was read as the citation %s, and it is not one this check claims to resolve", text, found[0].Text)
		}
	}
}

func TestTheSymbolMatchIsExactRatherThanFuzzy(t *testing.T) {
	root := tree(t, map[string]string{"a.go": "func runToolCallAgain() {}\n"})
	if claim := search.Cite(root, "a.go:1:runToolCall").Found[0].Claim; claim != search.ClaimNoSymbol {
		t.Fatalf("runToolCall matched inside runToolCallAgain and came back %s", claim)
	}
	if claim := search.Cite(root, "a.go:1:runToolCallAgain").Found[0].Claim; claim != search.ClaimResolved {
		t.Fatalf("the whole identifier came back %s", claim)
	}
}

func BenchmarkTenCitationsOverOneFile(b *testing.B) {
	root := tree(b, map[string]string{"internal/turn/loop.go": citedFile})
	var text strings.Builder
	for range 10 {
		text.WriteString("the row is written at internal/turn/loop.go:3:runToolCall and read back\n")
	}
	body := text.String()

	b.ResetTimer()
	for range b.N {
		cited := search.Cite(root, body)
		if len(cited.Found) != 10 || cited.Resolved != 10 {
			b.Fatalf("found %d citations and resolved %d, wanted 10 and 10", len(cited.Found), cited.Resolved)
		}
	}
}
