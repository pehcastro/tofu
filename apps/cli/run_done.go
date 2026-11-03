package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"tofu/internal/agent/loop"
	"tofu/internal/cli/wiring"
	"tofu/internal/judge/gate"
	"tofu/internal/judge/ledger"
	"tofu/internal/judge/state"
	"tofu/internal/models/classifiers/core"
	settingspkg "tofu/internal/store/settings"
)

const doneReviewPoint = state.StopCheckRuleRef

func newDoneReview(arm string, say func(string)) turn.DoneReview {
	switch arm {
	case settingspkg.DoneReviewOff:
		return nil
	case settingspkg.DoneReviewCheap:
		return turn.CheapDoneReview{}
	case settingspkg.DoneReviewScreen:
		return turn.ContractDoneReview{}
	case settingspkg.DoneReviewTyped:
		typed, err := newTypedDoneReview()
		if err == nil {
			return typed
		}
		reason := err.Error()
		if jev.IsUnbound(err) {
			reason = "no Jev classifier is bound"
		}
		say("the typed done review fell back to screen because " + reason + "; screen reads the acceptance lines with no Jev call")
		return turn.ContractDoneReview{}
	}
	panic("tofu run: unknown done review arm " + arm)
}

type typedDoneReview struct {
	client *jev.Client
	set    battery
}

func newTypedDoneReview() (typedDoneReview, error) {
	set, err := resolveStopCheckPoint()
	if err != nil {
		return typedDoneReview{}, err
	}
	client, err := wiring.NewJevClient()
	if err != nil {
		return typedDoneReview{}, err
	}
	return typedDoneReview{client: client, set: set}, nil
}

func resolveStopCheckPoint() (battery, error) {
	set, err := resolveLibrary(doneReviewPoint, "")
	if err != nil {
		return battery{}, err
	}
	pol, origin, err := state.StopCheckRule()
	if err != nil {
		return battery{}, err
	}
	if pol.Questions != set.SetName || pol.QuestionsVersion != set.QuestionsVersion {
		return battery{}, fmt.Errorf("rule %s names the question set %s@%d and the battery resolved %s@%d",
			doneReviewPoint, pol.Questions, pol.QuestionsVersion, set.SetName, set.QuestionsVersion)
	}
	resolution, err := resolveRuleMode(pol)
	if err != nil {
		return battery{}, err
	}
	pol = resolution.Rule
	set.Rule, set.Mode = &pol, resolution.Mode
	set.ModeReason = fmt.Sprintf("the rule came from %s as %s", origin, pol.File)
	if resolution.Reason != "" && !strings.Contains(resolution.Reason, pol.File) {
		set.ModeReason += "; " + resolution.Reason
	}
	return set, nil
}

func (r typedDoneReview) Review(ctx context.Context, subAgent turn.Row) (turn.DoneDecision, error) {
	screened, _ := (turn.ContractDoneReview{}).Review(ctx, subAgent)
	built, builder, err := state.BuildStopCheck(doneState(subAgent))
	if err != nil {
		return turn.DoneDecision{}, err
	}
	asked := json.RawMessage(built)
	in := rowInput{turnID: subAgent.ID, stateBuilder: builder}

	decision, askErr := r.client.Ask(ctx, jev.Request{State: asked, Questions: r.set.Questions})
	if askErr != nil {
		row, writeErr := appendFallbackRow(asked, r.set, in, askErr)
		if writeErr != nil {
			return turn.DoneDecision{}, fmt.Errorf("%w, and the row saying so did not write either: %w", askErr, writeErr)
		}
		if screened.Verdict == turn.DoneReopen {
			screened.ID = row.ID
			return screened, nil
		}
		return turn.DoneDecision{}, askErr
	}

	in.decision = &decision
	in.answers = toLedgerAnswers(r.set.QuestionsVersion, decision.Answers)
	row, err := appendRow(asked, r.set, in)
	if err != nil {
		return turn.DoneDecision{}, err
	}
	screened.ID = row.ID
	switch {
	case screened.Verdict == turn.DoneReopen:
		return screened, nil
	case r.set.Mode == gate.ModeEnforced:
		return turn.DoneDecision{ID: row.ID, Verdict: doneVerdict(row.Verdict), Reason: doneReason(row)}, nil
	}
	screened.Reason += "; stop_check ran in " + r.set.Mode.String() + ", so this decides nothing: " + doneReason(row)
	return screened, nil
}

func doneVerdict(verdict ledger.Verdict) turn.DoneVerdict {
	switch verdict {
	case ledger.VerdictAllow:
		return turn.DoneReopen
	case ledger.VerdictAsk, ledger.VerdictDeny:
		return turn.DoneAccepted
	}
	panic("tofu run: the done review cannot read the verdict " + verdict.String())
}

func doneReason(row ledger.Row) string {
	answered := make([]string, 0, len(row.Answers))
	for _, answer := range row.Answers {
		switch answer.Kind {
		case ledger.AnswerNoul:
			answered = append(answered, fmt.Sprintf("%s %.2f", answer.Question, answer.Noul))
		case ledger.AnswerScore:
			answered = append(answered, fmt.Sprintf("%s %.2f", answer.Question, answer.Score))
		case ledger.AnswerChoice:
			answered = append(answered, answer.Question+" "+answer.Choice)
		}
	}
	return fmt.Sprintf("%s read your own row and answered %s, which the rule reads as %s; tofu why %s has the whole chain",
		doneReviewPoint, strings.Join(answered, ", "), row.Verdict.String(), row.ID)
}

func doneState(subAgent turn.Row) state.StopCheckState {
	built := state.StopCheckState{
		Task: subAgent.Task,
		Budget: state.StopCheckBudget{
			AtStepCap:      subAgent.Outcome == turn.OutcomeStepCap || subAgent.Outcome == turn.OutcomeForkStop,
			AtDecisionCap:  subAgent.Outcome == turn.OutcomeDecisionCap,
			AtWallClockCap: subAgent.Outcome == turn.OutcomeRetiredWallClockCap,
		},
	}
	for at, step := range subAgent.Steps {
		recent := state.StopCheckStep{
			Index:              step.Index,
			AssistantText:      step.AssistantText,
			StopReason:         step.StopReason,
			RepeatsEarlierStep: repeatsEarlierStep(subAgent.Steps, at),
		}
		for _, call := range step.ToolCalls {
			recent.ToolCalls = append(recent.ToolCalls, state.StopCheckCall{
				Tool:    call.Tool,
				Command: call.Command,
				Failed:  call.Outcome().Failed(),
			})
		}
		built.RecentSteps = append(built.RecentSteps, recent)
	}
	return built
}

func repeatsEarlierStep(steps []turn.StepRow, at int) bool {
	if len(steps[at].ToolCalls) == 0 {
		return false
	}
	for _, call := range steps[at].ToolCalls {
		if call.Command == "" || !ranEarlier(steps[:at], call.Command) {
			return false
		}
	}
	return true
}

func ranEarlier(earlier []turn.StepRow, command string) bool {
	for _, step := range earlier {
		for _, call := range step.ToolCalls {
			if strings.Contains(call.Command, command) {
				return true
			}
		}
	}
	return false
}
