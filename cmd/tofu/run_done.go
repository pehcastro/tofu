package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"tofu/internal/judge/jev"
	"tofu/internal/judge/ledger"
	"tofu/internal/judge/state"
	"tofu/internal/turn"
)

const doneReviewPoint = "stop_check@1"

const (
	doneArmOff   = "off"
	doneArmCheap = "cheap"
	doneArmTyped = "typed"
)

func doneArms() []string { return []string{doneArmOff, doneArmCheap, doneArmTyped} }

func newDoneReview(arm string) (turn.DoneReview, error) {
	switch arm {
	case doneArmOff:
		return nil, nil
	case doneArmCheap:
		return turn.CheapDoneReview{}, nil
	case doneArmTyped:
		return newTypedDoneReview()
	}
	panic("tofu run: unknown done review arm " + arm)
}

type typedDoneReview struct {
	client *jev.Client
	set    battery
}

func newTypedDoneReview() (typedDoneReview, error) {
	set, err := resolvePoint(doneReviewPoint)
	if err != nil {
		return typedDoneReview{}, err
	}
	client, err := newJevClient()
	if err != nil {
		return typedDoneReview{}, err
	}
	return typedDoneReview{client: client, set: set}, nil
}

func (r typedDoneReview) Review(ctx context.Context, child turn.Row) (turn.DoneDecision, error) {
	built, builder, err := state.BuildStopCheck(doneState(child))
	if err != nil {
		return turn.DoneDecision{}, err
	}
	asked := json.RawMessage(built)
	in := rowInput{turnID: child.ID, stateBuilder: builder}

	decision, askErr := r.client.Ask(ctx, jev.Request{State: asked, Questions: r.set.Questions})
	if askErr != nil {
		if _, writeErr := appendFallbackRow(asked, r.set, in, askErr); writeErr != nil {
			return turn.DoneDecision{}, fmt.Errorf("%w, and the row saying so did not write either: %w", askErr, writeErr)
		}
		return turn.DoneDecision{}, askErr
	}

	in.decision = &decision
	in.answers = toLedgerAnswers(r.set.QuestionsVersion, decision.Answers)
	row, err := appendRow(asked, r.set, in)
	if err != nil {
		return turn.DoneDecision{}, err
	}
	return turn.DoneDecision{ID: row.ID, Verdict: doneVerdict(row.Verdict), Reason: doneReason(row)}, nil
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
	return fmt.Sprintf("%s read your own row and answered %s, which the policy reads as %s; tofu why %s has the whole chain",
		doneReviewPoint, strings.Join(answered, ", "), row.Verdict.String(), row.ID)
}

func doneState(child turn.Row) state.StopCheckState {
	built := state.StopCheckState{
		Task: child.Task,
		Budget: state.StopCheckBudget{
			AtStepCap:      child.Outcome == turn.OutcomeStepCap,
			AtDecisionCap:  child.Outcome == turn.OutcomeDecisionCap,
			AtWallClockCap: child.Outcome == turn.OutcomeRetiredWallClockCap,
		},
	}
	for at, step := range child.Steps {
		recent := state.StopCheckStep{
			Index:              step.Index,
			AssistantText:      step.AssistantText,
			StopReason:         step.StopReason,
			RepeatsEarlierStep: repeatsEarlierStep(child.Steps, at),
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
