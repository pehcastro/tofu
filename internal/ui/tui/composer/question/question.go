package question

import (
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

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
	besideGap   = "  "
	inChat      = "the full question is in the chat above"
	otherLabel  = "other"
	otherHint   = "type it in the chat, then enter"
	otherEmpty  = "the chat is empty: type your answer there first, then press enter"
	returnLabel = "return"
	returnHint  = "close the question without choosing anything"
	keysChoice  = "↑↓ move   enter choose   tab next question"
	keysMulti   = "↑↓ move   space toggle   enter confirm   tab next question"
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
	lift      int
	nudged    bool
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

func (f *Form) onReturn() bool { return f.focus[f.at] == len(f.questions[f.at].Options)+1 }

func proposes(q loop.PersonQuestion) bool {
	return slices.ContainsFunc(q.Options, func(o loop.PersonOption) bool { return o.Preview != "" })
}

func (f *Form) Key(key, composer string) (Action, bool) {
	q := f.questions[f.at]
	lifted := f.lift
	f.lift, f.nudged = 0, false
	if composer != "" {
		if key == "enter" && !f.onReturn() && (f.onOther() || proposes(q)) {
			return f.confirm(composer), true
		}
		return Kept, false
	}
	switch key {
	case "esc":
	case "up":
		if f.focus[f.at] == 0 {
			f.lift = lifted + 1
		}
		f.focus[f.at] = max(f.focus[f.at]-1, 0)
	case "down":
		f.focus[f.at] = min(f.focus[f.at]+1, len(q.Options)+1)
	case "tab":
		f.at = (f.at + 1) % len(f.questions)
	case "shift+tab":
		f.at = (f.at + len(f.questions) - 1) % len(f.questions)
	case "space":
		if q.Type != loop.QuestionMulti || f.focus[f.at] >= len(q.Options) {
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
	case f.onReturn():
		return Dismissed
	case f.onOther() && typed == "":
		f.nudged = true
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

func (f *Form) Lines(width, most, queued int, now time.Time) []string {
	q := f.questions[f.at]
	inner := width - 2*padX
	count := strconv.Itoa(f.at+1) + " of " + strconv.Itoa(len(f.questions))
	switch {
	case queued == 1:
		count += " · 1 more ask waits"
	case queued > 1:
		count += " · " + strconv.Itoa(queued) + " more asks wait"
	}
	head := beside(hang(amber("? "), q.Header, inner, amber), "  ", count, inner, look.Muted)
	if waiting := f.countdown(now); waiting != "" {
		head = append(head, hang("", waiting, inner, amber)...)
	}
	blocks, focused := f.blocks(inner)
	body := slices.Concat(blocks...)
	focusFrom := len(slices.Concat(blocks[:focused]...))
	focusTo := focusFrom + len(blocks[focused])
	keys := keysChoice
	if q.Type == loop.QuestionMulti {
		keys = keysMulti
	}
	var foot []string
	if proposes(q) {
		foot = hang("", keysNote, inner, look.Faint)
	}
	foot = append(foot, hang("", keys, inner, look.Faint)...)
	if room := most - len(head) - len(foot) - 1; len(head)+len(body)+len(foot) > most && room > 0 {
		top := max(focusTo-room, 0)
		if focusTo-focusFrom > room {
			top = focusFrom
		}
		f.lift = min(f.lift, top)
		top -= f.lift
		end := min(top+room, len(body))
		position := "rows " + strconv.Itoa(top+1) + "-" + strconv.Itoa(end) + " of " + strconv.Itoa(len(body))
		if top > 0 {
			position += "   ↑ on the first option shows the rows above"
		}
		body = append(body[top:end:end], look.Faint(render.Fit(position, inner)))
	}
	lines := slices.Concat(head, body, foot)
	lines = lines[:min(len(lines), max(most, 1))]
	return strings.Split(look.Surface(width, len(lines), look.Panel, padX, strings.Join(lines, "\n")), "\n")
}

func (f *Form) blocks(inner int) ([][]string, int) {
	q := f.questions[f.at]
	blocks, focused := [][]string{{look.Muted(inChat)}}, 1+f.focus[f.at]
	column := (inner - (len(q.Options)-1)*render.Cells(columnGap)) / max(len(q.Options), 1)
	if column >= columnLeast && proposes(q) {
		blocks, focused = append(blocks, f.columns(column)), 1+max(f.focus[f.at]-len(q.Options)+1, 0)
	} else {
		for at, o := range q.Options {
			shown := f.row(at, labelOf(q, at), "", inner, look.Muted)
			if at == f.focus[f.at] {
				for _, row := range previewRows(o.Preview, inner-render.Cells(previewMark)) {
					shown = append(shown, look.Faint(previewMark)+row)
				}
			}
			blocks = append(blocks, shown)
		}
	}
	other, otherPaint := otherHint, look.Muted
	if f.nudged {
		other, otherPaint = otherEmpty, amber
	}
	blocks = append(blocks, f.row(len(q.Options), otherLabel, other, inner, otherPaint), f.row(len(q.Options)+1, returnLabel, returnHint, inner, look.Muted))
	return blocks, focused
}

func amber(text string) string { return look.Style(look.Amber).Render(text) }

func ink(text string) string { return look.Style(look.Text).Render(text) }

func hang(lead, text string, width int, paint func(string) string) []string {
	indent := strings.Repeat(" ", render.Cells(lead))
	rows := []string{text}
	if render.Cells(lead)+render.Cells(text) > width || strings.Contains(text, "\n") {
		rows = render.Wrap(text, max(width-render.Cells(lead), 1))
	}
	for at, row := range rows {
		rows[at] = indent + paint(row)
	}
	rows[0] = lead + strings.TrimPrefix(rows[0], indent)
	return rows
}

func beside(lines []string, indent, tail string, width int, paint func(string) string) []string {
	last := len(lines) - 1
	switch {
	case tail == "":
		return lines
	case render.Cells(lines[last])+render.Cells(besideGap+tail) <= width:
		lines[last] += paint(besideGap + tail)
		return lines
	}
	return append(lines, hang(indent, tail, width, paint)...)
}

func option(lead, label, tail string, width int, paint func(string) string) []string {
	return beside(hang(lead, label, width, ink), strings.Repeat(" ", render.Cells(lead)), tail, width, paint)
}

func labelOf(q loop.PersonQuestion, at int) string {
	if q.Recommended != nil && *q.Recommended == at {
		return q.Options[at].Label + " (recommended)"
	}
	return q.Options[at].Label
}

func (f *Form) row(at int, label, tail string, width int, paint func(string) string) []string {
	q := f.questions[f.at]
	mark, box := restMark, ""
	if at == f.focus[f.at] {
		mark = look.Accent(focusMark)
	}
	if q.Type == loop.QuestionMulti && at < len(q.Options) {
		box = unpicked
		if f.picked[f.at][at] {
			box = pickedMark
		}
	}
	return option(mark+box+strconv.Itoa(at+1)+". ", label, tail, width, paint)
}

func (f *Form) columns(width int) []string {
	q := f.questions[f.at]
	cells, tallest := make([][]string, len(q.Options)), 0
	for at, o := range q.Options {
		cells[at] = f.row(at, labelOf(q, at), "", width, look.Muted)
		for _, row := range previewRows(o.Preview, width-render.Cells(restMark)) {
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
			parts[at] = render.Pad(text, width)
		}
		rows[row] = strings.Join(parts, look.Faint(columnGap))
	}
	return rows
}

func previewRows(preview string, width int) []string {
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
		for _, piece := range strings.Split(ansi.Hardwrap(strings.ReplaceAll(row, "\t", "    "), max(width, 1), true), "\n") {
			rows = append(rows, look.Style(colour).Render(piece))
		}
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
		return "answer whenever: the lead went on without you and still hears your answer"
	}
	return "answer whenever: the lead keeps working, and in " + render.Until(left) + " hears you have not answered yet"
}
