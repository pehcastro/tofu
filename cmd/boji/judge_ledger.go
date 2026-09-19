package main

import (
	"context"
	"encoding/json"
	"sort"

	"boji/internal/judge/jev"
	"boji/internal/judge/jev/wire/openrouter"
	"boji/internal/judge/ledger"
	"boji/internal/judge/policy"
	"boji/internal/judge/question"
)

type judgeOutcome struct {
	answers  []ledger.Answer
	decision jev.Decision
	fresh    bool
	verdict  ledger.Verdict
	mode     policy.Mode
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
}

func rowSkeleton(state any, set battery, in rowInput) (ledger.Row, error) {
	hash, err := ledger.Hash(state)
	if err != nil {
		return ledger.Row{}, err
	}
	return ledger.Row{
		Point:        set.SetName,
		Questions:    set.SetName,
		Version:      set.QuestionsVersion,
		Model:        openrouter.Alias,
		StateHash:    hash,
		StateBuilder: in.stateBuilder,
		Answers:      in.answers,
		ReplayOf:     in.replayOf,
		TurnID:       in.turnID,
	}, nil
}

func appendRow(state any, set battery, in rowInput) (ledger.Row, error) {
	dir, err := ledger.Dir()
	if err != nil {
		return ledger.Row{}, err
	}
	row, err := rowSkeleton(state, set, in)
	if err != nil {
		return ledger.Row{}, err
	}
	if set.Policy != nil {
		verdict, reason, err := policy.Decide(ledgerAnswersToJev(in.answers), *set.Policy)
		if err != nil {
			return ledger.Row{}, err
		}
		reason.Mode = set.Mode
		row.Verdict = toLedgerVerdict(verdict)
		row.Policy = set.Policy.Name
		row.PolicyVersion = set.Policy.PolicyVersion
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
	if set.Policy == nil {
		return ledger.Row{}, cause
	}
	dir, err := ledger.Dir()
	if err != nil {
		return ledger.Row{}, err
	}
	row, err := rowSkeleton(state, set, in)
	if err != nil {
		return ledger.Row{}, err
	}
	fallback := policy.DecideUnavailable(cause, state)
	sentence := fallback.Sentence()
	row.Verdict = toLedgerVerdict(fallback.Verdict)
	row.Policy, row.PolicyVersion = set.Policy.Name, set.Policy.PolicyVersion
	row.Reason = toLedgerReason(fallback.Reason(*set.Policy, set.Mode))
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
	ledgerReq := ledger.Request{State: req.State, Questions: set.SetName, Model: openrouter.Alias, Version: set.QuestionsVersion}
	entry, hit, err := ledger.NewCache(cacheDir).Resolve(ctx, ledgerReq, asker)
	if err != nil {
		return judgeOutcome{}, err
	}
	if !hit {
		return judgeOutcome{answers: entry.Answers, fresh: true, decision: asker.decision, verdict: asker.verdict, mode: set.Mode}, nil
	}
	replayInput := rowInput{replayOf: entry.RowID, build: entry.Build, requestID: entry.RequestID, answers: entry.Answers}
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
	panic("boji: unknown question kind")
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
	panic("boji: unknown answer kind")
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

func toLedgerVerdict(v policy.Verdict) ledger.Verdict {
	switch v {
	case policy.VerdictAllow:
		return ledger.VerdictAllow
	case policy.VerdictAsk:
		return ledger.VerdictAsk
	case policy.VerdictDeny:
		return ledger.VerdictDeny
	}
	panic("boji: unknown policy verdict " + string(v))
}

func toPolicyVerdict(v ledger.Verdict) policy.Verdict {
	switch v {
	case ledger.VerdictAllow:
		return policy.VerdictAllow
	case ledger.VerdictAsk:
		return policy.VerdictAsk
	case ledger.VerdictDeny:
		return policy.VerdictDeny
	}
	panic("boji: unknown ledger verdict " + string(v))
}

func toLedgerMode(m policy.Mode) ledger.Mode {
	switch m {
	case policy.ModeShadow:
		return ledger.ModeShadow
	case policy.ModeEnforced:
		return ledger.ModeEnforced
	}
	panic("boji: unknown policy mode " + string(m))
}

func toLedgerReason(r policy.Reason) *ledger.Reason {
	return &ledger.Reason{
		Question:   r.Question,
		Comparison: string(r.Comparison),
		Threshold:  r.Threshold,
		Value:      r.Value,
		DeadBand:   r.DeadBand,
		RelaxedBy:  r.RelaxedBy,
		Blocked:    r.Blocked,
		Ambiguous:  r.Ambiguous,
		Mode:       toLedgerMode(r.Mode),
	}
}
