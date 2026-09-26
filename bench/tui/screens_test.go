package tui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	app "tofu/interface/tui"
)

const (
	trackColumn   = benchWidth - 1
	thumbGlyph    = "┃"
	railGlyph     = "│"
	chatBodyTop   = 2
	settingsLinkX = benchWidth - 8
	wheelX        = 70
	wheelY        = 10
	wheelRun      = 15
	progressRows  = 30
	dragFromX     = 42
	dragFromY     = 16
	dragToX       = 58
	dragToY       = 33
	dragJitter    = 8
)

func scrollingScreens() []string { return []string{"chat", "sub-agents", "file_edits", "shells"} }

func key(built *app.App, pressed tea.KeyPressMsg, times int) {
	for range times {
		built.Update(pressed)
	}
}

func onScreen(built *app.App, screen string, replayed sitting) {
	tab := tea.KeyPressMsg{Code: tea.KeyTab}
	switch screen {
	case "chat":
	case "sub-agents":
		key(built, tab, 1)
	case "file_edits":
		key(built, tab, 2)
		key(built, tea.KeyPressMsg{Code: 'n', Text: "n"}, replayed.largestEdit+1)
	case "shells":
		key(built, tab, 3)
	case "settings":
		_ = built.View()
		built.Update(tea.MouseClickMsg{X: settingsLinkX, Y: 0, Button: tea.MouseLeft})
		built.Update(tea.MouseReleaseMsg{X: settingsLinkX, Y: 0, Button: tea.MouseLeft})
	default:
		panic("bench/tui: unknown screen " + screen)
	}
}

type track struct{ top, height, thumb int }

func trackOf(built *app.App) (track, bool) {
	found := track{top: -1, thumb: -1}
	for y, line := range strings.Split(ansi.Strip(built.View().Content), "\n") {
		glyph := ansi.Cut(line, trackColumn, trackColumn+1)
		if glyph != thumbGlyph && glyph != railGlyph {
			continue
		}
		if found.top < 0 {
			found.top = y
		}
		if glyph == thumbGlyph && found.thumb < 0 {
			found.thumb = y
		}
		found.height = y - found.top + 1
	}
	return found, found.thumb >= 0
}

func pressTrack(built *app.App, y int) {
	built.Update(tea.MouseClickMsg{X: trackColumn, Y: y, Button: tea.MouseLeft})
}

func onEachScreen(b *testing.B, screens []string, last int, measure func(b *testing.B, built *app.App)) {
	history := readRecorded(b)
	replayed := history.sitting(b, last)
	for _, screen := range screens {
		b.Run(screen, func(b *testing.B) {
			began := time.Now()
			built := replayedApp(b, replayed)
			replay := time.Since(began)
			onScreen(built, screen, replayed)
			b.ReportAllocs()
			measure(b, built)
			b.ReportMetric(float64(len(replayed.turns)), "turns")
			b.ReportMetric(float64(replayed.events), "events")
			b.ReportMetric(float64(replay.Nanoseconds())/float64(replayed.events), "replay-ns/event")
		})
	}
}

func progressed(b *testing.B, built *app.App) {
	bar, scrolls := trackOf(built)
	for at := 0; b.Loop(); at++ {
		if scrolls {
			pressTrack(built, bar.top+at%min(bar.height, progressRows))
		}
		_ = built.View()
	}
}

func centred(built *app.App) {
	if bar, scrolls := trackOf(built); scrolls {
		pressTrack(built, bar.top+bar.height/2)
	}
}

func BenchmarkProgressedScreenRender(b *testing.B) {
	onEachScreen(b, append(scrollingScreens(), "settings"), recentTurns, progressed)
}

func BenchmarkSteadyScreenRender(b *testing.B) {
	onEachScreen(b, scrollingScreens(), recentTurns, func(b *testing.B, built *app.App) {
		centred(built)
		_ = built.View()
		for b.Loop() {
			_ = built.View()
		}
	})
}

