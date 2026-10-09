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

type askType string

const (
	askChoice askType = "choice"
	askMulti  askType = "multi"
	askText   askType = "text"
	askYesNo  askType = "yesno"
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

type askOption struct {
	Label       string `json:"label"`
	Description string `json:"description"`
	Preview     string `json:"preview,omitempty"`
}

type askQuestion struct {
	ID          string      `json:"id"`
	Header      string      `json:"header"`
	Question    string      `json:"question"`
	Type        askType     `json:"type"`
	Options     []askOption `json:"options,omitempty"`
	Recommended *int        `json:"recommended,omitempty"`
}

type askAnswer struct {
	ID     string    `json:"id"`
	Status askStatus `json:"status"`
	Chosen []string  `json:"chosen"`
	Note   string    `json:"note,omitempty"`
}

type askResult struct {
	Outcome askOutcome  `json:"outcome"`
	Answers []askAnswer `json:"answers"`
	Next    string      `json:"next"`
}

type personReply struct {
	answer turn.PersonAnswer
	err    error
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
			"type":        map[string]any{"type": "string", "enum": []string{string(askChoice), string(askMulti), string(askText), string(askYesNo)}},
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
		Questions []askQuestion `json:"questions"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return turn.Result{}, fmt.Errorf("ask_person: arguments are not the expected shape: %w", err)
	}
	if err := checkQuestions(args.Questions); err != nil {
		return turn.Result{}, fmt.Errorf("ask_person: %w", err)
	}
	recorded := make(chan []string, 1)
	go func() { recorded <- a.recordShadow(context.WithoutCancel(ctx), args.Questions) }()
	replied := make(chan personReply, 1)
	go func() {
		if a.Person == nil {
			replied <- personReply{err: errors.New("no person can answer here")}
			return
		}
		answer, err := a.Person(ctx, turn.GateRequest{Tool: turn.AskPersonToolName, Args: raw}, turn.GateDecision{Verdict: ledger.VerdictAsk})
		replied <- personReply{answer, err}
	}()
	if !a.Auto() {
		reply := answered(<-replied, args.Questions)
		a.backfill(<-recorded, reply.Outcome)
		shown, err := json.Marshal(reply)
		return turn.Result{Content: string(shown)}, err
	}
	go a.answerLater(a.Inbox.Open(pollOf(args.Questions)), replied, recorded, args.Questions)
	return turn.Result{Content: askedAndWorking}, nil
}

func (a AskPerson) answerLater(open *turn.Asked, replied <-chan personReply, recorded <-chan []string, questions []askQuestion) {
	defer open.Close()
	var first askResult
	select {
	case reply := <-replied:
		first = answered(reply, questions)
	case <-time.After(a.Wait):
		first = resolved(askTimedOut, questions, autoSelected)
	}
	a.backfill(<-recorded, first.Outcome)
	if first.Outcome == askCancelled {
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

func pollOf(questions []askQuestion) string {
	polls := make([]string, len(questions))
	for i, q := range questions {
		polls[i] = q.Header + ": " + q.Question
		for at, o := range optionsOf(q) {
			polls[i] += "\n  " + strconv.Itoa(at+1) + ". " + o.Label
			if q.Recommended != nil && *q.Recommended == at {
				polls[i] += " (recommended)"
			}
		}
	}
	return strings.Join(polls, "\n")
}

func answered(reply personReply, questions []askQuestion) askResult {
	switch {
	case errors.Is(reply.err, context.Canceled):
		return resolved(askCancelled, questions, "the question was withdrawn when the turn stopped")
	case reply.err != nil:
		return resolved(askUndelivered, questions, "the person could not be asked: "+reply.err.Error())
	}
	result := askResult{Outcome: askSubmitted, Next: takeTheRecommended}
	for _, q := range questions {
		answer := askAnswer{ID: q.ID, Status: askSkipped, Chosen: []string{}, Note: "the person declined the recommended option and chose no other"}
		switch {
		case q.Type == askText:
			answer.Status, answer.Note = askUnanswered, nothingTyped
		case reply.answer == turn.PersonAllowedOnce || reply.answer == turn.PersonAlwaysHere:
			answer.Status, answer.Chosen, answer.Note = askAnswered, recommendedOf(q), "the person accepted the recommended option"
		}
		result.Answers = append(result.Answers, answer)
	}
	return result
}

func resolved(outcome askOutcome, questions []askQuestion, why string) askResult {
	result := askResult{Outcome: outcome, Next: takeTheRecommended}
	for _, q := range questions {
		answer := askAnswer{ID: q.ID, Status: askUnanswered, Chosen: recommendedOf(q), Note: why}
		if q.Type == askText {
			answer.Note = nothingTyped
		}
		result.Answers = append(result.Answers, answer)
	}
	return result
}

func (a AskPerson) recordShadow(ctx context.Context, questions []askQuestion) []string {
	if a.Judge == nil {
		return nil
	}
	var ids []string
	for _, q := range questions {
		state := subagent.AskState{Asker: subagent.AskerLead, Question: q.Question, Options: []subagent.AskOption{}}
		if q.Recommended != nil {
			state.Recommended = optionsOf(q)[*q.Recommended].Label
		}
		for _, o := range optionsOf(q) {
			state.Options = append(state.Options, subagent.AskOption{Label: o.Label, Description: o.Description})
		}
		if id, err := a.Judge.Record(ctx, state); err == nil {
			ids = append(ids, id)
		}
	}
	return ids
}

func optionsOf(q askQuestion) []askOption {
	if q.Type == askYesNo {
		return []askOption{{Label: "yes"}, {Label: "no"}}
	}
	return q.Options
}

func recommendedOf(q askQuestion) []string {
	if q.Recommended == nil {
		return []string{}
	}
	return []string{optionsOf(q)[*q.Recommended].Label}
}

func checkQuestions(questions []askQuestion) error {
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

func checkOptions(q askQuestion) error {
	switch q.Type {
	case askText:
		if len(q.Options) > 0 || q.Recommended != nil {
			return errors.New("a text question takes no options and no recommended")
		}
		return nil
	case askYesNo:
		if len(q.Options) > 0 {
			return errors.New("a yesno question is yes then no, so it takes no options")
		}
	case askChoice, askMulti:
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
