package main

import (
	"cmp"
	"context"
	"encoding/json"
	"sort"

	"tofu/internal/judge/gate"
	"tofu/internal/judge/jev"
	"tofu/internal/judge/ledger"
	"tofu/internal/judge/question"
	"tofu/internal/judge/state"
	"tofu/internal/llm/models"
	"tofu/internal/sys"
	"tofu/internal/turn/tools"
	"tofu/library/questions"
)

type judgeOutcome struct {
	answers  []ledger.Answer
	decision jev.Decision
	fresh    bool
	verdict  ledger.Verdict
	mode     gate.Mode
}

func (o judgeOutcome) output(kinds map[string]question.Kind) map[string]any {
	out := make(map[string]any, len(o.answers))
	if o.fresh {
		for id, answer := range o.decision.Answers {
			out[id] = jevAnswerJSON(answer)
		}
		return out
	}
	for _, answer := range o.answers {
		out[answer.Question] = ledgerAnswerJSON(kinds[answer.Question], answer)
	}
	return out
}

type rowInput struct {
	decision     *jev.Decision
	replayOf     string
	build        string
	requestID    string
	answers      []ledger.Answer
	turnID       string
	stateBuilder string
	fingerprint  string
}

func rowSkeleton(state any, set battery, in rowInput) (ledger.Row, error) {
	body, err := ledger.Canonical(state)
	if err != nil {
		return ledger.Row{}, err
	}
	classifier, err := boundClassifier()
	if err != nil {
		return ledger.Row{}, err
	}
	if in.decision != nil {
		classifier.Provider = models.Provider(in.decision.Wire)
	}
	return ledger.Row{
		Point:        set.SetName,
		Questions:    set.SetName,
		Version:      set.QuestionsVersion,
		Model:        classifier.Slug(),
		StateHash:    ledger.HashOf(body),
		Fingerprint:  in.fingerprint,
		StateBuilder: in.stateBuilder,
		State:        body,
		Answers:      in.answers,
		ReplayOf:     in.replayOf,
		TurnID:       in.turnID,
	}, nil
}

func appendRow(state any, set battery, in rowInput) (ledger.Row, error) {
	dir, err := sys.LogDir()
	if err != nil {
		return ledger.Row{}, err
	}
	row, err := rowSkeleton(state, set, in)
	if err != nil {
		return ledger.Row{}, err
	}
	if set.Rule != nil {
		verdict, reason, err := gate.Decide(ledgerAnswersToJev(in.answers), *set.Rule)
		if err != nil {
			return ledger.Row{}, err
		}
		reason.Mode = set.Mode
		row.Verdict = verdict.Ledger()
		row.Policy = set.Rule.Name
		row.PolicyVersion = set.Rule.RuleVersion
		row.Reason = toLedgerReason(reason)
		if set.ModeReason != "" {
			row.Reason.ModeReason = &set.ModeReason
		}
	}
	if in.decision != nil {
		row.Build = in.decision.Build
		row.LatencyMS = in.decision.Latency.Milliseconds()
		row.Cost = in.decision.Usage.Cost
		row.RequestID = in.decision.RequestID
	} else {
		row.Build = in.build
		row.RequestID = in.requestID
	}
	return ledger.NewWriter(dir).Append(row)
}

func appendFallbackRow(state json.RawMessage, set battery, in rowInput, cause error) (ledger.Row, error) {
	if set.Rule == nil {
		return ledger.Row{}, cause
	}
	dir, err := sys.LogDir()
	if err != nil {
		return ledger.Row{}, err
	}
	row, err := rowSkeleton(state, set, in)
	if err != nil {
		return ledger.Row{}, err
	}
	fallback := gate.DecideUnavailable(cause, state)
	sentence := fallback.Sentence()
	row.Verdict = fallback.Verdict.Ledger()
	row.Policy, row.PolicyVersion = set.Rule.Name, set.Rule.RuleVersion
	row.Reason = toLedgerReason(fallback.Reason(*set.Rule, set.Mode))
	row.Reason.ModeReason = &sentence
	return ledger.NewWriter(dir).Append(row)
}