func BenchmarkWheelEventFrame(b *testing.B) {
	onEachScreen(b, scrollingScreens(), recentTurns, func(b *testing.B, built *app.App) {
		centred(built)
		for at := 0; b.Loop(); at++ {
			button := tea.MouseWheelUp
			if at/wheelRun%2 == 1 {
				button = tea.MouseWheelDown
			}
			built.Update(tea.MouseWheelMsg{X: wheelX, Y: wheelY, Button: button})
			_ = built.View()
		}
	})
}

func BenchmarkScrollbarPress(b *testing.B) {
	onEachScreen(b, scrollingScreens(), recentTurns, func(b *testing.B, built *app.App) {
		bar, scrolls := trackOf(built)
		if !scrolls {
			b.Skip("this screen draws no scrollbar on the recorded data")
		}
		before := built.View().Content
		pressTrack(built, bar.thumb)
		built.Update(tea.MouseMotionMsg{X: trackColumn, Y: bar.top, Button: tea.MouseLeft})
		built.Update(tea.MouseReleaseMsg{X: trackColumn, Y: bar.top, Button: tea.MouseLeft})
		if built.View().Content == before {
			b.Fatal("a press and a drag on the thumb moved nothing")
		}
		bar, _ = trackOf(built)
		for b.Loop() {
			pressTrack(built, bar.thumb)
		}
	})
}

func BenchmarkChatScrollbarMetrics(b *testing.B) {
	onEachScreen(b, []string{"chat"}, recentTurns, func(b *testing.B, built *app.App) {
		press := tea.MouseClickMsg{X: 0, Y: chatBodyTop, Button: tea.MouseLeft}
		for b.Loop() {
			built.Update(press)
		}
	})
}

func dragFrame(b *testing.B, built *app.App) {
	_ = built.View()
	built.Update(tea.MouseClickMsg{X: dragFromX, Y: dragFromY, Button: tea.MouseLeft})
	built.Update(tea.MouseMotionMsg{X: dragToX, Y: dragToY, Button: tea.MouseLeft})
	for at := 0; b.Loop(); at++ {
		built.Update(tea.MouseMotionMsg{X: dragToX + at%dragJitter, Y: dragToY, Button: tea.MouseLeft})
		_ = built.View()
	}
}

func BenchmarkShellDragFrame(b *testing.B) {
	onEachScreen(b, []string{"shells"}, recentTurns, dragFrame)
}

func BenchmarkFileDiffDragFrame(b *testing.B) {
	onEachScreen(b, []string{"file_edits"}, recentTurns, dragFrame)
}

func BenchmarkSettingsCold(b *testing.B) {
	onEachScreen(b, []string{"settings"}, recentTurns, func(b *testing.B, built *app.App) {
		scope := tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}
		for b.Loop() {
			built.Update(scope)
			_ = built.View()
		}
	})
}

func BenchmarkResizeFrame(b *testing.B) {
	onEachScreen(b, append(scrollingScreens(), "settings"), recentTurns, func(b *testing.B, built *app.App) {
		for at := 1; b.Loop(); at++ {
			built.Update(tea.WindowSizeMsg{Width: benchWidth + at%2, Height: benchHeight})
			_ = built.View()
		}
	})
}

func BenchmarkSettingsRowChange(b *testing.B) {
	onEachScreen(b, []string{"settings"}, recentTurns, func(b *testing.B, built *app.App) {
		for at := 0; b.Loop(); at++ {
			step := tea.KeyDown
			if at%2 == 1 {
				step = tea.KeyUp
			}
			built.Update(tea.KeyPressMsg{Code: step})
			_ = built.View()
		}
	})
}

func BenchmarkFirstOpenRender(b *testing.B) {
	onEachScreen(b, append(scrollingScreens(), "settings"), recentTurns, func(b *testing.B, built *app.App) {
		began := time.Now()
		_ = built.View()
		first := time.Since(began)
		for b.Loop() {
			_ = built.View()
		}
		b.ReportMetric(float64(first.Nanoseconds()), "first-ns")
	})
}

func BenchmarkLongSessionRender(b *testing.B) {
	onEachScreen(b, append(scrollingScreens(), "settings"), everyTurn, progressed)
}
