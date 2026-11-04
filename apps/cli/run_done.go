package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"tofu/apps/cli/wiring"
	"tofu/internal/agent/loop"
	"tofu/internal/judge/gate"
	"tofu/internal/judge/ledger"
	"tofu/internal/judge/state"
	"tofu/internal/models/classifiers/core"
	settingspkg "tofu/internal/store/settings"
)

const doneReviewPoint = state.StopCheckRuleRef

func newDoneReview(arm string, say func(string)) loop.DoneReview {
	switch arm {
	case settingspkg.DoneReviewOff:
		return nil
	case settingspkg.DoneReviewCheap:
		return loop.CheapDoneReview{}
	case settingspkg.DoneReviewScreen:
		return loop.ContractDoneReview{}
	case settingspkg.DoneReviewTyped:
		typed, err := newTypedDoneReview()
		if err == nil {
			return typed
		}
		reason := err.Error()
		if core.IsUnbound(err) {
			reason = "no Jev classifier is bound"
		}
		say("the typed done review fell back to screen because " + reason + "; screen reads the acceptance lines with no Jev call")
		return loop.ContractDoneReview{}
	}
	panic("tofu run: unknown done review arm " + arm)
}

type typedDoneReview struct {
	client *core.Client
	set    wiring.Battery
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

func resolveStopCheckPoint() (wiring.Battery, error) {
	set, err := wiring.ResolveLibrary(doneReviewPoint, "")
	if err != nil {
		return wiring.Battery{}, err
	}
	pol, origin, err := state.StopCheckRule()
	if err != nil {
		return wiring.Battery{}, err
	}
	if pol.Questions != set.SetName || pol.QuestionsVersion != set.QuestionsVersion {
		return wiring.Battery{}, fmt.Errorf("rule %s names the question set %s@%d and the battery resolved %s@%d",
			doneReviewPoint, pol.Questions, pol.QuestionsVersion, set.SetName, set.QuestionsVersion)
	}
	resolution, err := wiring.ResolveRuleMode(pol)
	if err != nil {
		return wiring.Battery{}, err
	}
	pol = resolution.Rule
	set.Rule, set.Mode = &pol, resolution.Mode
	set.ModeReason = fmt.Sprintf("the rule came from %s as %s", origin, pol.File)
	if resolution.Reason != "" && !strings.Contains(resolution.Reason, pol.File) {
		set.ModeReason += "; " + resolution.Reason
	}
	return set, nil
}

func (r typedDoneReview) Review(ctx context.Context, subAgent loop.Row) (loop.DoneDecision, error) {
	screened, _ := (loop.ContractDoneReview{}).Review(ctx, subAgent)
	built, builder, err := state.BuildStopCheck(doneState(subAgent))
	if err != nil {
		return loop.DoneDecision{}, err
	}
	asked := json.RawMessage(built)
	in := wiring.RowInput{TurnID: subAgent.ID, StateBuilder: builder}

	decision, askErr := r.client.Ask(ctx, core.Request{State: asked, Questions: r.set.Questions})
	if askErr != nil {
		row, writeErr := wiring.AppendFallbackRow(asked, r.set, in, askErr)
		if writeErr != nil {
			return loop.DoneDecision{}, fmt.Errorf("%w, and the row saying so did not write either: %w", askErr, writeErr)
		}
		if screened.Verdict == loop.DoneReopen {
			screened.ID = row.ID
			return screened, nil
		}
		return loop.DoneDecision{}, askErr
	}

	in.Decision = &decision
	in.Answers = wiring.ToLedgerAnswers(r.set.QuestionsVersion, decision.Answers)
	row, err := wiring.AppendRow(asked, r.set, in)
	if err != nil {
		return loop.DoneDecision{}, err
	}
	screened.ID = row.ID
	switch {
	case screened.Verdict == loop.DoneReopen:
		return screened, nil
	case r.set.Mode == gate.ModeEnforced:
		return loop.DoneDecision{ID: row.ID, Verdict: doneVerdict(row.Verdict), Reason: doneReason(row)}, nil
	}
	screened.Reason += "; stop_check ran in " + r.set.Mode.String() + ", so this decides nothing: " + doneReason(row)
	return screened, nil
}

func doneVerdict(verdict ledger.Verdict) loop.DoneVerdict {
	switch verdict {
	case ledger.VerdictAllow:
		return loop.DoneReopen
	case ledger.VerdictAsk, ledger.VerdictDeny:
		return loop.DoneAccepted
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

func doneState(subAgent loop.Row) state.StopCheckState {
	built := state.StopCheckState{
		Task: subAgent.Task,
		Budget: state.StopCheckBudget{
			AtStepCap:      subAgent.Outcome == loop.OutcomeStepCap || subAgent.Outcome == loop.OutcomeForkStop,
			AtDecisionCap:  subAgent.Outcome == loop.OutcomeDecisionCap,
			AtWallClockCap: subAgent.Outcome == loop.OutcomeRetiredWallClockCap,
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

func repeatsEarlierStep(steps []loop.StepRow, at int) bool {
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

func ranEarlier(earlier []loop.StepRow, command string) bool {
	for _, step := range earlier {
		for _, call := range step.ToolCalls {
			if strings.Contains(call.Command, command) {
				return true
			}
		}
	}
	return false
}
