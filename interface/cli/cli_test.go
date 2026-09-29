package cli

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/colorprofile"

	"tofu/internal/golden"
	"tofu/internal/konst"
)

const sampleHome = "/home/sample"

func pieces(page Page) map[string][]string {
	return map[string][]string{
		"title": append(append(
			page.Title("Model reload", nil, Verdict{Warn, "2 new · 1 changed · 1 failed"}),
			page.Title("Accounts", []string{"3 signed in"}, Verdict{Warn, "1 needs attention"})...),
			page.Title("A subject long enough that the verdict cannot share its line at eighty columns wide", nil, Verdict{Done, "installed"})...),
		"section": {
			page.Section("claude-sub", Verdict{Done, "23 served"}),
			page.Section("keys", Verdict{}),
			page.Status("models.dev", Verdict{Done, "812 context windows"}),
		},
		"facts": page.Facts([]Fact{{"folder", page.Path(sampleHome + "/.tofu/browser/extension")}, {"empty", ""}, {"id", "jednanpboiikklhkkkimnmdmjmgjgphh"}}),
		"card": page.Card(page.Label("#1")+Gap+page.Subject("ada@example.com"), Verdict{Active, "in use"},
			page.Facts([]Fact{{"login", "oauth · re-login by 27 Oct"}, {"7d", page.Bar(0.67) + Gap + page.Label("resets in 22h")}})),
		"bar": {page.Bar(0), page.Bar(0.49), page.Bar(0.67), page.Bar(0.95), page.Bar(1)},
		"rows": page.Rows([]Row{
			{Mark: Done, Cells: []string{"classifier", "OpenRouter", "····3498"}, Detail: "judges tool calls"},
			{Mark: Idle, Cells: []string{"web search", "Brave", "not set"}, Hint: "tofu login brave"},
			{Mark: Added, Cells: []string{"claude-sonnet-5-5"}, Detail: "allowed"},
			{Mark: Removed, Cells: []string{"claude-haiku-4"}, Detail: "gone"},
			{Mark: Changed, Cells: []string{"claude-opus-5"}, Detail: "window 200k to 1M"},
		}),
		"glyph":   {page.Glyph(Done) + page.Glyph(Active) + page.Glyph(Idle) + page.Glyph(Warn) + page.Glyph(Fail) + page.Glyph(Added) + page.Glyph(Removed) + page.Glyph(Changed)},
		"hint":    {page.Hint("tofu login codex-sub")},
		"steps":   page.Steps([]string{"open chrome://extensions", "Load unpacked"}),
		"error":   page.ErrorLine("model list refused (403)", "tofu login codex-sub"),
		"receipt": {page.Receipt(Added, "rule no-force-push", sampleHome+"/.tofu/rules/no-force-push.md")},
		"path":    {page.Path(sampleHome + "/.tofu"), page.Path(sampleHome + "x/.tofu"), page.Path("/elsewhere/.tofu")},
	}
}

func TestTheEnvelopeCarriesTheVersionAnRFC3339TimeAndAnEmptyProblemList(t *testing.T) {
	moment := time.Date(2026, 9, 29, 12, 0, 0, 123, time.FixedZone("BRT", -3*60*60))
	document, err := json.MarshalIndent(Envelope{Verb: "models reload", OK: true, At: moment, Data: map[string]int{"served": 23}}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	golden.Assert(t, "envelope.golden", strings.ReplaceAll(string(document), konst.Version, "VERSION")+"\n")
}

func TestEveryPieceMatchesItsGoldenInColourAndPlainAtTwoWidths(t *testing.T) {
	modes := map[string]colorprofile.Profile{"colour": colorprofile.TrueColor, "plain": colorprofile.NoTTY}
	for mode, profile := range modes {
		for _, columns := range []int{80, 120} {
			page := Page{Profile: profile, Width: min(columns, pageMaxCells), Home: sampleHome}
			for name, lines := range pieces(page) {
				var out bytes.Buffer
				if err := page.Print(&out, lines); err != nil {
					t.Fatal(err)
				}
				golden.Assert(t, name+"-"+mode+"-"+strconv.Itoa(columns)+".golden", out.String())
			}
		}
	}
}

func TestNoColourWritesNoEscapeAndDumbWritesOnlyASCII(t *testing.T) {
	cases := []struct {
		name    string
		environ []string
		escape  bool
		ascii   bool
	}{
		{"a forced colour terminal", []string{"TTY_FORCE=1", "COLORTERM=truecolor", "TERM=xterm-256color"}, true, false},
		{"NO_COLOR=1 on a terminal", []string{"TTY_FORCE=1", "COLORTERM=truecolor", "TERM=xterm-256color", "NO_COLOR=1"}, false, false},
		{"NO_COLOR=yes, which ParseBool rejects", []string{"TTY_FORCE=1", "COLORTERM=truecolor", "TERM=xterm-256color", "NO_COLOR=yes"}, false, false},
		{"NO_COLOR beats CLICOLOR_FORCE when piped", []string{"CLICOLOR_FORCE=1", "NO_COLOR=1"}, false, false},
		{"FORCE_COLOR when piped", []string{"FORCE_COLOR=1", "TERM=xterm-256color"}, true, false},
		{"TERM=dumb", []string{"TTY_FORCE=1", "TERM=dumb"}, false, true},
		{"TERM=dumb with CLICOLOR_FORCE", []string{"TERM=dumb", "CLICOLOR_FORCE=1"}, true, true},
	}
	for _, c := range cases {
		var out bytes.Buffer
		page := Detect(&out, c.environ)
		for _, lines := range pieces(page) {
			if err := page.Print(&out, lines); err != nil {
				t.Fatal(err)
			}
		}
		if got := bytes.IndexByte(out.Bytes(), 0x1b) >= 0; got != c.escape {
			t.Errorf("%s: wrote an ESC byte %v, want %v", c.name, got, c.escape)
		}
		wide := bytes.IndexFunc(out.Bytes(), func(r rune) bool { return r > 0x7f })
		if got := wide < 0; got != c.ascii {
			t.Errorf("%s: only ASCII %v, want %v, first wide rune at byte %d", c.name, got, c.ascii, wide)
		}
	}
}

func TestAPipedPageIsEightyWideAndLinesNeverPassTheWidth(t *testing.T) {
	var out bytes.Buffer
	page := Detect(&out, nil)
	if page.Width != 80 {
		t.Fatalf("a piped page is %d wide, want 80", page.Width)
	}
	if err := page.Print(&out, page.Title("x", nil, Verdict{Done, string(bytes.Repeat([]byte("y"), 200))})); err != nil {
		t.Fatal(err)
	}
	for _, line := range bytes.Split(bytes.TrimSuffix(out.Bytes(), []byte("\n")), []byte("\n")) {
		if cells := len([]rune(string(line))); cells > page.Width {
			t.Errorf("a line is %d cells on an %d page: %q", cells, page.Width, line)
		}
	}
}
