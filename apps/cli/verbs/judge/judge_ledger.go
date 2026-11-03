package judge

import (
	"cmp"
	"context"
	"encoding/json"
	"sort"
	"strings"

	"tofu/apps/cli/wiring"
	"tofu/internal/judge/gate"
	"tofu/internal/judge/ledger"
	"tofu/internal/judge/questions"
	"tofu/internal/judge/state"
	"tofu/internal/models/classifiers/core"
	"tofu/internal/models/llm/catalog"
	"tofu/internal/sys"
	"tofu/internal/tools/registry"
	"tofu/library/questions"
)

type Outcome struct {
	answers  []ledger.Answer
	Decision jev.Decision
	fresh    bool
	verdict  ledger.Verdict
	mode     gate.Mode
}

func (o Outcome) output(kinds map[string]question.Kind) map[string]any {
	out := make(map[string]any, len(o.answers))
	if o.fresh {
		for id, answer := range o.Decision.Answers {
			out[id] = jevAnswerJSON(answer)
		}
		return out
	}
	for _, answer := range o.answers {
		out[answer.Question] = ledgerAnswerJSON(kinds[answer.Question], answer)
	}
	return out
}

type RowInput struct {
	Decision     *jev.Decision
	replayOf     string
	build        string
	requestID    string
	Answers      []ledger.Answer
	TurnID       string
	StateBuilder string
	Fingerprint  string
}

type RowLedger struct {
	writer     *ledger.Writer
	classifier models.Model
}

func OpenRowLedger(log string) (RowLedger, error) {
	classifier, err := wiring.BoundClassifier()
	if jev.IsUnbound(err) {
		err = nil
	}
	return RowLedger{writer: ledger.NewWriter(log), classifier: classifier}, err
}

func openedRowLedger() (RowLedger, error) {
	dir, err := sys.LogDir()
	if err != nil {
		return RowLedger{}, err
	}
	return OpenRowLedger(dir)
}

func AppendRow(state any, set Battery, in RowInput) (ledger.Row, error) {
	rows, err := openedRowLedger()
	if err != nil {
		return ledger.Row{}, err
	}
	return rows.Append(state, set, in)
}

func AppendFallbackRow(state json.RawMessage, set Battery, in RowInput, cause error) (ledger.Row, error) { //nolint:unparam
	if set.Rule == nil {
		return ledger.Row{}, cause
	}
	rows, err := openedRowLedger()
	if err != nil {
		return ledger.Row{}, err
	}
	return rows.AppendFallback(state, set, in, cause)
}

func (l RowLedger) skeleton(state any, set Battery, in RowInput) (ledger.Row, error) {
	body, err := ledger.Canonical(state)
	if err != nil {
		return ledger.Row{}, err
	}
	classifier := l.classifier
	if in.Decision != nil {
		classifier.Provider = models.Provider(in.Decision.Wire)
	}
	return ledger.Row{
		Point:        set.SetName,
		Questions:    set.SetName,
		Version:      set.QuestionsVersion,
		Model:        classifier.Slug(),
		StateHash:    ledger.HashOf(body),
		Fingerprint:  in.Fingerprint,
		StateBuilder: in.StateBuilder,
		State:        body,
		Answers:      in.Answers,
		ReplayOf:     in.replayOf,
		TurnID:       in.TurnID,
	}, nil
}

