package tui_test

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"tofu/interface/tui"
	"tofu/interface/tui/shells"
)

const (
	benchEvents    = 30
	benchLogLines  = 50
	benchFileLines = 1108
	commandSettle  = 20 * time.Millisecond
)

func benchShells(at time.Time) []shells.Entry {
	var log strings.Builder
	for line := range benchLogLines {
		fmt.Fprintf(&log, "GET /page/%d 200 %dms\n", line, line%40)
	}
	return []shells.Entry{
		{Name: "dev-server", Command: "npm run dev", State: shells.Running, Started: at.Add(-6 * time.Minute), PID: 18432, Dir: "web", Log: log.String()},
		{Name: "test-watch", Command: "go test ./... -watch", State: shells.Running, Started: at.Add(-3 * time.Minute), PID: 21904, Log: log.String()},
		{Name: "docs", Command: "npm run docs", State: shells.Exited, Started: at.Add(-2 * time.Minute), PID: 17812, Log: "generated 42 pages\nfinished with exit code 0\n"},
	}
}

func run(app *tui.App, cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	select {
	case msg := <-done:
		if batch, isBatch := msg.(tea.BatchMsg); isBatch {
			for _, inner := range batch {
				run(app, inner)
			}
			return
		}
		if msg != nil {
			app.Update(msg)
		}
	case <-time.After(commandSettle):
	}
}

func benchApp(width, height int) *tui.App {
	at := time.Date(2026, 9, 25, 14, 0, 0, 0, time.UTC)
	app := tui.New(tui.Options{Repo: "bob", Now: func() time.Time { return at }, Shells: func() []shells.Entry { return benchShells(at) }})
	run(app, app.Init())
	app.Update(tea.WindowSizeMsg{Width: width, Height: height})
	for index := range benchEvents {
		id := "c" + strconv.Itoa(index)
		app.Update(tui.Event{Kind: tui.EventToolCall, ID: id, Tool: "read", Text: "internal/file" + strconv.Itoa(index) + ".go", Detail: "path internal/file.go\nlimit 200"})
		app.Update(tui.Event{Kind: tui.EventToolResult, ID: id, Text: "412 lines\n11.8 KB\nread whole"})
	}
	var created strings.Builder
	for line := range benchFileLines {
		fmt.Fprintf(&created, "func step%d(value int) int { return value + %d }\n", line, line)
	}
	app.Update(tui.Event{Kind: tui.EventToolCall, ID: "e54", Tool: "write", Text: "internal/large.go"})
	app.Update(tui.Event{Kind: tui.EventToolResult, ID: "e54", Text: "created", Created: created.String()})
	return app
}

func press(app *tui.App, keys ...string) {
	for _, key := range keys {
		code := []rune(key)[0]
		switch key {
		case "enter":
			code = tea.KeyEnter
		case "tab":
			code = tea.KeyTab
		}
		app.Update(tea.KeyPressMsg{Code: code})
	}
}

func onScreen(app *tui.App, screen string) {
	switch screen {
	case "sub-agents":
		press(app, "tab")
	case "file_edits":
		press(app, "tab", "tab", "enter", "enter")
	case "shells":
		press(app, "tab", "tab", "tab")
	}
}

func thumb(b *testing.B, app *tui.App, width int) int {
	for y, line := range strings.Split(ansi.Strip(app.View().Content), "\n") {
		if ansi.Cut(line, width-1, width) == "┃" {
			return y
		}
	}
	b.Fatal("scrollbar thumb not visible")
	return 0
}

func BenchmarkScrollbarPress(b *testing.B) {
	for _, screen := range []string{"sub-agents", "file_edits", "shells"} {
		b.Run(screen, func(b *testing.B) {
			app := benchApp(120, 36)
			onScreen(app, screen)
			y := thumb(b, app, 120)
			before := app.View().Content
			app.Update(tea.MouseClickMsg{X: 119, Y: y, Button: tea.MouseLeft})
			app.Update(tea.MouseMotionMsg{X: 119, Y: 2, Button: tea.MouseLeft})
			app.Update(tea.MouseReleaseMsg{X: 119, Y: 2, Button: tea.MouseLeft})
			if app.View().Content == before {
				b.Fatal("a press and a drag on the thumb moved nothing")
			}
			click := tea.MouseClickMsg{X: 119, Y: y, Button: tea.MouseLeft}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				app.Update(click)
			}
		})
	}
}

func benchmarkDragFrame(b *testing.B, screen string, width, height int) {
	app := benchApp(width, height)
	onScreen(app, screen)
	app.View()
	app.Update(tea.MouseClickMsg{X: 42, Y: 16, Button: tea.MouseLeft})
	app.Update(tea.MouseMotionMsg{X: 58, Y: 33, Button: tea.MouseLeft})
	b.ReportAllocs()
	b.ResetTimer()
	for i := range b.N {
		app.Update(tea.MouseMotionMsg{X: 58 + i%8, Y: 33, Button: tea.MouseLeft})
		_ = app.View()
	}
}

func BenchmarkShellDragFrame(b *testing.B) { benchmarkDragFrame(b, "shells", 167, 47) }

func BenchmarkFileDiffDragFrame(b *testing.B) { benchmarkDragFrame(b, "file_edits", 168, 42) }
