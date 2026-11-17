package question

import (
	"encoding/json"
	"slices"
	"strconv"
	"strings"

	"tofu/internal/agent/loop"
	"tofu/internal/ui/tui/look/look"
)

const (
	chosenMark  = "✓ "
	waitingLine = "waiting for your answer in the form below"
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

func Asks(args, ids string) bool {
	var call askCall
	return json.Unmarshal([]byte(args), &call) == nil && len(call.Questions) > 0 && idsOf(call.Questions) == ids
}

func Transcript(args, answer string, width int) []string {
	var call askCall
	_ = json.Unmarshal([]byte(args), &call)
	var reply said
	if at := strings.Index(answer, `{"outcome"`); at >= 0 {
		_ = json.Unmarshal([]byte(answer[at:]), &reply)
	}
	var lines []string
	for _, q := range call.Questions {
		var mine given
		if at := slices.IndexFunc(reply.Answers, func(g given) bool { return g.ID == q.ID }); at >= 0 {
			mine = reply.Answers[at]
		}
		lines = append(lines, hang(amber("? "), q.Header, width, amber)...)
		lines = append(lines, hang("", q.Question, width, look.Title)...)
		if q.Context != "" {
			lines = append(lines, hang("", q.Context, width, ink)...)
		}
		for at, o := range q.Options {
			mark := restMark
			if slices.Contains(mine.Chosen, o.Label) {
				mark = look.Accent(chosenMark)
			}
			lines = append(lines, option(mark+strconv.Itoa(at+1)+". ", labelOf(q, at), o.Description, width, look.Muted)...)
		}
		state, paint := outcomeLine(reply.Outcome, mine)
		lines = append(lines, hang("", state, width, paint)...)
	}
	return lines
}

func outcomeLine(outcome string, mine given) (string, func(string) string) {
	chosen, typed := strings.Join(mine.Chosen, ", "), strings.TrimSpace(mine.Text+" "+mine.Note)
	switch outcome {
	case "":
		return waitingLine, look.Muted
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