func (l RowLedger) Append(state any, set Battery, in RowInput) (ledger.Row, error) {
	row, err := l.skeleton(state, set, in)
	if err != nil {
		return ledger.Row{}, err
	}
	if set.Rule != nil {
		verdict, reason, err := gate.Decide(LedgerAnswersToJev(in.Answers), *set.Rule)
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
	if in.Decision != nil {
		row.LatencyMS = in.Decision.Latency.Milliseconds()
		in.Decision.Spent(&row)
	} else {
		row.Build = in.build
		row.RequestID = in.requestID
	}
	return l.writer.Append(row)
}

func (l RowLedger) AppendFallback(state json.RawMessage, set Battery, in RowInput, cause error) (ledger.Row, error) {
	if set.Rule == nil {
		return ledger.Row{}, cause
	}
	row, err := l.skeleton(state, set, in)
	if err != nil {
		return ledger.Row{}, err
	}
	fallback := gate.DecideUnavailable(cause, state)
	sentence := fallback.Sentence()
	row.Verdict = fallback.Verdict.Ledger()
	row.Policy, row.PolicyVersion = set.Rule.Name, set.Rule.RuleVersion
	row.Reason = toLedgerReason(fallback.Reason(*set.Rule, set.Mode))
	row.Reason.ModeReason = &sentence
	return l.writer.Append(row)
}

func (l RowLedger) AppendOwnShell(state json.RawMessage, set Battery, in RowInput, own []string) (ledger.Row, error) {
	row, err := l.skeleton(state, set, in)
	if err != nil {
		return ledger.Row{}, err
	}
	sentence := "the call acts on " + strings.Join(own, ", ") + ", which tofu started, so it is allowed without asking jev, and a kill runs as shell stop"
	row.Model, row.Answers, row.Verdict = "", []ledger.Answer{}, ledger.VerdictAllow
	row.Reason = &ledger.Reason{Mode: set.Mode.Ledger(), ModeReason: &sentence}
	if set.Rule != nil {
		row.Policy, row.PolicyVersion = set.Rule.Name, set.Rule.RuleVersion
	}
	return l.writer.Append(row)
}

func AskFresh(ctx context.Context, client *jev.Client, req jev.Request, set Battery) (Outcome, ledger.Entry, error) {
	decision, err := client.Ask(ctx, req)
	if err != nil {
		state, notEncoded := json.Marshal(req.State)
		if notEncoded != nil {
			return Outcome{}, ledger.Entry{}, err
		}
		if _, unwritten := AppendFallbackRow(state, set, RowInput{}, err); unwritten != nil {
			return Outcome{}, ledger.Entry{}, unwritten
		}
		return Outcome{}, ledger.Entry{}, err
	}
	answers := ToLedgerAnswers(set.QuestionsVersion, decision.Answers)
	row, err := AppendRow(req.State, set, RowInput{Decision: &decision, Answers: answers})
	if err != nil {
		return Outcome{}, ledger.Entry{}, err
	}
	entry := ledger.Entry{RowID: row.ID, Build: decision.Build, RequestID: cmp.Or(decision.RequestID, decision.TransportID), Answers: answers}
	return Outcome{answers: answers, fresh: true, Decision: decision, verdict: row.Verdict, mode: set.Mode}, entry, nil
}

func RunJudge(ctx context.Context, client *jev.Client, req jev.Request, set Battery, noCache bool) (Outcome, error) {
	if noCache {
		outcome, _, err := AskFresh(ctx, client, req, set)
		return outcome, err
	}

	cacheDir, err := ledger.CacheDir()
	if err != nil {
		return Outcome{}, err
	}
	cache := ledger.NewCache(cacheDir)
	ledgerReq := ledger.Request{State: req.State, Questions: set.SetName, Model: client.Model(), Version: set.QuestionsVersion}
	key, err := cache.Key(ledgerReq)
	if err != nil {
		return Outcome{}, err
	}
	entry, hit, err := cache.Load(key)
	if err != nil {
		return Outcome{}, err
	}
	if !hit {
		outcome, entry, err := AskFresh(ctx, client, req, set)
		if err != nil {
			return Outcome{}, err
		}
		ledgerReq.Model = outcome.Decision.Alias
		if key, err = cache.Key(ledgerReq); err != nil {
			return Outcome{}, err
		}
		if err := cache.Store(key, ledgerReq, entry); err != nil {
			return Outcome{}, err
		}
		return outcome, nil
	}
	dir, err := sys.LogDir()
	if err != nil {
		return Outcome{}, err
	}
	replayed, _, err := ledger.NewReader(dir).ByID(entry.RowID)
	if err != nil {
		return Outcome{}, err
	}
	replayInput := RowInput{
		replayOf:    entry.RowID,
		build:       entry.Build,
		requestID:   entry.RequestID,
		Answers:     entry.Answers,
		Fingerprint: replayed.Fingerprint,
	}
	row, err := AppendRow(req.State, set, replayInput)
	if err != nil {
		return Outcome{}, err
	}
	return Outcome{answers: entry.Answers, verdict: row.Verdict, mode: set.Mode}, nil
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

func ToLedgerAnswers(wording int, in map[string]jev.Answer) []ledger.Answer {
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

func LedgerAnswersToJev(answers []ledger.Answer) map[string]jev.Answer {
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

func MemoryScopeJudge(dir, log string, keys jev.Keyring, say func(string)) *tools.ScopeJudge {
	layers, err := question.Layers(questions.Files(), dir)
	var set question.Set
	if err == nil {
		set, _, err = question.Resolve(state.MemoryScopeRef, layers)
	}
	var client *jev.Client
	if err == nil {
		client, err = wiring.JevClientFrom(keys, wiring.OneCallAtATime)
	}
	if err != nil {
		if say != nil {
			say("remember asks Jev nothing and refuses project local: " + err.Error())
		}
		return nil
	}
	return &tools.ScopeJudge{Client: client, Set: set, Ledger: ledger.NewWriter(log)}
}
