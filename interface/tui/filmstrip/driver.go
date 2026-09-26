package filmstrip

import (
	"errors"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"tofu/interface/tui"
	"tofu/internal/konst"
)

var namedKeys = map[string]rune{
	"enter":     tea.KeyEnter,
	"esc":       tea.KeyEscape,
	"escape":    tea.KeyEscape,
	"tab":       tea.KeyTab,
	"backspace": tea.KeyBackspace,
	"space":     tea.KeySpace,
	"up":        tea.KeyUp,
	"down":      tea.KeyDown,
	"left":      tea.KeyLeft,
	"right":     tea.KeyRight,
}

var namedModifiers = map[string]tea.KeyMod{
	"ctrl+":  tea.ModCtrl,
	"alt+":   tea.ModAlt,
	"shift+": tea.ModShift,
}

type Driver struct {
	app      *tui.App
	messages chan tea.Msg
	stopped  chan struct{}
}

func Drive(app *tui.App, width, height int) *Driver {
	d := &Driver{app: app, messages: make(chan tea.Msg, konst.DriveMessageBuffer), stopped: make(chan struct{})}
	d.command(app.Init())
	d.feed(tea.WindowSizeMsg{Width: width, Height: height})
	return d
}

func (d *Driver) Close() { close(d.stopped) }

func (d *Driver) command(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	go func() {
		message := cmd()
		if message == nil {
			return
		}
		select {
		case d.messages <- message:
		case <-d.stopped:
		}
	}()
}

func (d *Driver) feed(message tea.Msg) {
	if batch, batched := message.(tea.BatchMsg); batched {
		for _, cmd := range batch {
			d.command(cmd)
		}
		return
	}
	_, cmd := d.app.Update(message)
	d.command(cmd)
}

func (d *Driver) Settle() {
	quiet := time.NewTimer(konst.DriveSettleMillis * time.Millisecond)
	defer quiet.Stop()
	for {
		select {
		case message := <-d.messages:
			d.feed(message)
			quiet.Reset(konst.DriveSettleMillis * time.Millisecond)
		case <-quiet.C:
			return
		}
	}
}

func (d *Driver) Type(text string) {
	for _, code := range text {
		d.feed(tea.KeyPressMsg{Code: code, Text: string(code)})
	}
}

func (d *Driver) Press(name string) error {
	held := tea.KeyMod(0)
	for prefix, modifier := range namedModifiers {
		if rest, modified := strings.CutPrefix(name, prefix); modified {
			held, name = modifier, rest
			break
		}
	}
	if code, named := namedKeys[name]; named {
		d.feed(tea.KeyPressMsg{Code: code, Mod: held})
		return nil
	}
	runes := []rune(name)
	if len(runes) != 1 {
		return errors.New("no key is named " + name)
	}
	press := tea.KeyPressMsg{Code: runes[0], Mod: held}
	if held == 0 {
		press.Text = name
	}
	d.feed(press)
	return nil
}

func (d *Driver) Click(x, y int, mods ...string) error {
	return d.Drag(x, y, x, y, mods...)
}

func (d *Driver) Drag(x1, y1, x2, y2 int, mods ...string) error {
	held := tea.KeyMod(0)
	for _, name := range mods {
		modifier, named := namedModifiers[name+"+"]
		if !named {
			return errors.New("no modifier is named " + name + ", only alt, shift and ctrl")
		}
		held |= modifier
	}
	d.pointAtDrawnFrame(tea.MouseClickMsg{X: x1, Y: y1, Button: tea.MouseLeft, Mod: held})
	steps := max(x2-x1, x1-x2, y2-y1, y1-y2)
	for step := 1; step <= steps; step++ {
		d.pointAtDrawnFrame(tea.MouseMotionMsg{X: x1 + (x2-x1)*step/steps, Y: y1 + (y2-y1)*step/steps, Button: tea.MouseLeft, Mod: held})
	}
	d.pointAtDrawnFrame(tea.MouseReleaseMsg{X: x2, Y: y2, Button: tea.MouseLeft, Mod: held})
	return nil
}

func (d *Driver) Wheel(x, y int, up bool, notches int) error {
	if notches < 1 {
		return errors.New("a wheel turns at least one notch")
	}
	button := tea.MouseWheelDown
	if up {
		button = tea.MouseWheelUp
	}
	for range notches {
		d.pointAtDrawnFrame(tea.MouseWheelMsg{X: x, Y: y, Button: button})
	}
	return nil
}

func (d *Driver) pointAtDrawnFrame(message tea.Msg) {
	d.app.View()
	d.feed(message)
}

func (d *Driver) Resize(width, height int) { d.feed(tea.WindowSizeMsg{Width: width, Height: height}) }

func (d *Driver) Await(text string, within time.Duration) error {
	deadline := time.NewTimer(within)
	defer deadline.Stop()
	poll := time.NewTicker(konst.DrivePollMillis * time.Millisecond)
	defer poll.Stop()
	for {
		if strings.Contains(d.Plain(), text) {
			return nil
		}
		select {
		case message := <-d.messages:
			d.feed(message)
		case <-poll.C:
		case <-deadline.C:
			return errors.New("waited " + within.String() + " for " + text + " and it never appeared")
		}
	}
}

func (d *Driver) Screen() string { return d.app.View().Content }

func (d *Driver) Plain() string { return ansi.Strip(d.Screen()) }
