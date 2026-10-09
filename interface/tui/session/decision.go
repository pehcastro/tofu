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
	"tofu/internal/host"
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

type Verdict = host.Verdict

const (
	Allow = host.Allow
	Ask   = host.Ask
	Deny  = host.Deny
)

type Answer = host.GateAnswer

type Reason = host.Reason

type Decision = host.Decision

func verdictStyle(v Verdict) lipgloss.Style {
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

func (e *Entry) verdictShown() string {
	if e.Decision == nil || e.Decision.Verdict == Allow || !e.asked && !e.Decision.Enforced {
		return ""
	}
	return e.Decision.Verdict.String()
}

func decisionLines(d Decision, width int) []string {
	if d.Verdict == Allow {
		return nil
	}
	body := max(width-widget.Cells(continuation), 1)
	label := widget.Column(d.Answers, answerLabel, 0)
	bars := min(barColumns, body-label-valueColumns-2*widget.Cells(gap))
	var lines []string
	for _, answer := range d.Answers {
		row := widget.Pad(answerLabel(answer), label) + gap + widget.Lead(number(answer.Value), valueColumns)
		if bars > 0 {
			row += gap + widget.Bar(answerFraction(answer), bars)
		}
		lines = append(lines, look.Muted(continuation+widget.Fit(row, body)))
	}
	for _, sentence := range sentences(d) {
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
		if entry := m.entries[index]; entry.Decision != nil && entry.asking {
			return entry, true
		}
	}
	return Entry{}, false
}

func (m *Model) AsksWhereToOverride() bool {
	entry, open := m.openAsk()
	return open && entry.Decision.OverridesRule != ""
}

func (m *Model) AsksToRemember() (string, Decision, bool) {
	entry, open := m.openAsk()
	if !open || entry.Decision.Remembers == "" {
		return "", Decision{}, false
	}
	return entry.askID, *entry.Decision, true
}

type AskChoice struct {
	Label   string
	Answer  host.Answer
	Cancels bool
}

func (m *Model) AskChoices() []AskChoice {
	cancel := AskChoice{Label: "cancel", Cancels: true}
	if m.AsksWhereToOverride() {
		return []AskChoice{{Label: "this project", Answer: host.AllowedOnce}, {Label: "everywhere", Answer: host.AlwaysHere}, {Label: "no", Answer: host.Denied}, cancel}
	}
	return []AskChoice{{Label: "allow once", Answer: host.AllowedOnce}, {Label: "deny", Answer: host.Denied},
		{Label: "always here", Answer: host.AlwaysHere}, {Label: "never here", Answer: host.NeverHere}, cancel}
}

func (m *Model) askLines() []string {
	entry, open := m.openAsk()
	if !open || entry.Decision.Remembers != "" {
		return nil
	}
	head := askMarker + entry.Decision.Tool + wantsWord + entry.Body
	if tripped := tripped(*entry.Decision); tripped != "" {
		head += askGap + tripped
	}
	if overriding := entry.Decision.OverridesRule; overriding != "" {
		head = askMarker + "override " + overriding + "?"
	}
	id := ""
	if short := trace.Short(entry.ID); short != "" {
		id = askGap + look.TypedID(toolKind, short)
	}
	keys := ""
	for at, choice := range m.AskChoices() {
		keys += look.DialogChoice(false, "["+strconv.Itoa(at+1)+"] "+choice.Label)
	}
	inner := m.width - 2*askPadX
	typing := look.Faint(stillTakesTyping)
	if widget.Cells(keys+askGap+stillTakesTyping) > inner {
		typing = ""
	}
	block := spread(look.Style(look.Amber).Render(head), id, inner) + "\n" + spread(keys, typing, inner)
	return strings.Split(look.Surface(m.width, askBlockRows, look.Panel, askPadX, block), "\n")
}

func spread(left, right string, width int) string {
	room := max(width-widget.Cells(right), 1)
	return widget.Pad(widget.Fit(left, room), room) + right
}

func sentences(d Decision) []string {
	if d.Failure != "" {
		return []string{"the gate could not answer, so the call is an ask: " + d.Failure}
	}
	var out []string
	if d.Reason.Question != "" {
		out = append(out, threshold(d))
	}
	if d.Reason.RelaxedBy != "" {
		out = append(out, "relaxed one step by "+d.Reason.RelaxedBy)
	}
	if d.Reason.Blocked {
		out = append(out, "not relaxed, because the call carries untrusted content")
	}
	return out
}

func tripped(d Decision) string {
	if d.Reason.Question == "" || d.Failure != "" {
		if reasons := sentences(d); len(reasons) > 0 {
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

func threshold(d Decision) string {
	word := reasonWord(d.Reason)
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
	return d.Reason.Question + " is " + word + worded + ", so the call is " + verdictOutcome(d.Verdict)
}

func verdictOutcome(v Verdict) string {
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

func reasonWord(r Reason) string {
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

func answerLabel(a Answer) string {
	if a.Choice == "" {
		return a.Question
	}
	return a.Question + " " + a.Choice
}

func answerFraction(a Answer) float64 {
	if a.Max <= 0 {
		return 0
	}
	return a.Value / a.Max
}

func number(value float64) string {
	return strconv.FormatFloat(value, 'f', 2, 64)
}