type judgeAsker struct {
	client   *jev.Client
	request  jev.Request
	wording  int
	set      battery
	decision jev.Decision
	verdict  ledger.Verdict
}

func (a *judgeAsker) Ask(ctx context.Context, _ ledger.Request) (ledger.Entry, error) {
	decision, err := a.client.Ask(ctx, a.request)
	if err != nil {
		return ledger.Entry{}, a.recordUnavailable(err)
	}
	a.decision = decision
	answers := toLedgerAnswers(a.wording, decision.Answers)
	row, err := appendRow(a.request.State, a.set, rowInput{decision: &decision, answers: answers})
	if err != nil {
		return ledger.Entry{}, err
	}
	a.verdict = row.Verdict
	return ledger.Entry{RowID: row.ID, Build: decision.Build, RequestID: decision.RequestID, Answers: answers}, nil
}

func (a *judgeAsker) recordUnavailable(cause error) error {
	state, err := json.Marshal(a.request.State)
	if err != nil {
		return cause
	}
	if _, err := appendFallbackRow(state, a.set, rowInput{}, cause); err != nil {
		return err
	}
	return cause
}

func runJudge(ctx context.Context, client *jev.Client, req jev.Request, set battery, noCache bool) (judgeOutcome, error) {
	asker := &judgeAsker{client: client, request: req, wording: set.QuestionsVersion, set: set}
	if noCache {
		entry, err := asker.Ask(ctx, ledger.Request{})
		if err != nil {
			return judgeOutcome{}, err
		}
		return judgeOutcome{answers: entry.Answers, fresh: true, decision: asker.decision, verdict: asker.verdict, mode: set.Mode}, nil
	}

	cacheDir, err := ledger.CacheDir()
	if err != nil {
		return judgeOutcome{}, err
	}
	cache := ledger.NewCache(cacheDir)
	ledgerReq := ledger.Request{State: req.State, Questions: set.SetName, Model: client.Model(), Version: set.QuestionsVersion}
	key, err := cache.Key(ledgerReq)
	if err != nil {
		return judgeOutcome{}, err
	}
	entry, hit, err := cache.Load(key)
	if err != nil {
		return judgeOutcome{}, err
	}
	if !hit {
		entry, err = asker.Ask(ctx, ledgerReq)
		if err != nil {
			return judgeOutcome{}, err
		}
		ledgerReq.Model = asker.decision.Alias
		if key, err = cache.Key(ledgerReq); err != nil {
			return judgeOutcome{}, err
		}
		if err := cache.Store(key, ledgerReq, entry); err != nil {
			return judgeOutcome{}, err
		}
		return judgeOutcome{answers: entry.Answers, fresh: true, decision: asker.decision, verdict: asker.verdict, mode: set.Mode}, nil
	}
	dir, err := sys.LogDir()
	if err != nil {
		return judgeOutcome{}, err
	}
	replayed, _, err := ledger.NewReader(dir).ByID(entry.RowID)
	if err != nil {
		return judgeOutcome{}, err
	}
	replayInput := rowInput{
		replayOf:    entry.RowID,
		build:       entry.Build,
		requestID:   entry.RequestID,
		answers:     entry.Answers,
		fingerprint: replayed.Fingerprint,
	}
	row, err := appendRow(req.State, set, replayInput)
	if err != nil {
		return judgeOutcome{}, err
	}
	return judgeOutcome{answers: entry.Answers, verdict: row.Verdict, mode: set.Mode}, nil
}

func jevAnswerJSON(a jev.Answer) map[string]any {
	switch a.Kind {
	case jev.QuestionNoul:
		return map[string]any{"type": "noul", "noul": a.Noul}
	case jev.QuestionChoice:
		return map[string]any{"type": "choice", "choice": a.Choice, "probabilities": a.Probabilities, "confidence": a.Confidence}
	case jev.QuestionScore:
		return map[string]any{"type": "score", "score": a.Score, "probabilities": a.Probabilities, "confidence": a.Confidence}
	}
	panic("tofu: unknown question kind")
}

