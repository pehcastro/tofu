package stopcheck

import (
	"fmt"
	"sort"

	"boji/internal/judge/jev"
	"boji/internal/judge/jev/wire/openrouter"
	"boji/internal/judge/ledger"
	"boji/internal/judge/policy"
)

func toLedgerAnswers(wording int, in map[string]jev.Answer) []ledger.Answer {
	ids := make([]string, 0, len(in))
	for id := range in {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]ledger.Answer, 0, len(ids))
	for _, id := range ids {
		a := in[id]
		la := ledger.Answer{Question: id, Wording: wording}
		switch a.Kind {
		case jev.QuestionNoul:
			la.Kind, la.Noul = ledger.AnswerNoul, a.Noul
		case jev.QuestionChoice:
			la.Kind, la.Choice, la.Dist = ledger.AnswerChoice, a.Choice, distOf(a.Probabilities)
		case jev.QuestionScore:
			la.Kind, la.Score, la.Dist = ledger.AnswerScore, a.Score, distOf(a.Probabilities)
		default:
			panic(fmt.Sprintf("stopcheck: unknown answer kind %v", a.Kind))
		}
		out = append(out, la)
	}
	return out
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

func toJevAnswers(answers []ledger.Answer) map[string]jev.Answer {
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
		default:
			panic("stopcheck: unknown answer kind " + string(a.Kind))
		}
		out[a.Question] = ja
	}
	return out
}

type rowInput struct {
	state        any
	stateBuilder string
	wording      int
	answers      []ledger.Answer
	pol          policy.Policy
	mode         policy.Mode
	modeReason   string
	turnID       string
	replayOf     string
	build        string
	requestID    string
	latencyMS    int64
	cost         float64
}

func appendRow(writer *ledger.Writer, in rowInput) (ledger.Row, error) {
	hash, err := ledger.Hash(in.state)
	if err != nil {
		return ledger.Row{}, err
	}
	verdict, reason, err := policy.Decide(toJevAnswers(in.answers), in.pol)
	if err != nil {
		return ledger.Row{}, err
	}
	reason.Mode = in.mode
	row := ledger.Row{
		Point:         in.pol.Name,
		Questions:     in.pol.Questions,
		Version:       in.wording,
		Build:         in.build,
		Model:         openrouter.Alias,
		StateHash:     hash,
		StateBuilder:  in.stateBuilder,
		Answers:       in.answers,
		Verdict:       ledger.Verdict(verdict),
		Policy:        in.pol.Name,
		PolicyVersion: in.pol.PolicyVersion,
		Reason: &ledger.Reason{
			Question:   reason.Question,
			Comparison: string(reason.Comparison),
			Threshold:  reason.Threshold,
			Value:      reason.Value,
			DeadBand:   reason.DeadBand,
			RelaxedBy:  reason.RelaxedBy,
			Blocked:    reason.Blocked,
			Ambiguous:  reason.Ambiguous,
			Mode:       ledger.Mode(in.mode),
			ModeReason: &in.modeReason,
		},
		LatencyMS: in.latencyMS,
		Cost:      in.cost,
		RequestID: in.requestID,
		ReplayOf:  in.replayOf,
		TurnID:    in.turnID,
	}
	return writer.Append(row)
}
