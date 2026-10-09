package question

import (
	"slices"
	"strconv"
	"strings"
	"time"

	"tofu/interface/tui/look"
	"tofu/internal/turn"
	"tofu/internal/widget"
)

type Action int

const (
	Kept Action = iota
	Submitted
	Dismissed
)

const (
	padX        = 2
	focusMark   = "› "
	restMark    = "  "
	pickedMark  = "[x] "
	unpicked    = "[ ] "
	otherLabel  = "other"
	otherHint   = "type it in the chat, then enter"
	keysChoice  = "↑↓ move   enter choose   tab next question   esc dismiss"
	keysMulti   = "↑↓ move   space toggle   enter confirm   tab next question   esc dismiss"
	previewMark = "  │ "
)

type Form struct {
	ID        string
	questions []turn.PersonQuestion
	wait      time.Duration
	opened    time.Time
	at        int
	focus     []int
	picked    [][]bool
	typed     []string
	done      []bool
}

func Open(id string, questions []turn.PersonQuestion, wait time.Duration, now time.Time) *Form {
	f := &Form{ID: id, questions: questions, wait: wait, opened: now,
		focus: make([]int, len(questions)), picked: make([][]bool, len(questions)), typed: make([]string, len(questions)), done: make([]bool, len(questions))}
	for i, q := range questions {
		f.picked[i] = make([]bool, len(q.Options))
		if q.Recommended != nil {
			f.focus[i] = *q.Recommended
		}
	}
	return f
}

func (f *Form) onOther() bool { return f.focus[f.at] == len(f.questions[f.at].Options) }

func (f *Form) Key(key, composer string) (Action, bool) {
	q := f.questions[f.at]
	if composer != "" {
		if key == "enter" && f.onOther() {
			return f.confirm(composer), true
		}
		return Kept, false
	}
	switch key {
	case "esc":
		return Dismissed, true
	case "up":
		f.focus[f.at] = max(f.focus[f.at]-1, 0)
	case "down":
		f.focus[f.at] = min(f.focus[f.at]+1, len(q.Options))
	case "tab":
		f.at = (f.at + 1) % len(f.questions)
	case "shift+tab":
		f.at = (f.at + len(f.questions) - 1) % len(f.questions)
	case "space":
		if q.Type != turn.QuestionMulti || f.onOther() {
			return Kept, false
		}
		f.picked[f.at][f.focus[f.at]] = !f.picked[f.at][f.focus[f.at]]
	case "enter":
		return f.confirm(""), true
	default:
		at, err := strconv.Atoi(key)
		if err != nil || at < 1 || at > len(q.Options) {
			return Kept, false
		}
		f.focus[f.at] = at - 1
		if q.Type == turn.QuestionMulti {
			f.picked[f.at][at-1] = !f.picked[f.at][at-1]
			return Kept, true
		}
		return f.confirm(""), true
	}
	return Kept, true
}

func (f *Form) confirm(typed string) Action {
	q := f.questions[f.at]
	switch {
	case f.onOther() && typed == "":
		return Kept
	case f.onOther():
		f.typed[f.at] = typed
	case q.Type != turn.QuestionMulti || !slices.Contains(f.picked[f.at], true):
		clear(f.picked[f.at])
		f.picked[f.at][f.focus[f.at]] = true
	}
	f.done[f.at] = true
	if next := slices.Index(f.done, false); next >= 0 {
		f.at = next
		return Kept
	}
	return Submitted
}

func (f *Form) Replies() []turn.PersonReply {
	replies := make([]turn.PersonReply, len(f.questions))
	for i, q := range f.questions {
		replies[i] = turn.PersonReply{ID: q.ID, Chosen: []string{}, Text: f.typed[i]}
		for at, picked := range f.picked[i] {
			if picked && f.typed[i] == "" {
				replies[i].Chosen = append(replies[i].Chosen, q.Options[at].Label)
			}
		}
	}
	return replies
}

func (f *Form) Lines(width int, now time.Time) []string {
	q := f.questions[f.at]
	inner := width - 2*padX
	head := look.Style(look.Amber).Render("? "+q.Header) + look.Muted("  "+strconv.Itoa(f.at+1)+" of "+strconv.Itoa(len(f.questions)))
	lines := []string{widget.Fit(head, inner)}
	if waiting := f.countdown(q, now); waiting != "" {
		lines = append(lines, widget.Fit(look.Style(look.Amber).Render(waiting), inner))
	}
	lines = append(lines, widget.Fit(look.Title(q.Question), inner))
	for at, o := range q.Options {
		mark := restMark
		if at == f.focus[f.at] {
			mark = look.Accent(focusMark)
		}
		box := ""
		if q.Type == turn.QuestionMulti {
			box = unpicked
			if f.picked[f.at][at] {
				box = pickedMark
			}
		}
		label := strconv.Itoa(at+1) + ". " + o.Label
		if q.Recommended != nil && *q.Recommended == at {
			label += " (recommended)"
		}
		lines = append(lines, widget.Fit(mark+box+look.Style(look.Text).Render(label)+look.Muted("  "+o.Description), inner))
		if at == f.focus[f.at] && o.Preview != "" {
			for _, row := range strings.Split(o.Preview, "\n") {
				lines = append(lines, widget.Fit(look.Faint(previewMark+row), inner))
			}
		}
	}
	mark := restMark
	if f.onOther() {
		mark = look.Accent(focusMark)
	}
	lines = append(lines, widget.Fit(mark+strconv.Itoa(len(q.Options)+1)+". "+otherLabel+look.Muted("  "+otherHint), inner))
	keys := keysChoice
	if q.Type == turn.QuestionMulti {
		keys = keysMulti
	}
	lines = append(lines, widget.Fit(look.Faint(keys), inner))
	return strings.Split(look.Surface(width, len(lines), look.Panel, padX, strings.Join(lines, "\n")), "\n")
}

func (f *Form) countdown(q turn.PersonQuestion, now time.Time) string {
	left := f.wait - now.Sub(f.opened)
	switch {
	case f.wait == 0 || q.Recommended == nil:
		return ""
	case left <= 0:
		return "auto-selected " + q.Options[*q.Recommended].Label + " after timeout; answering still changes it"
	}
	return "auto-selects " + q.Options[*q.Recommended].Label + " in " + widget.Until(left)
}
