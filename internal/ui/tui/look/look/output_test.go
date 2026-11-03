package look

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestOutputLineSemanticColors(t *testing.T) {
	cases := []struct {
		line  string
		color Color
	}{
		{"ok  tofu/internal/shell  0.41s  passed", Mint},
		{"GET /health 200 3ms", Mint},
		{"warn: the lock file is older than go.sum", Amber},
		{"retry 2 of 3 after a reset", Amber},
		{"panic: index out of range", Red},
		{"build failed with 2 errors", Red},
		{"file change: internal/turn/loop.go", Blue},
		{"cache refreshed in 12ms", Blue},
		{"waiting for input", MutedColor},
	}
	for _, tc := range cases {
		got := OutputLine(tc.line)
		if ansi.Strip(got) != tc.line {
			t.Fatalf("rendered output changed text: %q", got)
		}
		if want := Style(tc.color).Render(tc.line); got != want {
			t.Fatalf("incorrect color for %q: got %q, want %q", tc.line, got, want)
		}
	}
}

func TestOutputLinePreservesEmbeddedSGR(t *testing.T) {
	line := "\x1b[33mwarning\x1b[0m: \x1b[34mcustom color\x1b[0m"
	if got := OutputLine(line); got != line {
		t.Fatalf("existing subprocess SGR was changed: %q", got)
	}
}

func TestOutputLineStripsEveryControlSequenceButSGR(t *testing.T) {
	cases := []struct{ name, line, want string }{
		{"erase line, title, CR overwrite", "50%\r\x1b[2K\x1b]0;title\x07\x1b[32mdone\x1b[0m", "\x1b[32mdone\x1b[0m"},
		{"OSC closed by ST", "\x1b]0;title\x1b\\\x1b[31mred\x1b[0m", "\x1b[31mred\x1b[0m"},
		{"cursor up and home", "\x1b[3A\x1b[H\x1b[1mbold\x1b[m", "\x1b[1mbold\x1b[m"},
		{"hyperlink keeps its text", "\x1b[4m\x1b]8;;https://example.com\x1b\\link\x1b]8;;\x1b\\\x1b[m", "\x1b[4mlink\x1b[m"},
		{"CRLF ending keeps the text", "\x1b[32mok\x1b[0m\r", "\x1b[32mok\x1b[0m"},
		{"bell and backspace", "\x1b[35ma\x07b\bc\x1b[0m", "\x1b[35mabc\x1b[0m"},
		{"truncated escape at the end", "\x1b[36mhalf\x1b[0m\x1b", "\x1b[36mhalf\x1b[0m"},
		{"wide text survives", "\x1b[32m界界 ok\x1b[0m\x1b[K", "\x1b[32m界界 ok\x1b[0m"},
	}
	for _, tc := range cases {
		if got := OutputLine(tc.line); got != tc.want {
			t.Errorf("%s: OutputLine(%q) = %q, want %q", tc.name, tc.line, got, tc.want)
		}
	}
}

func TestOutputLineColoursWhatIsLeftAfterStripping(t *testing.T) {
	cases := []struct {
		line, text string
		color      Color
	}{
		{"\x1b]0;build\x07compiled 12 packages", "compiled 12 packages", Mint},
		{"40%\r80%\rlistening on :8080", "listening on :8080", Mint},
		{"a\tb", "a" + strings.Repeat(" ", tabCells) + "b", MutedColor},
	}
	for _, tc := range cases {
		if got, want := OutputLine(tc.line), Style(tc.color).Render(tc.text); got != want {
			t.Errorf("OutputLine(%q) = %q, want %q", tc.line, got, want)
		}
	}
}