func ledgerAnswerJSON(_ question.Kind, a ledger.Answer) map[string]any {
	switch a.Kind {
	case ledger.AnswerNoul:
		return map[string]any{"type": "noul", "noul": a.Noul}
	case ledger.AnswerChoice:
		return map[string]any{"type": "choice", "choice": a.Choice, "probabilities": distMap(a.Dist)}
	case ledger.AnswerScore:
		return map[string]any{"type": "score", "score": a.Score, "probabilities": distMap(a.Dist)}
	}
	panic("tofu: unknown answer kind")
}

func distMap(dist []ledger.Slice) map[string]float64 {
	out := make(map[string]float64, len(dist))
	for _, slice := range dist {
		out[slice.Option] = slice.P
	}
	return out
}

func toLedgerAnswers(wording int, in map[string]jev.Answer) []ledger.Answer {
	ids := make([]string, 0, len(in))
	for id := range in {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]ledger.Answer, 0, len(ids))
	for _, id := range ids {
		out = append(out, toLedgerAnswer(id, wording, in[id]))
	}
	return out
}

func toLedgerAnswer(id string, wording int, a jev.Answer) ledger.Answer {
	la := ledger.Answer{Question: id, Wording: wording}
	switch a.Kind {
	case jev.QuestionNoul:
		la.Kind = ledger.AnswerNoul
		la.Noul = a.Noul
	case jev.QuestionChoice:
		la.Kind = ledger.AnswerChoice
		la.Choice = a.Choice
		la.Dist = distOf(a.Probabilities)
	case jev.QuestionScore:
		la.Kind = ledger.AnswerScore
		la.Score = a.Score
		la.Dist = distOf(a.Probabilities)
		la.Legend = a.Legend
	}
	return la
}

func distOf(p map[string]float64) []ledger.Slice {
	keys := make([]string, 0, len(p))
	for k := range p {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]ledger.Slice, 0, len(keys))
	for _, k := range keys {
		out = append(out, ledger.Slice{Option: k, P: p[k]})
	}
	return out
}

func ledgerAnswersToJev(answers []ledger.Answer) map[string]jev.Answer {
	out := make(map[string]jev.Answer, len(answers))
	for _, a := range answers {
		ja := jev.Answer{}
		switch a.Kind {
		case ledger.AnswerNoul:
			ja.Kind, ja.Noul = jev.QuestionNoul, a.Noul
		case ledger.AnswerChoice:
			ja.Kind, ja.Choice = jev.QuestionChoice, a.Choice
		case ledger.AnswerScore:
			ja.Kind, ja.Score = jev.QuestionScore, a.Score
		}
		out[a.Question] = ja
	}
	return out
}

func toLedgerReason(r gate.Reason) *ledger.Reason {
	return &ledger.Reason{
		Question:   r.Question,
		Comparison: string(r.Comparison),
		Threshold:  r.Threshold,
		Value:      r.Value,
		DeadBand:   r.DeadBand,
		RelaxedBy:  r.RelaxedBy,
		Blocked:    r.Blocked,
		Ambiguous:  r.Ambiguous,
		Mode:       r.Mode.Ledger(),
	}
}

func memoryScopeJudge(dir string, say func(string)) *tools.ScopeJudge {
	layers, err := question.Layers(questions.Files(), dir)
	var set question.Set
	if err == nil {
		set, _, err = question.Resolve(state.MemoryScopeRef, layers)
	}
	var client *jev.Client
	if err == nil {
		client, err = newJevClient(oneCallAtATime)
	}
	logDir, logErr := sys.LogDir()
	if err = cmp.Or(err, logErr); err != nil {
		if say != nil {
			say("remember asks Jev nothing and refuses project local: " + err.Error())
		}
		return nil
	}
	return &tools.ScopeJudge{Client: client, Set: set, Ledger: ledger.NewWriter(logDir)}
}
