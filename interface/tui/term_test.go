package tui

import (
	"bytes"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"

	"tofu/interface/tui/session"
)

const conhostTabStop = 8

type conhost struct {
	rows [][]rune
	x, y int
}

func (c *conhost) lineFeed() {
	if c.y < len(c.rows)-1 {
		c.y++
		return
	}
	c.rows = append(c.rows[1:], []rune(strings.Repeat(" ", len(c.rows[0]))))
}

func (c *conhost) text() string {
	lines := make([]string, len(c.rows))
	for at, row := range c.rows {
		lines[at] = strings.TrimRight(string(row), " ")
	}
	return strings.Join(lines, "\n")
}

func (c *conhost) feed(t *testing.T, out string) {
	parser := ansi.NewParser()
	var state byte
	for len(out) > 0 {
		seq, width, n, next := ansi.DecodeSequenceWc(out, state, parser)
		state, out = next, out[n:]
		last := len(c.rows[c.y]) - 1
		switch {
		case width > 0:
			c.rows[c.y][c.x] = []rune(seq)[0]
			c.x = min(c.x+width, last)
		case seq == "\r":
			c.x = 0
		case seq == "\n":
			c.lineFeed()
		case seq == "\b":
			c.x = max(c.x-1, 0)
		case seq == "\t" && c.x == last:
			c.x = 0
			c.lineFeed()
		case seq == "\t":
			c.x = min((c.x/conhostTabStop+1)*conhostTabStop, last)
		case ansi.HasCsiPrefix(seq):
			c.csi(t, seq, parser)
		default:
			t.Fatalf("conhost stand-in cannot read %q", seq)
		}
	}
}

func (c *conhost) csi(t *testing.T, seq string, parser *ansi.Parser) {
	first, _ := parser.Param(0, 1)
	first = max(first, 1)
	row := c.rows[c.y]
	blanks := []rune(strings.Repeat(" ", len(row)))
	switch ansi.Cmd(parser.Command()).Final() {
	case 'm', 'h', 'l', '`':
	case 'H':
		second, _ := parser.Param(1, 1)
		c.y, c.x = first-1, max(second, 1)-1
	case 'G':
		c.x = first - 1
	case 'd':
		c.y = first - 1
	case 'A':
		c.y -= first
	case 'B':
		c.y += first
	case 'C':
		c.x += first
	case 'D':
		c.x -= first
	case '@':
		copy(row[c.x+first:], row[c.x:])
		copy(row[c.x:c.x+first], blanks)
	case 'P':
		copy(row[c.x:], row[c.x+first:])
		copy(row[len(row)-first:], blanks)
	case 'X':
		copy(row[c.x:min(c.x+first, len(row))], blanks)
	case 'K':
		erased, _ := parser.Param(0, 0)
		switch erased {
		case 0:
			copy(row[c.x:], blanks)
		case 1:
			copy(row[:c.x+1], blanks)
		default:
			copy(row, blanks)
		}
	default:
		t.Fatalf("conhost stand-in cannot read %q", seq)
	}
}

type windowsConsole struct {
	conhost
	out      bytes.Buffer
	renderer *uv.TerminalRenderer
	screen   uv.ScreenBuffer
}

func newWindowsConsole(environ []string, width, height int) *windowsConsole {
	console := &windowsConsole{screen: uv.NewScreenBuffer(width, height)}
	console.renderer = uv.NewTerminalRenderer(&console.out, consoleEnviron(environ, "windows"))
	console.renderer.SetFullscreen(true)
	console.renderer.SetRelativeCursor(false)
	console.renderer.SetTabStops(width)
	console.renderer.SetBackspace(true)
	console.renderer.SetScrollOptim(false)
	for range height {
		console.rows = append(console.rows, []rune(strings.Repeat(" ", width)))
	}
	return console
}

func (w *windowsConsole) show(t *testing.T, view string) {
	t.Helper()
	w.out.Reset()
	w.screen.Clear()
	uv.NewStyledString(view).Draw(w.screen, w.screen.Bounds())
	w.renderer.Render(w.screen.RenderBuffer)
	if err := w.renderer.Flush(); err != nil {
		t.Fatal(err)
	}
	w.feed(t, string(columnMovesAsCHA(w.out.Bytes())))
}

