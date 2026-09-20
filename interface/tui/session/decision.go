package session

import (
	"strconv"

	"charm.land/lipgloss/v2"

	"tofu/interface/tui/theme"
	"tofu/internal/widget"
)

const (
	barColumns   = 20
	valueColumns = 4
	gap          = "  "
	answerKeys   = "[a] allow once   [d] deny   [A] always here"
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
		return theme.Dim()
	case Ask:
		return theme.Warn()
	case Deny:
		return theme.Fail()
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
	label := 0
	for _, answer := range d.Answers {
		label = max(label, widget.Cells(answer.label()))
	}
	bars := min(barColumns, body-label-valueColumns-2*widget.Cells(gap))
	var lines []string
	for _, answer := range d.Answers {
		row := widget.Pad(answer.label(), label) + gap + widget.Lead(number(answer.Value), valueColumns)
		if bars > 0 {
			row += gap + widget.Bar(answer.fraction(), bars)
		}
		lines = append(lines, theme.Dim().Render(continuation+widget.Fit(row, body)))
	}
	for _, sentence := range d.sentences() {
		for _, line := range widget.Wrap(sentence, body) {
			lines = append(lines, theme.Faint().Render(continuation+line))
		}
	}
	if d.Awaiting {
		lines = append(lines, theme.Warn().Render(continuation+widget.Fit(answerKeys, body)))
	}
	return lines
}

func (d Decision) sentences() []string {
	if d.Failure != "" {
		return []string{"the gate could not answer, so the call is an ask: " + d.Failure}
	}
	var out []string
	if d.Reason.Question != "" {
		out = append(out, d.Reason.Question+" "+number(d.Reason.Value)+" "+d.standing()+" "+
			d.Reason.Limit+" "+number(d.Reason.Threshold))
	}
	if d.Reason.RelaxedBy != "" {
		out = append(out, "relaxed one step by "+d.Reason.RelaxedBy)
	}
	if d.Reason.Blocked {
		out = append(out, "not relaxed, because the call carries untrusted content")
	}
	return out
}

func (d Decision) standing() string {
	switch {
	case d.Reason.DeadBand:
		return "is within the dead band of"
	case d.Reason.Value > d.Reason.Threshold:
		return "is over"
	}
	return "is under"
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
