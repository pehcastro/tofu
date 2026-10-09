package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"tofu/internal/judge/ledger"
	"tofu/internal/konst"
	"tofu/internal/llm"
	"tofu/internal/subagent"
	"tofu/internal/turn"
)

type askOutcome string

const (
	askSubmitted   askOutcome = "submitted"
	askCancelled   askOutcome = "cancelled"
	askTimedOut    askOutcome = "timed_out"
	askUndelivered askOutcome = "undelivered"
)

type askStatus string

const (
	askAnswered   askStatus = "answered"
	askSkipped    askStatus = "skipped"
	askUnanswered askStatus = "unanswered"
)

const autoSelected = "the recommended option, auto-selected after timeout"

const askedAndWorking = "asked; the answer comes as a message. keep working on what does not depend on it, and take the recommended option where it does until the answer arrives."

const nothingTyped = "nothing was typed, so this one is yours to decide"

const takeTheRecommended = "on any outcome but submitted, take each recommended option, say which you took, and continue. " +
	"on submitted, follow what was chosen; a skipped question is yours to decide. a later answer from the person arrives as a message: correct course if it differs."

type askAnswer struct {
	ID     string    `json:"id"`
	Status askStatus `json:"status"`
	Chosen []string  `json:"chosen"`
	Text   string    `json:"text,omitempty"`
	Note   string    `json:"note,omitempty"`
}

type askResult struct {
	Outcome askOutcome  `json:"outcome"`
	Answers []askAnswer `json:"answers"`
	Next    string      `json:"next"`
}

type personReply struct {
	replies []turn.PersonReply
	err     error
}

type AskPerson struct {
	Person turn.Person
	Auto   func() bool
	Wait   time.Duration
	Judge  *subagent.AskJudge
	Inbox  *turn.Inbox
}

func (AskPerson) Name() string { return turn.AskPersonToolName }

func (AskPerson) Definition() llm.Tool {
	option := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"label":       map[string]any{"type": "string", "description": "1 to " + strconv.Itoa(konst.AskPersonLabelWordsMost) + " words"},
			"description": map[string]any{"type": "string", "description": "what taking it means, in one line"},
			"preview":     map[string]any{"type": "string", "description": "optional: a short block, such as code or a layout, shown when the option is focused"},
		},
		"required": []string{"label", "description"},
	}
	question := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id":          map[string]any{"type": "string"},
			"header":      map[string]any{"type": "string", "description": "at most " + strconv.Itoa(konst.AskPersonHeaderRunes) + " characters"},
			"question":    map[string]any{"type": "string", "description": "one sentence"},
			"type":        map[string]any{"type": "string", "enum": []string{string(turn.QuestionChoice), string(turn.QuestionMulti), string(turn.QuestionText), string(turn.QuestionYesNo)}},
			"options":     map[string]any{"type": "array", "items": option, "minItems": konst.AskPersonOptionsLeast, "maxItems": konst.AskPersonOptionsMost, "description": "for choice and multi; yesno is yes then no; text has none"},
			"recommended": map[string]any{"type": "integer", "description": "the index of the option you would take; required for every type but text"},
		},
		"required": []string{"id", "header", "question", "type"},
	}
	return llm.Tool{
		Name: turn.AskPersonToolName,
		Description: "puts a decision only the person can make to them: a choice between libraries or designs, a matter of taste, spending money, something that cannot be undone. " +
			"never for permission to run a tool; the gate asks that. one call at a time, 1 to " + strconv.Itoa(konst.AskPersonQuestionsMost) + " questions, each with options and the one you recommend. " +
			"tofu adds a free-text other to every choice, so never write one. the answer says the outcome and, for each question, its status and what was chosen. " +
			"in auto the work never stops on it: with no answer in time the recommended option is taken",
		Parameters: map[string]any{
			"type":       "object",
			"properties": map[string]any{"questions": map[string]any{"type": "array", "items": question, "minItems": 1, "maxItems": konst.AskPersonQuestionsMost}},
			"required":   []string{"questions"},
		},
	}
}

