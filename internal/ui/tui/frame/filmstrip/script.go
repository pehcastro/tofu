package filmstrip

import (
	"errors"
	"io"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
)

type Step struct {
	Verb string
	Text string
	Line int
}

func ReadScript(body string) []Step {
	var steps []Step
	for number, line := range strings.Split(body, "\n") {
		line = strings.TrimLeft(strings.TrimRight(line, "\r"), " \t")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		verb, text, _ := strings.Cut(line, " ")
		if verb != "type" && verb != "paste" {
			text = strings.TrimSpace(text)
		}
		steps = append(steps, Step{Verb: verb, Text: text, Line: number + 1})
	}
	return steps
}

func cells(fields []string, count, least int) ([]int, []string, bool) {
	if len(fields) < count {
		return nil, nil, false
	}
	numbers := make([]int, count)
	for index := range numbers {
		number, err := strconv.Atoi(fields[index])
		if err != nil || number < least {
			return nil, nil, false
		}
		numbers[index] = number
	}
	return numbers, fields[count:], true
}

func (d *Driver) point(verb string, fields []string) error {
	switch verb {
	case "click":
		if at, mods, ok := cells(fields, 2, 0); ok {
			return d.Click(at[0], at[1], mods...)
		}
		return errors.New("the step is click X Y [alt] [shift] [ctrl], with X and Y cells from 0")
	case "drag":
		if at, mods, ok := cells(fields, 4, 0); ok {
			return d.Drag(at[0], at[1], at[2], at[3], mods...)
		}
		return errors.New("the step is drag X1 Y1 X2 Y2 [alt] [shift] [ctrl], with every X and Y a cell from 0")
	case "wheel":
		at, turn, ok := cells(fields, 2, 0)
		if ok && len(turn) == 1 {
			turn = append(turn, "1")
		}
		if ok && len(turn) == 2 && (turn[0] == "up" || turn[0] == "down") {
			if notches, _, counted := cells(turn[1:], 1, 1); counted {
				return d.Wheel(at[0], at[1], turn[0] == "up", notches[0])
			}
		}
		return errors.New("the step is wheel X Y up|down [N], with N notches from 1")
	}
	size, rest, ok := cells(fields, 2, 1)
	if !ok || len(rest) > 0 {
		return errors.New("the step is resize W H, with W columns and H rows from 1")
	}
	d.Resize(size[0], size[1])
	return nil
}

func (d *Driver) Play(step Step, within time.Duration, plain bool, out io.Writer) error {
	if err := d.settle(within); err != nil {
		return err
	}
	switch step.Verb {
	case "type":
		d.Type(step.Text)
	case "paste":
		d.feed(tea.PasteMsg{Content: step.Text}, nil)
	case "key":
		return d.Press(step.Text)
	case "wait":
		return d.Await(step.Text, within)
	case "absent":
		if strings.Contains(d.Plain(), step.Text) {
			return errors.New(step.Text + " is on the screen and should not be")
		}
	case "screen":
		screen := d.Screen()
		if plain {
			screen = d.Plain()
		}
		_, err := io.WriteString(out, screen+"\n")
		return err
	case "click", "drag", "wheel", "resize":
		return d.point(step.Verb, strings.Fields(step.Text))
	default:
		return errors.New("no step is named " + strconv.Quote(step.Verb))
	}
	return nil
}
