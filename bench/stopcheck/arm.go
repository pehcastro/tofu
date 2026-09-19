package stopcheck

import (
	"strings"

	"boji/internal/judge/state"
	"boji/internal/konst"
)

type Answer string

const (
	Continue Answer = "continue"
	Stop     Answer = "stop"
)

type Cheap struct {
	Answer Answer
	Rule   string
}

func CheapArm(turn Turn, at int) Cheap {
	step := turn.Steps[at]
	switch {
	case step.Index >= konst.TurnMaxSteps:
		return Cheap{Stop, "the loop's own step cap"}
	case len(step.Calls) == 0:
		return Cheap{Stop, "the step called no tool, which is how the loop ends a turn today"}
	case repeatsEarlierStep(turn, at):
		return Cheap{Stop, "every command in the step had already run earlier in this turn"}
	}
	return Cheap{Continue, "the step ran a command the turn had not run before"}
}

func repeatsEarlierStep(turn Turn, at int) bool {
	step := turn.Steps[at]
	if len(step.Calls) == 0 {
		return false
	}
	for _, call := range step.Calls {
		if call.Command == "" || !ranEarlier(turn.Steps[:at], call.Command) {
			return false
		}
	}
	return true
}

func ranEarlier(earlier []Step, command string) bool {
	for _, step := range earlier {
		for _, call := range step.Calls {
			if strings.Contains(call.Command, command) {
				return true
			}
		}
	}
	return false
}

func StateAt(turn Turn, at int) state.StopCheckState {
	built := state.StopCheckState{Task: turn.Task}
	decisions := 0
	for i := 0; i <= at; i++ {
		step := turn.Steps[i]
		converted := state.StopCheckStep{
			Index:              step.Index,
			AssistantText:      step.AssistantText,
			StopReason:         step.StopReason,
			RepeatsEarlierStep: repeatsEarlierStep(turn, i),
		}
		for _, call := range step.Calls {
			decisions++
			converted.ToolCalls = append(converted.ToolCalls, state.StopCheckCall{
				Tool: call.Tool, Command: call.Command, Failed: call.Failed,
			})
		}
		built.RecentSteps = append(built.RecentSteps, converted)
	}
	built.Budget = state.StopCheckBudget{
		AtStepCap:     turn.Steps[at].Index >= konst.TurnMaxSteps,
		AtDecisionCap: decisions >= konst.TurnMaxDecisions,
	}
	return built
}