func (a AskPerson) Run(ctx context.Context, raw json.RawMessage) (turn.Result, error) {
	var args struct {
		Questions []turn.PersonQuestion `json:"questions"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return turn.Result{}, fmt.Errorf("ask_person: arguments are not the expected shape: %w", err)
	}
	if err := checkQuestions(args.Questions); err != nil {
		return turn.Result{}, fmt.Errorf("ask_person: %w", err)
	}
	questions := args.Questions
	for i := range questions {
		questions[i].Options = optionsOf(questions[i])
	}
	recorded := make(chan []string, 1)
	go func() { recorded <- a.recordShadow(context.WithoutCancel(ctx), questions) }()
	auto := a.Auto()
	if blocks, decided := turn.QuestionsBlockFrom(ctx); decided {
		auto = !blocks
	}
	replied := make(chan personReply, 1)
	go func() { replied <- a.reply(ctx, raw, questions, auto) }()
	if !auto {
		reply := answered(<-replied, questions)
		a.backfill(<-recorded, reply.Outcome)
		shown, err := json.Marshal(reply)
		return turn.Result{Content: string(shown)}, err
	}
	go a.answerLater(a.Inbox.Open(pollOf(questions)), replied, recorded, questions)
	return turn.Result{Content: askedAndWorking}, nil
}

func (a AskPerson) reply(ctx context.Context, raw json.RawMessage, questions []turn.PersonQuestion, auto bool) personReply {
	if form := turn.PersonFormFrom(ctx); form != nil {
		wait := time.Duration(0)
		if auto {
			wait = a.Wait
		}
		replies, err := form(ctx, questions, wait)
		return personReply{replies, err}
	}
	if a.Person == nil {
		return personReply{err: turn.QuestionUndelivered{Why: "no person can answer here"}}
	}
	answer, err := a.Person(ctx, turn.GateRequest{Tool: turn.AskPersonToolName, Args: raw}, turn.GateDecision{Verdict: ledger.VerdictAsk})
	if err != nil {
		return personReply{err: err}
	}
	accepted := answer == turn.PersonAllowedOnce || answer == turn.PersonAlwaysHere
	var replies []turn.PersonReply
	for _, q := range questions {
		switch {
		case accepted && q.Type == turn.QuestionText:
		case accepted:
			replies = append(replies, turn.PersonReply{ID: q.ID, Chosen: recommendedOf(q)})
		default:
			replies = append(replies, turn.PersonReply{ID: q.ID})
		}
	}
	return personReply{replies: replies}
}

func (a AskPerson) answerLater(open *turn.Asked, replied <-chan personReply, recorded <-chan []string, questions []turn.PersonQuestion) {
	defer open.Close()
	var first askResult
	stopped := false
	select {
	case reply := <-replied:
		first, stopped = answered(reply, questions), errors.Is(reply.err, context.Canceled)
	case <-time.After(a.Wait):
		first = resolved(askTimedOut, questions, autoSelected)
	}
	a.backfill(<-recorded, first.Outcome)
	if stopped {
		return
	}
	shown, _ := json.Marshal(first)
	open.Answer("the answer to your " + turn.AskPersonToolName + " question: " + string(shown))
	if first.Outcome != askTimedOut {
		return
	}
	if later := answered(<-replied, questions); later.Outcome == askSubmitted {
		shown, _ := json.Marshal(later)
		open.Answer("the person answered your " + turn.AskPersonToolName + " question after the timeout, so correct course where it differs from the option you took: " + string(shown))
	}
}

func (a AskPerson) backfill(ids []string, outcome askOutcome) {
	for _, id := range ids {
		_ = a.Judge.Answered(id, string(outcome))
	}
}

func pollOf(questions []turn.PersonQuestion) string {
	polls := make([]string, len(questions))
	for i, q := range questions {
		polls[i] = q.Header + ": " + q.Question
		for at, o := range q.Options {
			polls[i] += "\n  " + strconv.Itoa(at+1) + ". " + o.Label
			if q.Recommended != nil && *q.Recommended == at {
				polls[i] += " (recommended)"
			}
		}
	}
	return strings.Join(polls, "\n")
}

func answered(reply personReply, questions []turn.PersonQuestion) askResult {
	switch {
	case errors.Is(reply.err, context.Canceled):
		return resolved(askCancelled, questions, "the question was withdrawn when the turn stopped")
	case errors.As(reply.err, &turn.QuestionDismissed{}):
		return resolved(askCancelled, questions, "the person dismissed the question, so the recommended option stands")
	case reply.err != nil:
		return resolved(askUndelivered, questions, "the person could not be asked: "+reply.err.Error())
	}
	result := askResult{Outcome: askSubmitted, Next: takeTheRecommended}
	for _, q := range questions {
		answer := askAnswer{ID: q.ID, Status: askUnanswered, Chosen: []string{}, Note: nothingTyped}
		if at := slices.IndexFunc(reply.replies, func(r turn.PersonReply) bool { return r.ID == q.ID }); at >= 0 {
			given := reply.replies[at]
			answer.Status, answer.Note, answer.Text = askSkipped, "the person chose no option and typed nothing", given.Text
			if len(given.Chosen) > 0 || given.Text != "" {
				answer.Status, answer.Note = askAnswered, ""
				answer.Chosen = append(answer.Chosen, given.Chosen...)
			}
		}
		result.Answers = append(result.Answers, answer)
	}
	return result
}

func resolved(outcome askOutcome, questions []turn.PersonQuestion, why string) askResult {
	result := askResult{Outcome: outcome, Next: takeTheRecommended}
	for _, q := range questions {
		answer := askAnswer{ID: q.ID, Status: askUnanswered, Chosen: recommendedOf(q), Note: why}
		if q.Type == turn.QuestionText {
			answer.Note = nothingTyped
		}
		result.Answers = append(result.Answers, answer)
	}
	return result
}

func (a AskPerson) recordShadow(ctx context.Context, questions []turn.PersonQuestion) []string {
	if a.Judge == nil {
		return nil
	}
	var ids []string
	for _, q := range questions {
		state := subagent.AskState{Asker: subagent.AskerLead, Question: q.Question, Options: []subagent.AskOption{}}
		if q.Recommended != nil {
			state.Recommended = q.Options[*q.Recommended].Label
		}
		for _, o := range q.Options {
			state.Options = append(state.Options, subagent.AskOption{Label: o.Label, Description: o.Description})
		}
		if id, err := a.Judge.Record(ctx, state); err == nil {
			ids = append(ids, id)
		}
	}
	return ids
}

func optionsOf(q turn.PersonQuestion) []turn.PersonOption {
	if q.Type == turn.QuestionYesNo {
		return []turn.PersonOption{{Label: "yes"}, {Label: "no"}}
	}
	return q.Options
}

func recommendedOf(q turn.PersonQuestion) []string {
	if q.Recommended == nil {
		return []string{}
	}
	return []string{optionsOf(q)[*q.Recommended].Label}
}

func checkQuestions(questions []turn.PersonQuestion) error {
	if len(questions) < 1 || len(questions) > konst.AskPersonQuestionsMost {
		return fmt.Errorf("asks %d questions, and a call asks 1 to %d", len(questions), konst.AskPersonQuestionsMost)
	}
	var ids []string
	for _, q := range questions {
		switch {
		case strings.TrimSpace(q.ID) == "":
			return errors.New("a question has no id")
		case slices.Contains(ids, q.ID):
			return fmt.Errorf("the id %q is used twice", q.ID)
		case utf8.RuneCountInString(q.Header) > konst.AskPersonHeaderRunes:
			return fmt.Errorf("%s: the header %q is over %d characters", q.ID, q.Header, konst.AskPersonHeaderRunes)
		}
		ids = append(ids, q.ID)
		if err := checkOptions(q); err != nil {
			return fmt.Errorf("%s: %w", q.ID, err)
		}
	}
	return nil
}

func checkOptions(q turn.PersonQuestion) error {
	switch q.Type {
	case turn.QuestionText:
		if len(q.Options) > 0 || q.Recommended != nil {
			return errors.New("a text question takes no options and no recommended")
		}
		return nil
	case turn.QuestionYesNo:
		if len(q.Options) > 0 {
			return errors.New("a yesno question is yes then no, so it takes no options")
		}
	case turn.QuestionChoice, turn.QuestionMulti:
		if len(q.Options) < konst.AskPersonOptionsLeast || len(q.Options) > konst.AskPersonOptionsMost {
			return fmt.Errorf("offers %d options, and a choice offers %d to %d", len(q.Options), konst.AskPersonOptionsLeast, konst.AskPersonOptionsMost)
		}
	default:
		return fmt.Errorf("the type %q is not choice, multi, text or yesno", q.Type)
	}
	for _, o := range q.Options {
		words := len(strings.Fields(o.Label))
		switch {
		case words < 1 || words > konst.AskPersonLabelWordsMost:
			return fmt.Errorf("the label %q is not 1 to %d words", o.Label, konst.AskPersonLabelWordsMost)
		case strings.EqualFold(strings.TrimSpace(o.Label), "other"):
			return errors.New("tofu adds other to every choice, so the options never name one")
		}
	}
	if q.Recommended == nil || *q.Recommended < 0 || *q.Recommended >= len(optionsOf(q)) {
		return errors.New("recommended is the index of one of the options, and every question but text names one")
	}
	return nil
}
