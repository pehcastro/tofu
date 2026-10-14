package tea

import (
	"fmt"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
)

const (
	defaultFPS = 60
	maxFPS     = 120
)

type renderer interface {
	start()

	close() error

	render(View)

	flush(closing bool) error

	reset()

	insertAbove(string) error

	setSyncdUpdates(bool)

	setWidthMethod(ansi.Method)

	resize(int, int)

	setColorProfile(colorprofile.Profile)

	clearScreen()

	writeString(string) (int, error)

	onMouse(MouseMsg) Cmd
}

type printLineMessage struct {
	messageBody string
}

func Println(args ...any) Cmd {
	return func() Msg {
		return printLineMessage{
			messageBody: fmt.Sprint(args...),
		}
	}
}

func Printf(template string, args ...any) Cmd {
	return func() Msg {
		return printLineMessage{
			messageBody: fmt.Sprintf(template, args...),
		}
	}
}

func encodeCursorStyle(style CursorShape, blink bool) int {

	style = (style * 2) + 1 //nolint:mnd
	if !blink {
		style++
	}
	return int(style)
}
