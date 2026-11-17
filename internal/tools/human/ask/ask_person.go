package ask

import (
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

const takeTheRecommended = "on any outcome but submitted, take what each answer chose, which is the recommended option unless it waits on the person's yes, say which you took, and continue. " +
	"on submitted, follow what was chosen; a skipped question is yours to decide."

const noAnswerYet = "no answer came in time, so nothing was chosen and the question stays open: the person can still answer it, and that answer arrives as a message. " +
	"take no action that depends on it and pick no option for them. keep to work that does not depend on it, or end the turn saying it waits on this question."

const closedStaysOpen = "the person closed the question without choosing, so nothing was chosen and the question stays open: take no option for them, do nothing that depends on it, and do not ask it again. " +
	"keep to work that does not depend on it, or end the turn saying it waits on their decision; they answer in words when they are ready."

const noAnswerHead = "no answer yet; nothing was chosen and the question stays open"

const waitsOnYes = "this question waits on the person's own yes, and none came: do nothing it asks, and say it waits on them"

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
			"description": map[string]any{"type": "string", "description": "what taking it means, in one line of at most " + strconv.Itoa(konst.AskPersonDescriptionRunesMost) + " characters"},
			"preview":     map[string]any{"type": "string", "description": "optional: what taking it looks like, as code, a unified diff or a text screen; options that carry one are shown side by side"},
		},
		"required": []string{"label", "description"},
	}
	question := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id":          map[string]any{"type": "string"},
			"header":      map[string]any{"type": "string", "description": "1 to " + strconv.Itoa(konst.AskPersonHeaderWordsMost) + " words"},
			"question":    map[string]any{"type": "string", "description": "one sentence of at most " + strconv.Itoa(konst.AskPersonQuestionRunesMost) + " characters"},
			"context":     map[string]any{"type": "string", "description": "why it is asked and what each way leads to, as short markdown of at most " + strconv.Itoa(konst.AskPersonContextRunesMost) + " characters, shown under the question; the person reads it beside the options, so the chat need not repeat it"},
			"type":        map[string]any{"type": "string", "enum": []string{string(loop.QuestionChoice), string(loop.QuestionMulti), string(loop.QuestionText), string(loop.QuestionYesNo)}},
			"options":     map[string]any{"type": "array", "items": option, "minItems": konst.AskPersonOptionsLeast, "maxItems": konst.AskPersonOptionsMost, "description": "for choice and multi; yesno is yes then no; text has none"},
			"recommended": map[string]any{"type": "integer", "description": "the index of the option you would take; required for every type but text"},
			"reversible":  map[string]any{"type": "boolean", "description": "true only when the recommended option can be undone later at little cost; leave it out otherwise"},
			"needs_yes":   map[string]any{"type": "boolean", "description": "true when nothing it asks may happen without the person's own answer: never taken in auto, by the ask judge, by a timeout or by a dismissal"},
		},
		"required": []string{"id", "header", "question", "type"},
	}
	return core.Tool{
		Name: loop.AskPersonToolName,
		Description: "puts a short choice only the person can make to them as a form: which library, a name, a matter of taste, spending money, something that cannot be undone. " +
			"use it only when each option fits in a few words, and put the why in context. " +
			"a big or explanatory decision, a trade-off that needs paragraphs, or a plan to review is never asked here: write it in your reply as prose with numbered options, and the person answers in words. " +
			"never for permission to run a tool; the gate asks that. ask a decision once, and never repeat the question or its options in chat. " +
			"one call at a time, 1 to " + strconv.Itoa(konst.AskPersonQuestionsMost) + " questions, each with options and the one you recommend. " +
			"tofu adds a free-text other to every choice, so never write one. the answer says the outcome and, for each question, its status, what was chosen and any note the person typed with it. " +
			"in auto the work never stops on it: with no answer in time you are told none came, nothing is chosen, and the question stays open until the person answers. " +
			"a person who closes the form chooses nothing, and the decision stays open. " +
			"a reversible recommendation the ask judge agrees with is taken without asking, unless it needs_yes",
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
			NeedsYes   bool `json:"needs_yes"`
		} `json:"questions"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return contract.Result{}, fmt.Errorf("ask_person: arguments are not the expected shape: %w", err)
	}
	questions, reversible, held := make([]loop.PersonQuestion, len(args.Questions)), true, []string{}
	for i, q := range args.Questions {
		questions[i], reversible = q.PersonQuestion, reversible && q.Reversible && !q.NeedsYes && q.Recommended != nil
		if q.NeedsYes {
			held = append(held, q.ID)
		}
	}
	if err := loop.CheckQuestions(questions); err != nil {
		return contract.Result{}, fmt.Errorf("ask_person: %w", err)
	}
	for i := range questions {
		questions[i].Options = questions[i].Choices()
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
			taken := resolved(askTakenInAuto, questions, held, "the ask judge said proceed and the recommended option can be undone, so gatePrompt auto took it without asking the person")
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
		reply := answered(<-replied, questions, held)
		a.backfill(<-recorded, reply.Outcome)
		shown, err := json.Marshal(reply)
		return contract.Result{Content: string(shown)}, err
	}
	open := a.Inbox.Open()
	safe.Go(report, "ask answer", func() { a.answerLater(open, replied, recorded, questions, held) })
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
	answer, err := a.Person.Clocked(ctx, loop.GateRequest{Tool: loop.AskPersonToolName, Args: raw}, loop.GateDecision{Verdict: ledger.VerdictAsk})
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

func (a AskPerson) answerLater(open *loop.Asked, replied <-chan personReply, recorded <-chan []string, questions []loop.PersonQuestion, held []string) {
	defer open.Close()
	ids := make([]string, len(questions))
	for i, q := range questions {
		ids[i] = q.ID
	}
	asked, late := strings.Join(ids, ","), false
	var reply personReply
	select {
	case reply = <-replied:
		a.backfill(<-recorded, answered(reply, questions, held).Outcome)
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
	result := answered(reply, questions, held)
	said, _ := json.Marshal(result)
	from, intro, head := postedAs(result.Outcome, late, held)
	open.Answer(intro+string(said), core.Origin{Source: from, Name: asked, Shown: shownToPerson(head, result, questions)})
}

func postedAs(outcome askOutcome, late bool, held []string) (core.Source, string, string) {
	yours := "your " + loop.AskPersonToolName + " question"
	stands := "so the recommended option stands"
	if len(held) > 0 {
		stands = "so nothing was chosen and it waits on your answer"
	}
	switch {
	case outcome == askSubmitted && late:
		return core.SourceLateAnswer, "the answer to " + yours + ", which came after you were told none had: ", "you answered"
	case outcome == askSubmitted:
		return core.SourceAnswer, "the answer to " + yours + ": ", "you answered"
	case outcome == askCancelled:
		return core.SourceDismissed, "the person closed " + yours + " without choosing: ", "you closed the question; nothing was chosen and it stays open"
	case outcome == askUndelivered:
		return core.SourceUnasked, yours + " could not be put to the person: ", "the question could not be asked, " + stands
	}
	panic("ask_person: no answer is posted for outcome " + string(outcome))
}

func shownToPerson(head string, result askResult, questions []loop.PersonQuestion) string {
	replies := make([]loop.PersonReply, len(result.Answers))
	for i, given := range result.Answers {
		replies[i] = loop.PersonReply{ID: given.ID, Chosen: given.Chosen, Text: given.Text}
		if given.Status == askAnswered && given.Note != "" {
			replies[i].Text = given.Note
		}
	}
	return loop.ShownAnswers(head, questions, replies)
}

func (a AskPerson) backfill(ids []string, outcome askOutcome) {
	for _, id := range ids {
		_ = a.Judge.Answered(id, string(outcome))
	}
}

func answered(reply personReply, questions []loop.PersonQuestion, held []string) askResult {
	switch {
	case errors.Is(reply.err, context.Canceled):
		return resolved(askCancelled, questions, held, "the question was withdrawn when the turn stopped")
	case errors.As(reply.err, &loop.QuestionDismissed{}):
		closed := askResult{Outcome: askCancelled, Next: closedStaysOpen}
		for _, q := range questions {
			note := "the person closed the question without choosing"
			if slices.Contains(held, q.ID) {
				note = waitsOnYes
			}
			closed.Answers = append(closed.Answers, askAnswer{ID: q.ID, Status: askUnanswered, Chosen: []string{}, Note: note})
		}
		return closed
	case reply.err != nil:
		return resolved(askUndelivered, questions, held, "the person could not be asked: "+reply.err.Error())
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
		if answer.Status != askAnswered && slices.Contains(held, q.ID) {
			answer.Note = waitsOnYes
		}
		result.Answers = append(result.Answers, answer)
	}
	return result
}

func resolved(outcome askOutcome, questions []loop.PersonQuestion, held []string, why string) askResult {
	result := askResult{Outcome: outcome, Next: takeTheRecommended}
	for _, q := range questions {
		answer := askAnswer{ID: q.ID, Status: askUnanswered, Chosen: recommendedOf(q), Note: why}
		switch {
		case slices.Contains(held, q.ID):
			answer.Chosen, answer.Note = []string{}, waitsOnYes
		case q.Type == loop.QuestionText:
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

func recommendedOf(q loop.PersonQuestion) []string {
	if q.Recommended == nil {
		return []string{}
	}
	return []string{q.Choices()[*q.Recommended].Label}
}
