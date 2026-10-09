package subagent

import (
	"context"
	"encoding/json"
	"maps"
	"slices"

	"tofu/internal/judge/jev"
	"tofu/internal/judge/ledger"
	"tofu/internal/judge/question"
)

const AskStateBuilder = "subagent.AskState@2"

const OutcomeKindAskAnswer = "ask-answer"

type AskJudge struct {
	Client *jev.Client
	Set    question.Set
	Rule   AskRule
	Ledger *ledger.Writer
}

func (j AskJudge) Record(ctx context.Context, state AskState) (string, error) {
	body, err := ledger.Canonical(state)
	if err != nil {
		return "", err
	}
	asked := make([]jev.Question, len(j.Set.Questions))
	for i, q := range j.Set.Questions {
		asked[i] = q.ToJev()
	}
	row := ledger.Row{
		Point: j.Set.Name, Questions: j.Set.Name, Version: j.Set.QuestionsVersion,
		StateHash: ledger.HashOf(body), StateBuilder: AskStateBuilder, State: body,
		Verdict: ledger.VerdictAsk, Policy: j.Rule.Name, PolicyVersion: j.Rule.RuleVersion,
		Reason: &ledger.Reason{Question: DeterminedQuestion, Comparison: ">=", Threshold: j.Rule.DeterminedLowAt, Mode: j.Rule.Mode.Ledger()},
	}
	decision, askErr := j.Client.Ask(ctx, jev.Request{State: json.RawMessage(body), Questions: asked})
	if askErr != nil {
		failed := "jev could not answer, so the code's ask_now stands: " + askErr.Error()
		row.Reason.ModeReason = &failed
	} else {
		action, determined := decision.Answers[ActionQuestion], decision.Answers[DeterminedQuestion]
		verdict, err := DecideAsk(action.Choice, determined.Noul, j.Rule.DeterminedLowAt)
		if err != nil {
			return "", err
		}
		if verdict.Effective != ActionAskNow {
			row.Verdict = ledger.VerdictAllow
		}
		row.Reason.Value = determined.Noul
		row.Build, row.Model, row.RequestID = decision.Build, decision.Alias, decision.RequestID
		row.LatencyMS, row.Cost = decision.Latency.Milliseconds(), decision.Usage.Cost
		row.Answers = []ledger.Answer{
			{Question: ActionQuestion, Wording: j.Set.QuestionsVersion, Kind: ledger.AnswerChoice, Choice: action.Choice, Dist: distOf(action.Probabilities)},
			{Question: DeterminedQuestion, Wording: j.Set.QuestionsVersion, Kind: ledger.AnswerNoul, Noul: determined.Noul},
		}
	}
	written, err := j.Ledger.Append(row)
	return written.ID, err
}

func (j AskJudge) Answered(id, outcome string) error {
	return j.Ledger.Backfill(id, ledger.Outcome{Kind: OutcomeKindAskAnswer, Detail: outcome})
}

func distOf(probabilities map[string]float64) []ledger.Slice {
	dist := make([]ledger.Slice, 0, len(probabilities))
	for _, option := range slices.Sorted(maps.Keys(probabilities)) {
		dist = append(dist, ledger.Slice{Option: option, P: probabilities[option]})
	}
	return dist
}
