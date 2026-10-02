package tui

import (
	"bytes"
	"slices"
	"strings"
	"testing"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"

	"tofu/interface/tui/session"
)

type conhost struct {
	rows [][]rune
	x, y int
}

func (c *conhost) feed(t *testing.T, out string) {
	parser := ansi.NewParser()
	var state byte
	for len(out) > 0 {
		seq, width, n, next := ansi.DecodeSequenceWc(out, state, parser)
		state, out = next, out[n:]
		switch {
		case width > 0:
			c.rows[c.y][c.x] = []rune(seq)[0]
			c.x = min(c.x+width, len(c.rows[c.y])-1)
		case seq == "\r":
			c.x = 0
		case seq == "\n":
			c.y = min(c.y+1, len(c.rows)-1)
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
	case 'm', 'h', 'l':
	case 'H':
		second, _ := parser.Param(1, 1)
		c.y, c.x = first-1, max(second, 1)-1
	case 'G':
		c.x = first - 1
	case 'd':
		c.y = first - 1
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
		copy(row[c.x:], blanks)
	default:
		t.Fatalf("conhost stand-in cannot read %q", seq)
	}
}

func TestTheCookedLineReachesAConsoleWithoutTermWhole(t *testing.T) {
	const width, height = 140, 45
	at := time.Date(2026, 10, 2, 10, 38, 0, 0, time.UTC)
	model := session.New(func() time.Time { return at }, func(source string, _ int) []string { return []string{source} })
	model.SetSize(width, height)
	model.Append(session.Entry{Kind: session.User, Body: "While that runs: name the sub-agent"})
	var out bytes.Buffer
	renderer := uv.NewTerminalRenderer(&out, consoleEnviron([]string{"SystemRoot=C:\\Windows"}, "windows"))
	renderer.SetFullscreen(true)
	renderer.SetScrollOptim(false)
	screen := uv.NewScreenBuffer(width, height)
	console := &conhost{}
	for range height {
		console.rows = append(console.rows, []rune(strings.Repeat(" ", width)))
	}
	var want string
	draw := func() {
		view := model.View()
		out.Reset()
		screen.Clear()
		uv.NewStyledString(view).Draw(screen, screen.Bounds())
		renderer.Render(screen.RenderBuffer)
		if err := renderer.Flush(); err != nil {
			t.Fatal(err)
		}
		console.feed(t, out.String())
		for _, row := range strings.Split(ansi.Strip(view), "\n") {
			if strings.Contains(row, "cooked for") {
				want = strings.TrimRight(row, " ")
			}
		}
	}
	model.Start()
	draw()
	at = at.Add(2 * time.Second)
	model.Returned()
	draw()
	model.Close("cooked for", "")
	model.Stop()
	draw()
	for _, row := range console.rows {
		if got := strings.TrimRight(string(row), " "); strings.Contains(got, "cooked for") && got != want {
			t.Fatalf("the console shows\n%q\nwhere the frame says\n%q\nbytes of the last frame %q", got, want, out.String())
		}
	}
}

func TestATermThePersonSetIsLeftAlone(t *testing.T) {
	for _, set := range [][]string{{"TERM=screen"}, {"TERM="}, {"TERM=dumb", "COLORTERM=truecolor"}} {
		if got := consoleEnviron(set, "windows"); !slices.Equal(got, set) {
			t.Errorf("%q became %q", set, got)
		}
	}
	if got := consoleEnviron(nil, "linux"); len(got) != 0 {
		t.Errorf("an absent TERM off Windows became %q", got)
	}
}
