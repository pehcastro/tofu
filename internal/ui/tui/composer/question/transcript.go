package question

import (
	"encoding/json"
	"slices"
	"strings"

	"tofu/internal/agent/loop"
	"tofu/internal/cli/render"
	"tofu/internal/ui/tui/look/look"
)

const (
	waitingLine     = "waiting for your answer in the form below"
	contextLabel    = " Question context "
	cardEdge        = "│ "
	outcomeMark     = `{"outcome"`
	answerElsewhere = `{"outcome":"answered"}`
)

type askCall struct {
	Questions []loop.PersonQuestion `json:"questions"`
}

type given struct {
	ID     string   `json:"id"`
	Chosen []string `json:"chosen"`
	Text   string   `json:"text"`
	Note   string   `json:"note"`
}

type said struct {
	Outcome string  `json:"outcome"`
	Answers []given `json:"answers"`
}

func idsOf(questions []loop.PersonQuestion) string {
	ids := make([]string, len(questions))
	for at, q := range questions {
		ids[at] = q.ID
	}
	return strings.Join(ids, ",")
}

func (f *Form) Said(action Action) (string, string) {
	reply := said{Outcome: "cancelled"}
	if action == Submitted {
		reply.Outcome = "submitted"
		for _, r := range f.Replies() {
			reply.Answers = append(reply.Answers, given{ID: r.ID, Chosen: r.Chosen, Text: r.Text})
		}
	}
	shown, _ := json.Marshal(reply)
	return idsOf(f.questions), string(shown)
}

func Call(questions []loop.PersonQuestion) (string, string) {
	args, _ := json.Marshal(askCall{Questions: questions})
	return string(args), idsOf(questions)
}

func Settled(answer string) bool { return strings.Contains(answer, outcomeMark) }

func Asks(args, ids string) bool {
	var call askCall
	return json.Unmarshal([]byte(args), &call) == nil && len(call.Questions) > 0 && idsOf(call.Questions) == ids
}

func Transcript(args, answer string, width int) []string {
	var call askCall
	_ = json.Unmarshal([]byte(args), &call)
	line, settled, answered := strings.Cut(answer, outcomeMark)
	switch line = strings.TrimSpace(line); {
	case answered && line != "":
		return hang(amber("? "), line, width, look.Accent)
	case answered:
		return outcomes(call.Questions, outcomeMark+settled, width)
	}
	var lines []string
	for _, q := range call.Questions {
		lines = append(lines, hang(amber("? "), q.Header, width, amber)...)
		lines = append(lines, hang("", q.Question, width, look.Title)...)
		if q.Context != "" {
			lines = append(lines, contextCard(q.Context, width)...)
		}
		lines = append(lines, hang("", waitingLine, width, look.Muted)...)
	}
	return lines
}

func outcomes(questions []loop.PersonQuestion, settled string, width int) []string {
	var reply said
	_ = json.Unmarshal([]byte(settled), &reply)
	var lines []string
	for _, q := range questions {
		var mine given
		if at := slices.IndexFunc(reply.Answers, func(g given) bool { return g.ID == q.ID }); at >= 0 {
			mine = reply.Answers[at]
		}
		state, paint := outcomeLine(reply.Outcome, mine)
		lines = append(lines, hang(amber("? "), q.Header+": "+state, width, paint)...)
	}
	return lines
}

func Answered(line, said string) string {
	settled := answerElsewhere
	if at := strings.Index(said, outcomeMark); at >= 0 {
		settled = said[at:]
	}
	return line + "\n" + settled
}

func contextCard(text string, width int) []string {
	inner := max(width-2*render.Cells(cardEdge), 1)
	lines := []string{look.Faint("╭─") + look.Style(look.Blue).Render(contextLabel) + look.Faint(strings.Repeat("─", max(inner-render.Cells(contextLabel), 0))+"─╮")}
	for _, row := range render.Wrap(text, inner) {
		lines = append(lines, look.Faint(cardEdge)+ink(render.Pad(row, inner))+look.Faint(" │"))
	}
	return append(lines, look.Faint("╰"+strings.Repeat("─", inner+2)+"╯"))
}

func outcomeLine(outcome string, mine given) (string, func(string) string) {
	chosen, typed := strings.Join(mine.Chosen, ", "), strings.TrimSpace(mine.Text+" "+mine.Note)
	switch outcome {
	case "submitted":
		switch {
		case chosen != "" && typed != "":
			return "you chose " + chosen + ", and wrote: " + typed, look.Accent
		case chosen != "":
			return "you chose " + chosen, look.Accent
		case typed != "":
			return "you wrote: " + typed, look.Accent
		}
		return "you skipped it: nothing chosen", amber
	case "taken_in_auto":
		return "taken without asking you: " + chosen, look.Accent
	case "cancelled":
		return "you closed it: nothing chosen, and the question stays open", amber
	case "timed_out":
		return "no answer yet: nothing chosen, and the question stays open", amber
	case "undelivered":
		return "it could not be put to you: nothing chosen", amber
	}
	return "answered as " + outcome, amber
}
