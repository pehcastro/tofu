package wiring

import (
	"encoding/json"
	"sort"
	"strings"

	"tofu/internal/judge/gate"
	"tofu/internal/judge/ledger"
	"tofu/internal/models/classifiers/core"
	"tofu/internal/models/llm/catalog"
	"tofu/internal/platform/project"
)

type RowInput struct {
	Decision     *core.Decision
	Judged       *ModelJudged
	ReplayOf     string
	Build        string
	RequestID    string
	Answers      []ledger.Answer
	TurnID       string
	StateBuilder string
	Fingerprint  string
}

type RowLedger struct {
	writer     *ledger.Writer
	classifier catalog.Model
}

func OpenRowLedger(log string) (RowLedger, error) {
	classifier, err := BoundClassifier()
	if core.IsUnbound(err) {
		err = nil
	}
	return RowLedger{writer: ledger.NewWriter(log), classifier: classifier}, err
}

func openedRowLedger() (RowLedger, error) {
	dir, err := project.LogDir()
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
		classifier.Provider = catalog.Provider(in.Decision.Wire)
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
		ReplayOf:     in.ReplayOf,
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
	switch judged := in.Judged; {
	case in.Decision != nil:
		in.Decision.Spent(&row)
	case judged != nil:
		charge := ledger.ChargeOf(judged.CostUSD, 0)
		row.Model, row.Provider, row.Build, row.LatencyMS, row.Cost, row.Charge = judged.Slug, judged.Source, judged.Build, judged.Latency.Milliseconds(), judged.CostUSD, &charge
		if row.Reason != nil {
			row.Reason.ModeReason = &judged.Why
		}
	default:
		row.Build = in.Build
		row.RequestID = in.RequestID
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
	sentence := "the call acts on " + strings.Join(own, ", ") + ", which tofu started, so it is allowed without asking jev, and a kill runs as shell stop"
	return l.allowedWithoutJev(state, set, in, ledger.Reason{ModeReason: &sentence})
}

func (l RowLedger) AppendPersonAllowed(state json.RawMessage, set Battery, in RowInput, by string) (ledger.Row, error) {
	sentence := "the person allowed this exact call for the rest of the session, so it runs without asking jev"
	return l.allowedWithoutJev(state, set, in, ledger.Reason{AllowedBy: by, ModeReason: &sentence})
}

func (l RowLedger) allowedWithoutJev(state json.RawMessage, set Battery, in RowInput, reason ledger.Reason) (ledger.Row, error) {
	row, err := l.skeleton(state, set, in)
	if err != nil {
		return ledger.Row{}, err
	}
	reason.Mode = set.Mode.Ledger()
	row.Model, row.Answers, row.Verdict, row.Reason = "", []ledger.Answer{}, ledger.VerdictAllow, &reason
	if set.Rule != nil {
		row.Policy, row.PolicyVersion = set.Rule.Name, set.Rule.RuleVersion
	}
	return l.writer.Append(row)
}

func ToLedgerAnswers(wording int, in map[string]core.Answer) []ledger.Answer {
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

func toLedgerAnswer(id string, wording int, a core.Answer) ledger.Answer {
	la := ledger.Answer{Question: id, Wording: wording}
	switch a.Kind {
	case core.QuestionNoul:
		la.Kind = ledger.AnswerNoul
		la.Noul = a.Noul
	case core.QuestionChoice:
		la.Kind = ledger.AnswerChoice
		la.Choice = a.Choice
		la.Dist = distOf(a.Probabilities)
	case core.QuestionScore:
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

func LedgerAnswersToJev(answers []ledger.Answer) map[string]core.Answer {
	out := make(map[string]core.Answer, len(answers))
	for _, a := range answers {
		ja := core.Answer{}
		switch a.Kind {
		case ledger.AnswerNoul:
			ja.Kind, ja.Noul = core.QuestionNoul, a.Noul
		case ledger.AnswerChoice:
			ja.Kind, ja.Choice = core.QuestionChoice, a.Choice
		case ledger.AnswerScore:
			ja.Kind, ja.Score = core.QuestionScore, a.Score
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