func TestTheCookedLineReachesAConsoleWithoutTermWhole(t *testing.T) {
	const width, height = 140, 45
	at := time.Date(2026, 10, 2, 10, 38, 0, 0, time.UTC)
	model := session.New(func() time.Time { return at }, func(source string, _ int) []string { return []string{source} })
	model.SetSize(width, height)
	model.Append(session.Entry{Kind: session.User, Body: "While that runs: name the sub-agent"})
	console := newWindowsConsole([]string{"SystemRoot=C:\\Windows"}, width, height)
	model.Start()
	console.show(t, model.View())
	at = at.Add(2 * time.Second)
	model.Returned()
	console.show(t, model.View())
	model.Close("cooked for", "")
	model.Stop()
	view := model.View()
	console.show(t, view)
	var want string
	for _, row := range strings.Split(ansi.Strip(view), "\n") {
		if strings.Contains(row, "cooked for") {
			want = strings.TrimRight(row, " ")
		}
	}
	for _, row := range console.rows {
		if got := strings.TrimRight(string(row), " "); strings.Contains(got, "cooked for") && got != want {
			t.Fatalf("the console shows\n%q\nwhere the frame says\n%q\nbytes of the last frame %q", got, want, console.out.String())
		}
	}
}

func TestThePickerAndItsFilterDrawInPlaceOnAWindowsConsole(t *testing.T) {
	const width, height = 120, 36
	for _, environ := range [][]string{{"SystemRoot=C:\\Windows"}, {"SystemRoot=C:\\Windows", "TERM=xterm"}, {"TERM=xterm-256color"}} {
		t.Run(strings.Join(environ, " "), func(t *testing.T) {
			app := newTestApp(Options{Repo: testRepo, Now: fixedClock(), Wires: anthropicAlone, Models: shippedFixture, Fresh: true})
			app.Init()
			app.Update(tea.WindowSizeMsg{Width: width, Height: height})
			console := newWindowsConsole(environ, width, height)
			esc, enter := tea.KeyPressMsg{Code: tea.KeyEscape}, tea.KeyPressMsg{Code: tea.KeyEnter}
			for _, step := range []func(){
				func() {},
				func() { app.openPicker("") },
				func() { typeText(app, "claude-sub") },
				func() { app.Update(esc) },
				func() { typeText(app, "/keys") },
				func() { app.Update(enter) },
				func() { typeText(app, "history") },
				func() { app.Update(esc) },
				func() { pressCtrlP(app) },
			} {
				step()
				view := app.View().Content
				console.show(t, view)
				for at, want := range strings.Split(ansi.Strip(view), "\n") {
					if got := strings.TrimRight(string(console.rows[at]), " "); got != strings.TrimRight(want, " ") {
						t.Fatalf("row %d of the console shows\n%q\nwhere the frame says\n%q\nthe console:\n%s", at, got, strings.TrimRight(want, " "), console.text())
					}
				}
			}
		})
	}
}

func TestOnWindowsBubbleteaAloneIsToldATermWithoutHardTabs(t *testing.T) {
	for _, set := range [][]string{nil, {"TERM=xterm"}, {"TERM="}, {"TERM=screen", "TERM=dumb", "COLORTERM=truecolor"}} {
		kept := slices.Clone(set)
		got := consoleEnviron(set, "windows")
		terms := slices.DeleteFunc(slices.Clone(got), func(entry string) bool { return !strings.HasPrefix(entry, "TERM=") })
		if !slices.Equal(terms, []string{termWithoutHardTabs}) {
			t.Errorf("%q gave bubbletea the TERM entries %q", set, terms)
		}
		if !slices.Equal(set, kept) {
			t.Errorf("the environment read for colour became %q, was %q", set, kept)
		}
	}
	for _, set := range [][]string{nil, {"TERM=xterm"}} {
		if got := consoleEnviron(set, "linux"); !slices.Equal(got, set) {
			t.Errorf("%q off Windows became %q", set, got)
		}
	}
}
