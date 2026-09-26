package look

import (
	"strconv"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

func TestPlainSurfaceFastMatchesLipgloss(t *testing.T) {
	for _, size := range [][2]int{{12, 4}, {60, 20}, {120, 36}} {
		for _, content := range []string{
			"", "\nhello", "\nhello\n", "\x1b[32mgreen\x1b[m\nnext",
			"wide ┃ glyph", strings.Repeat("small\n", 10), strings.Repeat("wide line ", 20),
			"\x1b[32m" + strings.Repeat("styled long ", 16) + "\x1b[m",
			"界界界界界", "combining é accent", "emoji 👩‍💻 and text",
		} {
			got, ok := PlainSurfaceFast(size[0], size[1], 2, content)
			if !ok {
				continue
			}
			if want := Surface(size[0], size[1], "", 2, content); got != want {
				t.Fatalf("plain surface changed at %dx%d content=%q\n got %q\nwant %q", size[0], size[1], content, got, want)
			}
		}
	}
}

func TestCachedSurfaceFastPathAndFallbackMatchLipgloss(t *testing.T) {
	for _, content := range []string{"\n" + strings.Repeat("short row\n", 15), "\n" + strings.Repeat("very long line of text ", 20), "\nwith\rreturn", "\nwith\ttab", "\n\x1b]8;;https://example.com\x1b\\link\x1b]8;;\x1b\\"} {
		var cache PaneCache
		want := Surface(80, 24, "", 2, content)
		for pass := range 2 {
			if cache.Surface(80, 24, "", 2, content) != want {
				t.Fatalf("cached transparent surface changed on pass %d for %q", pass, content)
			}
		}
	}
}

func TestPlainSurfaceOverflowMatchesLipgloss(t *testing.T) {
	for _, content := range []string{"12345678", "123456789", "\x1b[32m123456789\x1b[m", "12345\n123456789"} {
		got, ok := PlainSurfaceFast(12, 4, 2, content)
		if !ok || got != Surface(12, 4, "", 2, content) {
			t.Fatalf("overflow rendering changed for %q", content)
		}
	}
}

func TestJoinFixedPanesMatchesLipgloss(t *testing.T) {
	for _, size := range [][2]int{{18, 4}, {31, 20}, {100, 36}} {
		for _, bg := range []Color{"", Panel} {
			for _, content := range []string{"", "a\nb", "\x1b[32mgreen\x1b[m\nline two", "wide ┃ glyph \nwith style"} {
				left := Surface(size[0], size[1], bg, 2, content)
				right := Surface(size[0]+7, size[1], "", 2, content)
				if got, want := JoinFixedPanes(left, right), lipgloss.JoinHorizontal(lipgloss.Top, left, right); got != want {
					t.Fatalf("fixed panes changed at %dx%d background=%q content=%q", size[0], size[1], bg, content)
				}
			}
		}
	}
}

func BenchmarkReadingPaneSurface(b *testing.B) {
	var diff strings.Builder
	for row := range 32 {
		diff.WriteString("\n" + Faint(strconv.Itoa(1000+row)) + " ")
		diff.WriteString(Painted("+ ", Mint, DiffAddBackground) + Style(SyntaxKeyword).Render("func ") + Style(SyntaxFunction).Render("render"))
		diff.WriteString(Style(Text).Render("(width int) string { return ") + Style(SyntaxString).Render(`"row"`) + Style(Text).Render(" }"))
		diff.WriteString(Painted(strings.Repeat(" ", 24), Text, DiffAddBackground))
	}
	content := diff.String()
	if got, fast := PlainSurfaceFast(90, 33, 2, content); !fast || got != Surface(90, 33, "", 2, content) {
		b.Fatal("renderers differ")
	}
	for _, name := range []string{"lipgloss", "fixed-cells"} {
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				if name == "lipgloss" {
					_ = Surface(90, 33, "", 2, content)
				} else {
					_, _ = PlainSurfaceFast(90, 33, 2, content)
				}
			}
		})
	}
}
