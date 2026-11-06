package ask

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"tofu/internal/agent/loop"
	"tofu/internal/agent/subagents"
	"tofu/internal/judge/gate"
	"tofu/internal/judge/ledger"
	"tofu/internal/konst"
	"tofu/internal/models/llm/core"
	"tofu/internal/platform/project"
	"tofu/internal/platform/safe"
	"tofu/internal/store/memory/entries"
	"tofu/internal/tools/contract"
)

type askOutcome string

const (
	askSubmitted   askOutcome = "submitted"
	askCancelled   askOutcome = "cancelled"
	askTimedOut    askOutcome = "timed_out"
	askUndelivered askOutcome = "undelivered"
	askTakenInAuto askOutcome = "taken_in_auto"
)

type askStatus string

const (
	askAnswered   askStatus = "answered"
	askSkipped    askStatus = "skipped"
	askUnanswered askStatus = "unanswered"
)

const askedAndWorking = "asked; the answer comes as a message. keep working on what does not depend on it, and take no action that does until the answer arrives."

const nothingTyped = "nothing was typed, so this one is yours to decide"

const takeTheRecommended = "on any outcome but submitted, take each recommended option, say which you took, and continue. " +
	"on submitted, follow what was chosen; a skipped question is yours to decide."

const noAnswerYet = "no answer came in time, so nothing was chosen and the question stays open: the person can still answer it, and that answer arrives as a message. " +
	"take no action that depends on it and pick no option for them. keep to work that does not depend on it, or end the turn saying it waits on this question."

const noAnswerHead = "no answer yet; nothing was chosen and the question stays open"

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
	replies []loop.PersonReply
	err     error
}

type AskPerson struct {
	Person loop.Person
	Auto   func() bool
	Wait   time.Duration
	Judge  *subagents.AskJudge
	Inbox  *loop.Inbox
	Log    string
}

func (AskPerson) Name() string { return loop.AskPersonToolName }

func (AskPerson) Definition() core.Tool {
	option := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"label":       map[string]any{"type": "string", "description": "1 to " + strconv.Itoa(konst.AskPersonLabelWordsMost) + " words"},
			"description": map[string]any{"type": "string", "description": "what taking it means, in one line"},
			"preview":     map[string]any{"type": "string", "description": "optional: what taking it looks like, as code, a unified diff or a text screen; options that carry one are shown side by side"},
		},
		"required": []string{"label", "description"},
	}
	question := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id":          map[string]any{"type": "string"},
			"header":      map[string]any{"type": "string", "description": "1 to " + strconv.Itoa(konst.AskPersonHeaderWordsMost) + " words"},
			"question":    map[string]any{"type": "string", "description": "one sentence"},
			"type":        map[string]any{"type": "string", "enum": []string{string(loop.QuestionChoice), string(loop.QuestionMulti), string(loop.QuestionText), string(loop.QuestionYesNo)}},
			"options":     map[string]any{"type": "array", "items": option, "minItems": konst.AskPersonOptionsLeast, "maxItems": konst.AskPersonOptionsMost, "description": "for choice and multi; yesno is yes then no; text has none"},
			"recommended": map[string]any{"type": "integer", "description": "the index of the option you would take; required for every type but text"},
			"reversible":  map[string]any{"type": "boolean", "description": "true only when the recommended option can be undone later at little cost; leave it out otherwise"},
		},
		"required": []string{"id", "header", "question", "type"},
	}
	return core.Tool{
		Name: loop.AskPersonToolName,
		Description: "puts a decision only the person can make to them: a choice between libraries or designs, a matter of taste, spending money, something that cannot be undone. " +
			"never for permission to run a tool; the gate asks that. one call at a time, 1 to " + strconv.Itoa(konst.AskPersonQuestionsMost) + " questions, each with options and the one you recommend. " +
			"tofu adds a free-text other to every choice, so never write one. the answer says the outcome and, for each question, its status, what was chosen and any note the person typed with it. " +
			"in auto the work never stops on it: with no answer in time you are told none came, nothing is chosen, and the question stays open until the person answers. " +
			"a reversible recommendation the ask judge agrees with is taken without asking",
		Parameters: map[string]any{
			"type":       "object",
			"properties": map[string]any{"questions": map[string]any{"type": "array", "items": question, "minItems": 1, "maxItems": konst.AskPersonQuestionsMost}},
			"required":   []string{"questions"},
		},
	}
}

