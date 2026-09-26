package session

import (
	"math"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"charm.land/lipgloss/v2"

	"tofu/interface/tui/look"
	"tofu/interface/tui/trace"
	"tofu/internal/widget"
)

const (
	barColumns       = 20
	valueColumns     = 4
	gap              = "  "
	askGap           = "   "
	askPadX          = 2
	askMarker        = "? "
	wantsWord        = " wants "
	stillTakesTyping = "the chat still takes what you type"
	askBlockRows     = 2
)

type Verdict int

const (
	Allow Verdict = iota
	Ask
	Deny
)

func (v Verdict) String() string {
	switch v {
	case Allow:
		return "allow"
	case Ask:
		return "ask"
	case Deny:
		return "deny"
	}
	panic("session: unknown verdict")
}

func (v Verdict) style() lipgloss.Style {
	switch v {
	case Allow:
		return look.Style(look.MutedColor)
	case Ask:
		return look.Style(look.Amber)
	case Deny:
		return look.Style(look.Red)
	}
	panic("session: unknown verdict")
}

type Answer struct {
	Question string
	Choice   string
	Value    float64
	Max      float64
}

type Reason struct {
	Question  string
	Limit     string
	Levels    []string
	Threshold float64
	Value     float64
	DeadBand  bool
	RelaxedBy string
	Blocked   bool
}

type Decision struct {
	Tool     string
	Verdict  Verdict
	Answers  []Answer
	Reason   Reason
	Failure  string
	Awaiting bool
}

func (d Decision) lines(width int) []string {
	if d.Verdict == Allow {
		return nil
	}
	body := max(width-widget.Cells(continuation), 1)
	label := widget.Column(d.Answers, Answer.label, 0)
	bars := min(barColumns, body-label-valueColumns-2*widget.Cells(gap))
	var lines []string
	for _, answer := range d.Answers {
		row := widget.Pad(answer.label(), label) + gap + widget.Lead(number(answer.Value), valueColumns)
		if bars > 0 {
			row += gap + widget.Bar(answer.fraction(), bars)
		}
		lines = append(lines, look.Muted(continuation+widget.Fit(row, body)))
	}
	for _, sentence := range d.sentences() {
		for _, line := range widget.Wrap(sentence, body) {
			lines = append(lines, look.Faint(continuation+line))
		}
	}
	return lines
}

func (m *Model) openAsk() (Entry, bool) {
	if !m.Awaiting() {
		return Entry{}, false
	}
	for index := len(m.entries) - 1; index >= 0; index-- {
		if entry := m.entries[index]; entry.Decision != nil && entry.Decision.Awaiting {
			return entry, true
		}
	}
	return Entry{}, false
}

func (m *Model) askLines() []string {
	entry, open := m.openAsk()
	if !open {
		return nil
	}
	head := askMarker + entry.Decision.Tool + wantsWord + entry.Body
	if tripped := entry.Decision.tripped(); tripped != "" {
		head += askGap + tripped
	}
	id := ""
	if short := trace.Short(entry.ID); short != "" {
		id = askGap + look.TypedID(toolKind, short)
	}
	keys := ""
	for _, label := range [...]string{"[1] allow once", "[2] deny", "[3] always here"} {
		keys += look.DialogChoice(false, label)
	}
	inner := m.width - 2*askPadX
	block := spread(look.Style(look.Amber).Render(head), id, inner) + "\n" + spread(keys, look.Faint(stillTakesTyping), inner)
	return strings.Split(look.Surface(m.width, askBlockRows, look.Panel, askPadX, block), "\n")
}

func spread(left, right string, width int) string {
	room := max(width-widget.Cells(right), 1)
	return widget.Pad(widget.Fit(left, room), room) + right
}

func (d Decision) sentences() []string {
	if d.Failure != "" {
		return []string{"the gate could not answer, so the call is an ask: " + d.Failure}
	}
	var out []string
	if d.Reason.Question != "" {
		out = append(out, d.threshold())
	}
	if d.Reason.RelaxedBy != "" {
		out = append(out, "relaxed one step by "+d.Reason.RelaxedBy)
	}
	if d.Reason.Blocked {
		out = append(out, "not relaxed, because the call carries untrusted content")
	}
	return out
}

func (d Decision) tripped() string {
	if d.Reason.Question == "" || d.Failure != "" {
		if reasons := d.sentences(); len(reasons) > 0 {
			return reasons[0]
		}
		return ""
	}
	crossed := "over"
	switch {
	case d.Reason.DeadBand:
		crossed = "in the dead band at"
	case d.Reason.Value <= d.Reason.Threshold:
		crossed = "under"
	}
	return d.Reason.Question + " " + number(d.Reason.Value) + " " + crossed + " " + number(d.Reason.Threshold)
}

func (d Decision) threshold() string {
	word := d.Reason.word()
	numeric, worded := "is over", ""
	switch {
	case d.Reason.DeadBand:
		numeric, worded = "is within the dead band of", ", inside the dead band"
	case d.Reason.Value <= d.Reason.Threshold:
		numeric, worded = "is under", ", under the line"
	}
	if word == "" {
		return d.Reason.Question + " " + number(d.Reason.Value) + " " + numeric + " " +
			d.Reason.Limit + " " + number(d.Reason.Threshold)
	}
	return d.Reason.Question + " is " + word + worded + ", so the call is " + d.Verdict.outcome()
}

func (v Verdict) outcome() string {
	switch v {
	case Allow:
		return "allowed"
	case Ask:
		return "asked about"
	case Deny:
		return "refused"
	}
	panic("session: unknown verdict")
}

func (r Reason) word() string {
	level := int(math.Round(r.Value))
	if len(r.Levels) < 2 || level < 0 || level >= len(r.Levels) {
		return ""
	}
	head := r.Levels[level]
	if stop := strings.IndexAny(head, ":."); stop >= 0 {
		head = head[:stop]
	}
	head = strings.TrimSpace(head)
	if head == "" {
		return ""
	}
	first, size := utf8.DecodeRuneInString(head)
	return string(unicode.ToLower(first)) + head[size:]
}

func (a Answer) label() string {
	if a.Choice == "" {
		return a.Question
	}
	return a.Question + " " + a.Choice
}

func (a Answer) fraction() float64 {
	if a.Max <= 0 {
		return 0
	}
	return a.Value / a.Max
}

func number(value float64) string {
	return strconv.FormatFloat(value, 'f', 2, 64)
}
