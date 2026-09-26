package pointer_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"tofu/interface/tui/look"
	"tofu/interface/tui/pointer"
)

const split = 30

func paneFrame(width, height int, paint func(y int, text string) string) string {
	rows := make([]string, height)
	for y := range rows {
		side := fmt.Sprintf("side-%02d", y)
		text := fmt.Sprintf("out-%02d %s", y, strings.Repeat("x", y%5*10))
		row := side + strings.Repeat(" ", split-len(side)) + text
		rows[y] = paint(y, row+strings.Repeat(" ", width-len(row)))
	}
	return strings.Join(rows, "\n")
}

func plain(_ int, text string) string { return text }

func TestScrollbarPressHitboxMatchesRenderedTrack(t *testing.T) {
	for _, width := range []int{80, 120} {
		for _, height := range []int{24, 36} {
			for _, total := range []int{10, 60, 500} {
				for _, fromTop := range []int{0, 11} {
					track := pointer.Track{Top: 2, Height: height - 4, Total: total, Visible: height - 10, FromTop: fromTop}
					frame := strings.Split(pointer.Overlay(paneFrame(width, height, plain), width, track), "\n")
					hits := 0
					for y := 1; y < height-1; y++ {
						want := pointer.IsTrackGlyph(frame[y], width-1)
						_, got := pointer.Press(width-1, y, width, track)
						if got != want {
							t.Fatalf("%dx%d total=%d fromTop=%d y=%d: hit=%t rendered=%t", width, height, total, fromTop, y, got, want)
						}
						if _, left := pointer.Press(width-2, y, width, track); left {
							t.Fatalf("%dx%d y=%d: press left of the track grabbed it", width, height, y)
						}
						if got {
							hits++
						}
					}
					wantHits := 0
					if total > track.Visible {
						wantHits = track.Height
					}
					if hits != wantHits {
						t.Fatalf("%dx%d total=%d: %d track rows hit, want %d", width, height, total, hits, wantHits)
					}
				}
			}
		}
	}
}

func TestDragSelectionStaysInOriginPaneAndSkipsPadding(t *testing.T) {
	width, height := 120, 36
	frame := pointer.Overlay(paneFrame(width, height, plain), width, pointer.Track{Top: 2, Height: height - 4, Total: 100, Visible: 26})
	pane := pointer.Pane{Split: split, Width: width, Top: 2, Bottom: height - 2}
	mainSelection := pointer.SelectedText(frame, pointer.Selection{Start: pointer.Cell{X: 45, Y: 6}, End: pointer.Cell{X: 2, Y: 12}, Dragged: true}, pane)
	sideSelection := pointer.Selection{Start: pointer.Cell{X: 5, Y: 6}, End: pointer.Cell{X: 100, Y: 12}, Dragged: true}
	sidebarSelection := pointer.SelectedText(frame, sideSelection, pane)
	if mainSelection == "" || strings.Contains(mainSelection, "side-") {
		t.Fatalf("main-pane selection leaked sidebar text: %q", mainSelection)
	}
	if sidebarSelection == "" || strings.Contains(sidebarSelection, "out-") {
		t.Fatalf("sidebar selection leaked main-pane text: %q", sidebarSelection)
	}
	for _, row := range strings.Split(mainSelection+"\n"+sidebarSelection, "\n") {
		if strings.HasSuffix(row, " ") || strings.ContainsAny(row, "│┃") {
			t.Fatalf("selection copied padding or the scroll track: %q", row)
		}
	}
	painted := pointer.Paint(frame, sideSelection, pane)
	if !strings.Contains(painted, look.Painted(fmt.Sprintf("%-*s", split, "side-07"), look.Background, look.MutedColor)) {
		t.Fatal("selection highlight is missing")
	}
	if ansi.Strip(painted) != ansi.Strip(frame) {
		t.Fatal("selection highlight changed the text under it")
	}
}

func TestReferencesOnlyOccupyTheirVisibleCells(t *testing.T) {
	prefix := look.Muted("│ ") + "wrote 界 "
	view := "header\n" + prefix + look.Style(look.ReferenceType).Render("[tool#w106]") + " then [&go-dev 13]"
	x := ansi.StringWidth(prefix)
	for _, probe := range []struct {
		x, y int
		want string
	}{
		{x - 1, 1, ""},
		{x, 1, "[tool#w106]"},
		{x + 10, 1, "[tool#w106]"},
		{x + 11, 1, ""},
		{x + 17, 1, "[&go-dev 13]"},
		{x, 0, ""},
		{x, 2, ""},
	} {
		if got := pointer.ReferenceAt(view, probe.x, probe.y); got != probe.want {
			t.Fatalf("ReferenceAt(%d, %d) = %q, want %q", probe.x, probe.y, got, probe.want)
		}
	}
}

func BenchmarkScrollbarPress(b *testing.B) {
	width, height := 120, 36
	for _, total := range []int{100, 10_000, 1_000_000} {
		b.Run(fmt.Sprintf("rows=%d", total), func(b *testing.B) {
			track := pointer.Track{Top: 2, Height: height - 4, Total: total, Visible: height - 10, FromTop: total / 3}
			frame := strings.Split(pointer.Overlay(paneFrame(width, height, plain), width, track), "\n")
			y := track.Top
			for ansi.Strip(ansi.Cut(frame[y], width-1, width)) != "┃" {
				y++
			}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				drag, ok := pointer.Press(width-1, y, width, track)
				if !ok {
					b.Fatal("scrollbar press missed")
				}
				_ = drag.Scroll(y)
			}
		})
	}
}

func benchmarkDragFrame(b *testing.B, frame string) {
	pane := pointer.Pane{Split: split, Width: 120, Top: 2, Bottom: 34}
	s := pointer.Selection{Start: pointer.Cell{X: 42, Y: 16}, End: pointer.Cell{X: 58, Y: 33}, Pressed: true, Dragged: true}
	b.ReportAllocs()
	b.ResetTimer()
	for i := range b.N {
		s.End.X = 58 + i%8
		_ = pointer.Paint(frame, s, pane)
		_ = pointer.SelectedText(frame, s, pane)
	}
}

func BenchmarkShellDragFrame(b *testing.B) {
	benchmarkDragFrame(b, paneFrame(120, 36, plain))
}

func BenchmarkFileDiffDragFrame(b *testing.B) {
	benchmarkDragFrame(b, paneFrame(120, 36, func(y int, text string) string {
		background := look.DiffAddBackground
		if y%2 == 1 {
			background = look.DiffDeleteBackground
		}
		return look.Painted(text, look.Text, background)
	}))
}
