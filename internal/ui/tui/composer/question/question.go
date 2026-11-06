package question

import (
	"slices"
	"strconv"
	"strings"
	"time"

	"tofu/internal/agent/loop"
	"tofu/internal/cli/render"
	"tofu/internal/ui/tui/look/look"
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
	keysNote    = "a note typed in the chat goes with the option enter chooses"
	previewMark = "  │ "
	columnGap   = " │ "
	columnLeast = 24
	previewMost = 14
)

type Form struct {
	ID        string
	questions []loop.PersonQuestion
	wait      time.Duration
	opened    time.Time
	at        int
	focus     []int
	picked    [][]bool
	typed     []string
	done      []bool
}

func Open(id string, questions []loop.PersonQuestion, wait time.Duration, now time.Time) *Form {
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

func proposes(q loop.PersonQuestion) bool {
	return slices.ContainsFunc(q.Options, func(o loop.PersonOption) bool { return o.Preview != "" })
}

func (f *Form) Key(key, composer string) (Action, bool) {
	q := f.questions[f.at]
	if composer != "" {
		if key == "enter" && (f.onOther() || proposes(q)) {
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
		if q.Type != loop.QuestionMulti || f.onOther() {
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
		if q.Type == loop.QuestionMulti {
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
		clear(f.picked[f.at])
	case q.Type != loop.QuestionMulti || !slices.Contains(f.picked[f.at], true):
		clear(f.picked[f.at])
		f.picked[f.at][f.focus[f.at]] = true
	}
	f.typed[f.at], f.done[f.at] = typed, true
	if next := slices.Index(f.done, false); next >= 0 {
		f.at = next
		return Kept
	}
	return Submitted
}

func (f *Form) Replies() []loop.PersonReply {
	replies := make([]loop.PersonReply, len(f.questions))
	for i, q := range f.questions {
		replies[i] = loop.PersonReply{ID: q.ID, Chosen: []string{}, Text: f.typed[i]}
		for at, picked := range f.picked[i] {
			if picked {
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
	lines := []string{render.Fit(head, inner)}
	if waiting := f.countdown(now); waiting != "" {
		lines = append(lines, render.Fit(look.Style(look.Amber).Render(waiting), inner))
	}
	lines = append(lines, render.Fit(look.Title(q.Question), inner))
	column := (inner - (len(q.Options)-1)*render.Cells(columnGap)) / max(len(q.Options), 1)
	if column >= columnLeast && proposes(q) {
		lines = append(lines, f.columns(column)...)
	} else {
		for at, o := range q.Options {
			lines = append(lines, render.Fit(f.optionHead(at)+look.Muted("  "+o.Description), inner))
			if at == f.focus[f.at] {
				for _, row := range previewRows(o.Preview) {
					lines = append(lines, render.Fit(look.Faint(previewMark)+row, inner))
				}
			}
		}
	}
	mark := restMark
	if f.onOther() {
		mark = look.Accent(focusMark)
	}
	lines = append(lines, render.Fit(mark+strconv.Itoa(len(q.Options)+1)+". "+otherLabel+look.Muted("  "+otherHint), inner))
	keys := keysChoice
	if q.Type == loop.QuestionMulti {
		keys = keysMulti
	}
	if proposes(q) {
		lines = append(lines, render.Fit(look.Faint(keysNote), inner))
	}
	lines = append(lines, render.Fit(look.Faint(keys), inner))
	return strings.Split(look.Surface(width, len(lines), look.Panel, padX, strings.Join(lines, "\n")), "\n")
}

func (f *Form) optionHead(at int) string {
	q := f.questions[f.at]
	mark, box := restMark, ""
	if at == f.focus[f.at] {
		mark = look.Accent(focusMark)
	}
	if q.Type == loop.QuestionMulti {
		box = unpicked
		if f.picked[f.at][at] {
			box = pickedMark
		}
	}
	label := strconv.Itoa(at+1) + ". " + q.Options[at].Label
	if q.Recommended != nil && *q.Recommended == at {
		label += " (recommended)"
	}
	return mark + box + look.Style(look.Text).Render(label)
}

func (f *Form) columns(width int) []string {
	q := f.questions[f.at]
	cells, tallest := make([][]string, len(q.Options)), 0
	for at, o := range q.Options {
		cells[at] = []string{f.optionHead(at), look.Muted(restMark + o.Description)}
		for _, row := range previewRows(o.Preview) {
			cells[at] = append(cells[at], restMark+row)
		}
		tallest = max(tallest, len(cells[at]))
	}
	rows := make([]string, tallest)
	for row := range rows {
		parts := make([]string, len(cells))
		for at, cell := range cells {
			text := ""
			if row < len(cell) {
				text = cell[row]
			}
			parts[at] = render.Pad(render.Fit(text, width), width)
		}
		rows[row] = strings.Join(parts, look.Faint(columnGap))
	}
	return rows
}

func previewRows(preview string) []string {
	if preview == "" {
		return nil
	}
	raw := strings.Split(strings.TrimRight(preview, "\n"), "\n")
	var rows []string
	for _, row := range raw[:min(len(raw), previewMost)] {
		colour := look.Text
		switch {
		case strings.HasPrefix(row, "+++"), strings.HasPrefix(row, "---"):
			colour = look.MutedColor
		case strings.HasPrefix(row, "+"):
			colour = look.Mint
		case strings.HasPrefix(row, "-"):
			colour = look.Red
		case strings.HasPrefix(row, "@@"):
			colour = look.Blue
		}
		rows = append(rows, look.Style(colour).Render(strings.ReplaceAll(row, "\t", "    ")))
	}
	if hidden := len(raw) - previewMost; hidden > 0 {
		rows = append(rows, look.Faint("+"+strconv.Itoa(hidden)+" more rows"))
	}
	return rows
}

func (f *Form) countdown(now time.Time) string {
	left := f.wait - now.Sub(f.opened)
	switch {
	case f.wait == 0:
		return ""
	case left <= 0:
		return "no answer yet; the lead was told none came, and answering still reaches it"
	}
	return "no answer yet; the lead hears in " + render.Until(left) + " that none came, and picks nothing"
}