func (a AskPerson) Run(ctx context.Context, raw json.RawMessage) (contract.Result, error) {
	var args struct {
		Questions []struct {
			loop.PersonQuestion
			Reversible bool `json:"reversible"`
		} `json:"questions"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return contract.Result{}, fmt.Errorf("ask_person: arguments are not the expected shape: %w", err)
	}
	questions, reversible := make([]loop.PersonQuestion, len(args.Questions)), true
	for i, q := range args.Questions {
		questions[i], reversible = q.PersonQuestion, reversible && q.Reversible && q.Recommended != nil
	}
	if err := checkQuestions(questions); err != nil {
		return contract.Result{}, fmt.Errorf("ask_person: %w", err)
	}
	for i := range questions {
		questions[i].Options = optionsOf(questions[i])
	}
	auto := a.Auto()
	if blocks, decided := loop.QuestionsBlockFrom(ctx); decided {
		auto = !blocks
	}
	recorded, report := make(chan []string, 1), project.Crashes(".")
	if auto && reversible && a.Judge != nil {
		acting := *a.Judge
		acting.Rule.Mode = gate.ModeEnforced
		a.Judge = &acting
		ids := a.recordShadow(context.WithoutCancel(ctx), questions)
		if proceeds(a.Log, ids, len(questions)) {
			taken := resolved(askTakenInAuto, questions, "the ask judge said proceed and the recommended option can be undone, so gatePrompt auto took it without asking the person")
			a.backfill(ids, taken.Outcome)
			shown, err := json.Marshal(taken)
			return contract.Result{Content: string(shown)}, err
		}
		recorded <- ids
	} else {
		safe.Go(report, "ask record", func() {
			var ids []string
			defer func() { recorded <- ids }()
			ids = a.recordShadow(context.WithoutCancel(ctx), questions)
		})
	}
	replied := make(chan personReply, 1)
	safe.Go(report, "ask reply", func() {
		var reply personReply
		if err := safe.Call(report, "ask reply", func() error {
			reply = a.reply(ctx, raw, questions, auto)
			return nil
		}); err != nil {
			reply.err = err
		}
		replied <- reply
	})
	if !auto {
		reply := answered(<-replied, questions)
		a.backfill(<-recorded, reply.Outcome)
		shown, err := json.Marshal(reply)
		return contract.Result{Content: string(shown)}, err
	}
	open := a.Inbox.Open(pollOf(questions))
	safe.Go(report, "ask answer", func() { a.answerLater(open, replied, recorded, questions) })
	return contract.Result{Content: askedAndWorking}, nil
}

func (a AskPerson) reply(ctx context.Context, raw json.RawMessage, questions []loop.PersonQuestion, auto bool) personReply {
	if form := loop.PersonFormFrom(ctx); form != nil {
		wait := time.Duration(0)
		if auto {
			wait = a.Wait
		}
		replies, err := form(ctx, questions, wait)
		return personReply{replies, err}
	}
	if a.Person == nil {
		return personReply{err: loop.QuestionUndelivered{Why: "no person can answer here"}}
	}
	answer, err := a.Person(ctx, loop.GateRequest{Tool: loop.AskPersonToolName, Args: raw}, loop.GateDecision{Verdict: ledger.VerdictAsk})
	if err != nil {
		return personReply{err: err}
	}
	accepted := answer == loop.PersonAllowedOnce || answer == loop.PersonAlwaysHere
	var replies []loop.PersonReply
	for _, q := range questions {
		switch {
		case accepted && q.Type == loop.QuestionText:
		case accepted:
			replies = append(replies, loop.PersonReply{ID: q.ID, Chosen: recommendedOf(q)})
		default:
			replies = append(replies, loop.PersonReply{ID: q.ID})
		}
	}
	return personReply{replies: replies}
}

func (a AskPerson) answerLater(open *loop.Asked, replied <-chan personReply, recorded <-chan []string, questions []loop.PersonQuestion) {
	defer open.Close()
	ids := make([]string, len(questions))
	for i, q := range questions {
		ids[i] = q.ID
	}
	asked, late := strings.Join(ids, ","), false
	var reply personReply
	select {
	case reply = <-replied:
		a.backfill(<-recorded, answered(reply, questions).Outcome)
	case <-time.After(a.Wait):
		a.backfill(<-recorded, askTimedOut)
		none := askResult{Outcome: askTimedOut, Next: noAnswerYet}
		for _, q := range questions {
			none.Answers = append(none.Answers, askAnswer{ID: q.ID, Status: askUnanswered, Chosen: []string{}})
		}
		told, _ := json.Marshal(none)
		open.Answer("no answer yet to your "+loop.AskPersonToolName+" question: "+string(told), core.Origin{Source: core.SourceNoAnswer, Name: asked, Shown: shownToPerson(noAnswerHead, none, questions)})
		late, reply = true, <-replied
	}
	if errors.Is(reply.err, context.Canceled) {
		return
	}
	result := answered(reply, questions)
	said, _ := json.Marshal(result)
	from, intro, head := postedAs(result.Outcome, late)
	open.Answer(intro+string(said), core.Origin{Source: from, Name: asked, Shown: shownToPerson(head, result, questions)})
}

func postedAs(outcome askOutcome, late bool) (core.Source, string, string) {
	yours := "your " + loop.AskPersonToolName + " question"
	switch {
	case outcome == askSubmitted && late:
		return core.SourceLateAnswer, "the answer to " + yours + ", which came after you were told none had: ", "you answered"
	case outcome == askSubmitted:
		return core.SourceAnswer, "the answer to " + yours + ": ", "you answered"
	case outcome == askCancelled:
		return core.SourceDismissed, "the person dismissed " + yours + ": ", "the question was dismissed, so the recommended option stands"
	case outcome == askUndelivered:
		return core.SourceUnasked, yours + " could not be put to the person: ", "the question could not be asked, so the recommended option stands"
	}
	panic("ask_person: no answer is posted for outcome " + string(outcome))
}

func shownToPerson(head string, result askResult, questions []loop.PersonQuestion) string {
	lines := []string{head}
	for at, q := range questions {
		given := result.Answers[at]
		taken := cmp.Or(strings.Join(given.Chosen, ", "), given.Text)
		if given.Status == askAnswered && given.Note != "" {
			taken += " · " + given.Note
		}
		lines = append(lines, q.Question+" › "+cmp.Or(taken, string(given.Status)))
	}
	return strings.Join(lines, "\n")
}

func (a AskPerson) backfill(ids []string, outcome askOutcome) {
	for _, id := range ids {
		_ = a.Judge.Answered(id, string(outcome))
	}
}

func pollOf(questions []loop.PersonQuestion) string {
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

func answered(reply personReply, questions []loop.PersonQuestion) askResult {
	switch {
	case errors.Is(reply.err, context.Canceled):
		return resolved(askCancelled, questions, "the question was withdrawn when the turn stopped")
	case errors.As(reply.err, &loop.QuestionDismissed{}):
		return resolved(askCancelled, questions, "the person dismissed the question, so the recommended option stands")
	case reply.err != nil:
		return resolved(askUndelivered, questions, "the person could not be asked: "+reply.err.Error())
	}
	result := askResult{Outcome: askSubmitted, Next: takeTheRecommended}
	for _, q := range questions {
		answer := askAnswer{ID: q.ID, Status: askUnanswered, Chosen: []string{}, Note: nothingTyped}
		if at := slices.IndexFunc(reply.replies, func(r loop.PersonReply) bool { return r.ID == q.ID }); at >= 0 {
			switch given := reply.replies[at]; {
			case len(given.Chosen) > 0:
				answer.Status, answer.Chosen, answer.Note = askAnswered, given.Chosen, given.Text
			case given.Text != "":
				answer.Status, answer.Note, answer.Text = askAnswered, "", given.Text
			default:
				answer.Status, answer.Note = askSkipped, "the person chose no option and typed nothing"
			}
		}
		result.Answers = append(result.Answers, answer)
	}
	return result
}

func resolved(outcome askOutcome, questions []loop.PersonQuestion, why string) askResult {
	result := askResult{Outcome: outcome, Next: takeTheRecommended}
	for _, q := range questions {
		answer := askAnswer{ID: q.ID, Status: askUnanswered, Chosen: recommendedOf(q), Note: why}
		if q.Type == loop.QuestionText {
			answer.Note = nothingTyped
		}
		result.Answers = append(result.Answers, answer)
	}
	return result
}

func (a AskPerson) recordShadow(ctx context.Context, questions []loop.PersonQuestion) []string {
	if a.Judge == nil {
		return nil
	}
	var ids []string
	for _, q := range questions {
		state := subagents.AskState{Asker: subagents.AskerLead, Question: q.Question, Options: []subagents.AskOption{}}
		if q.Recommended != nil {
			state.Recommended = q.Options[*q.Recommended].Label
		}
		for _, o := range q.Options {
			state.Options = append(state.Options, subagents.AskOption{Label: o.Label, Description: o.Description})
		}
		if id, err := a.Judge.Record(ctx, state); err == nil {
			ids = append(ids, id)
		}
	}
	return ids
}

func proceeds(log string, ids []string, asked int) bool {
	if log == "" || len(ids) != asked {
		return false
	}
	rows := ledger.NewReader(log)
	for _, id := range ids {
		row, found, err := rows.ByID(id)
		if err != nil || !found || row.Verdict != ledger.VerdictAllow || len(row.Answers) == 0 {
			return false
		}
		judged, err := subagents.DecideAsk(row.Answers[0].Choice, row.Reason.Value, row.Reason.Threshold)
		if err != nil || judged.Effective != subagents.ActionProceed {
			return false
		}
	}
	return true
}

func optionsOf(q loop.PersonQuestion) []loop.PersonOption {
	if q.Type == loop.QuestionYesNo {
		return []loop.PersonOption{{Label: "yes"}, {Label: "no"}}
	}
	return q.Options
}

func recommendedOf(q loop.PersonQuestion) []string {
	if q.Recommended == nil {
		return []string{}
	}
	return []string{optionsOf(q)[*q.Recommended].Label}
}

func checkQuestions(questions []loop.PersonQuestion) error {
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
		}
		if err := entries.WordsAtMost(fmt.Sprintf("the header %q", q.Header), q.Header, konst.AskPersonHeaderWordsMost); err != nil {
			return fmt.Errorf("%s: %w", q.ID, err)
		}
		ids = append(ids, q.ID)
		if err := checkOptions(q); err != nil {
			return fmt.Errorf("%s: %w", q.ID, err)
		}
	}
	return nil
}

func checkOptions(q loop.PersonQuestion) error {
	switch q.Type {
	case loop.QuestionText:
		if len(q.Options) > 0 || q.Recommended != nil {
			return errors.New("a text question takes no options and no recommended")
		}
		return nil
	case loop.QuestionYesNo:
		if len(q.Options) > 0 {
			return errors.New("a yesno question is yes then no, so it takes no options")
		}
	case loop.QuestionChoice, loop.QuestionMulti:
		if len(q.Options) < konst.AskPersonOptionsLeast || len(q.Options) > konst.AskPersonOptionsMost {
			return fmt.Errorf("offers %d options, and a choice offers %d to %d", len(q.Options), konst.AskPersonOptionsLeast, konst.AskPersonOptionsMost)
		}
	default:
		return fmt.Errorf("the type %q is not choice, multi, text or yesno", q.Type)
	}
	for _, o := range q.Options {
		switch {
		case strings.TrimSpace(o.Label) == "":
			return fmt.Errorf("the label %q is empty, and a label is 1 to %d words", o.Label, konst.AskPersonLabelWordsMost)
		case strings.EqualFold(strings.TrimSpace(o.Label), "other"):
			return errors.New("tofu adds other to every choice, so the options never name one")
		}
		if err := entries.WordsAtMost(fmt.Sprintf("the label %q", o.Label), o.Label, konst.AskPersonLabelWordsMost); err != nil {
			return err
		}
	}
	if q.Recommended == nil || *q.Recommended < 0 || *q.Recommended >= len(optionsOf(q)) {
		return errors.New("recommended is the index of one of the options, and every question but text names one")
	}
	return nil
}
