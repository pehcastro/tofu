package filmstrip

import (
	"errors"
	"reflect"
	"strings"
	"time"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"tofu/interface/tui"
	"tofu/internal/konst"
)

const (
	settleQuiet   = konst.DriveSettleMillis * time.Millisecond
	settleCeiling = konst.DriveTimeoutMillis * time.Millisecond
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
	"home":      tea.KeyHome,
	"end":       tea.KeyEnd,
	"pgup":      tea.KeyPgUp,
	"pgdown":    tea.KeyPgDown,
}

var namedModifiers = map[string]tea.KeyMod{
	"ctrl+":  tea.ModCtrl,
	"alt+":   tea.ModAlt,
	"shift+": tea.ModShift,
}

type delivery struct {
	message tea.Msg
	cause   tea.Msg
}

func (d delivery) rearmedTimer() bool {
	kind := reflect.TypeOf(d.message)
	return kind == reflect.TypeOf(d.cause) && kind.Comparable()
}

type Driver struct {
	app        *tui.App
	deliveries chan delivery
	stopped    chan struct{}
}

func Drive(app *tui.App, width, height int) *Driver {
	d := &Driver{app: app, deliveries: make(chan delivery, konst.DriveMessageBuffer), stopped: make(chan struct{})}
	d.command(app.Init(), nil)
	d.feed(tea.WindowSizeMsg{Width: width, Height: height}, nil)
	return d
}

func (d *Driver) Close() { close(d.stopped) }

func (d *Driver) command(cmd tea.Cmd, cause tea.Msg) {
	if cmd == nil {
		return
	}
	go func() {
		message := cmd()
		if message == nil {
			return
		}
		select {
		case d.deliveries <- delivery{message: message, cause: cause}:
		case <-d.stopped:
		}
	}()
}

func (d *Driver) feed(message, cause tea.Msg) {
	if batch, batched := message.(tea.BatchMsg); batched {
		for _, cmd := range batch {
			d.command(cmd, cause)
		}
		return
	}
	_, cmd := d.app.Update(message)
	d.command(cmd, message)
}

func (d *Driver) Settle() error { return d.settle(settleCeiling) }

func (d *Driver) settle(within time.Duration) error {
	deadline := time.NewTimer(within)
	defer deadline.Stop()
	quiet := time.NewTimer(settleQuiet)
	defer quiet.Stop()
	var timers []delivery
	for {
		select {
		case got := <-d.deliveries:
			if got.rearmedTimer() {
				timers = append(timers, got)
				continue
			}
			d.feed(got.message, got.cause)
			quiet.Reset(settleQuiet)
		case <-quiet.C:
			for _, timer := range timers {
				d.feed(timer.message, timer.cause)
			}
			return nil
		case <-deadline.C:
			return errors.New("the app was still busy after " + within.String() + ", so the step never had a quiet screen to act on")
		}
	}
}

func (d *Driver) Type(text string) {
	for _, code := range text {
		d.feed(tea.KeyPressMsg{Code: code, Text: string(code)}, nil)
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
	code, named := namedKeys[name]
	if runes := []rune(name); !named && len(runes) == 1 {
		code, named = runes[0], true
	}
	if !named {
		return errors.New("no key is named " + name)
	}
	press := tea.KeyPressMsg{Code: code, Mod: held}
	if held == 0 && unicode.IsPrint(code) {
		press.Text = string(code)
	}
	d.feed(press, nil)
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
	d.feed(message, nil)
}

func (d *Driver) Resize(width, height int) {
	d.feed(tea.WindowSizeMsg{Width: width, Height: height}, nil)
}

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
		case got := <-d.deliveries:
			d.feed(got.message, got.cause)
		case <-poll.C:
		case <-deadline.C:
			return errors.New("waited " + within.String() + " for " + text + " and it never appeared")
		}
	}
}

func (d *Driver) Screen() string { return d.app.View().Content }

func (d *Driver) Plain() string { return ansi.Strip(d.Screen()) }
