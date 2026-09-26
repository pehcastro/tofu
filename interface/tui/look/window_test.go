package look

import (
	"strings"
	"testing"

	"charm.land/bubbles/v2/viewport"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func viewportWindowOracle(content string, width, height, offset int) (string, int, int) {
	width, height = max(2, width), max(1, height)
	view := viewport.New(viewport.WithWidth(width-1), viewport.WithHeight(height))
	view.SetContent(content)
	view.GotoBottom()
	view.ScrollUp(max(0, offset))
	return view.View(), view.TotalLineCount(), view.YOffset()
}

func TestFastWindowMatchesViewport(t *testing.T) {
	styled := "\x1b[38;2;120;200;100mgreen\x1b[m"
	for _, content := range []string{
		"", "\x1b[31m\x1b[m", "short", "one\ntwo\nthree", "tail\n", strings.Repeat("x", 160),
		"first\n" + strings.Repeat("wide ", 70) + "\nlast", "a\r\nb\r\nc", styled + "\n" + strings.Repeat(styled, 20),
	} {
		for _, width := range []int{2, 8, 35, 120} {
			for _, height := range []int{1, 3, 12} {
				for _, offset := range []int{0, 1, 10, 100} {
					got, gotTotal, gotTop := Window(content, width, height, offset)
					want, wantTotal, wantTop := viewportWindowOracle(content, width, height, offset)
					if got != want || gotTotal != wantTotal || gotTop != wantTop {
						t.Fatalf("viewport mismatch content=%q width=%d height=%d offset=%d\n got total=%d top=%d view=%q\nwant total=%d top=%d view=%q", content, width, height, offset, gotTotal, gotTop, got, wantTotal, wantTop, want)
					}
				}
			}
		}
	}
}

func TestFastWindowDoesNotMutateContent(t *testing.T) {
	content := "a\n" + strings.Repeat("long ", 40) + "\nb"
	_, _, _ = Window(content, 8, 3, 0)
	if got, _, _ := Window(content, 120, 3, 0); !strings.Contains(got, "long long") {
		t.Fatal("windowing mutated source content")
	}
}

func TestFastWindowTrackedMatchesViewportComposition(t *testing.T) {
	for _, content := range []string{"", strings.Repeat("abcdef\n", 30), strings.Repeat("\x1b[32mgreen\x1b[m\n", 30), strings.Repeat("wide ", 50) + "\nend"} {
		for _, width := range []int{8, 20, 120} {
			for _, height := range []int{2, 8, 30} {
				for _, offset := range []int{0, 3, 99} {
					want, total, top := viewportWindowOracle(content, width, height, offset)
					want = lipgloss.JoinHorizontal(lipgloss.Top, want, ScrollTrack(height, total, height, top))
					got := WindowTracked(content, width, height, offset)
					if got != want {
						t.Fatalf("tracked viewport changed for width=%d height=%d offset=%d:\n got %q\nwant %q", width, height, offset, got, want)
					}
					for row, line := range strings.Split(got, "\n") {
						if cells := ansi.StringWidth(line); cells != width {
							t.Fatalf("tracked row %d is %d cells, want %d, for content=%q height=%d offset=%d", row, cells, width, content, height, offset)
						}
					}
				}
			}
		}
	}
}

func BenchmarkWindowLongTranscript(b *testing.B) {
	content := strings.Repeat("  "+strings.Repeat("This is a styled conversation line. ", 3)+"\n", 300)
	for _, name := range []string{"viewport", "visible-only"} {
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for i := range b.N {
				if name == "viewport" {
					_, _, _ = viewportWindowOracle(content, 120, 30, i%50)
				} else {
					_, _, _ = Window(content, 120, 30, i%50)
				}
			}
		})
	}
}
