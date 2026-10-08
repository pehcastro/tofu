package turn

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"tofu/internal/judge/method"
	"tofu/internal/judge/state"
	"tofu/internal/rule"
	"tofu/internal/subagent"
	shipped "tofu/library"
)

type DoneVerdict string

const (
	DoneAccepted DoneVerdict = "accepted"
	DoneReopen   DoneVerdict = "reopen"
)

type DoneDecision struct {
	ID      string
	Verdict DoneVerdict
	Reason  string
}

type DoneReview interface {
	Review(ctx context.Context, subAgent Row) (DoneDecision, error)
}

type CheapDoneReview struct{}

func (CheapDoneReview) Review(_ context.Context, subAgent Row) (DoneDecision, error) {
	for _, step := range subAgent.Steps {
		for _, call := range step.ToolCalls {
			if call.Error == "" && (call.ExitCode == nil || *call.ExitCode == 0) {
				return DoneDecision{Verdict: DoneAccepted, Reason: "the sub-agent ran " + call.Tool + " and it did not fail"}, nil
			}
		}
	}
	return DoneDecision{Verdict: DoneReopen, Reason: "nothing in the sub-agent's row is evidence the work happened: not one tool call ran without failing"}, nil
}

type ContractDoneReview struct{}

func (ContractDoneReview) Review(_ context.Context, subAgent Row) (DoneDecision, error) {
	contract := ContractOf(subAgent)
	switch {
	case contract.NoTicket:
		return DoneDecision{Verdict: DoneAccepted, Reason: "no ticket, so no acceptance lines and nothing to evaluate"}, nil
	case contract.Omissions() > 0:
		var missing []string
		for _, claim := range contract.Claims {
			if claim.Omitted() {
				missing = append(missing, claim.Line)
			}
		}
		reason := fmt.Sprintf("%d of %d acceptance lines carry no command and no output: %s",
			len(missing), len(contract.Claims), strings.Join(missing, "; "))
		return DoneDecision{Verdict: DoneReopen, Reason: reason}, nil
	default:
		return DoneDecision{Verdict: DoneAccepted, Reason: fmt.Sprintf("all %d acceptance lines carry a command and its output", len(contract.Claims))}, nil
	}
}

func ContractOf(subAgent Row) subagent.Contract {
	prose := ""
	for i := len(subAgent.Steps) - 1; i >= 0; i-- {
		if spoken := strings.TrimSpace(subAgent.Steps[i].AssistantText); spoken != "" {
			prose = spoken
			break
		}
	}
	return subagent.BuildContract(subAgent.Task, prose, outcomeStoppedEarly(subAgent.Outcome))
}

func (t *SpawnTool) decided(ctx context.Context, first Row) (DoneDecision, error) {
	table := t.Methods
	if table.Version == 0 {
		loaded, err := method.Load(shipped.Files())
		if err != nil {
			return DoneDecision{}, err
		}
		table = loaded
	}
	decision, _, err := method.Run(ctx, table, state.StopCheckPoint, method.Arms[DoneDecision]{
		Cheap:  func(ctx context.Context) (DoneDecision, error) { return CheapDoneReview{}.Review(ctx, first) },
		Judged: func(ctx context.Context) (DoneDecision, error) { return t.Review.Review(ctx, first) },
	})
	return decision, err
}

func stoppedEarly(state subagent.State, outcome Outcome) bool {
	return state == subagent.Parked || outcomeStoppedEarly(outcome)
}

func outcomeStoppedEarly(outcome Outcome) bool {
	switch outcome {
	case OutcomeStepCap, OutcomeRetiredCostCap, OutcomeRetiredWallClockCap, OutcomeDecisionCap:
		return true
	}
	return false
}

func roundState(outerCtx context.Context, runErr error) subagent.State {
	switch {
	case outerCtx.Err() != nil:
		return subagent.Parked
	case runErr != nil:
		return subagent.Errored
	default:
		return subagent.Finished
	}
}

func wholeRun(rounds []Row) Row {
	whole := rounds[len(rounds)-1]
	whole.Steps, whole.Warnings, whole.TotalCostUSD = nil, nil, 0
	for _, round := range rounds {
		whole.Steps, whole.Warnings = append(whole.Steps, round.Steps...), append(whole.Warnings, round.Warnings...)
		whole.TotalCostUSD += round.TotalCostUSD
	}
	return whole
}

func (t *SpawnTool) runRounds(outerCtx, subAgentCtx context.Context, held *heldSubAgent, subAgent Config) ([]Row, []string, subagent.State, error) {
	agent := held.agent
	first, firstErr := Run(subAgentCtx, subAgent)
	claims, state := []Row{first}, roundState(outerCtx, firstErr)
	held.remember(first)
	var sentBack []string
	recipes := projectRecipes(t.Project)
	for state == subagent.Finished {
		missed := gateMissed(t.Project, recipes, held.definition, held.boundary.Owns(), claims)
		if len(missed) == 0 && t.Review == nil {
			break
		}
		last := &claims[len(claims)-1]
		why := strings.Join(missed, "; ")
		decision := DoneDecision{Verdict: DoneReopen, Reason: fmt.Sprintf("you changed a %s file, and your gate is %s, each run after your last edit and passing: %s",
			held.definition.Language, strings.Join(held.definition.Gate, " and "), why)}
		if len(missed) == 0 {
			t.roster.Reached(agent.ID, subagent.InReview, reportOf(agent, held.forkedSoFar(), []Row{wholeRun(claims)}, subagent.InReview).Text(t.proseStore()))
			reviewed := *last
			reviewed.Task = agent.Brief
			var err error
			if decision, err = t.decided(subAgentCtx, reviewed); err != nil {
				last.Warnings = append(last.Warnings, "the done review did not run, so the sub-agent's own claim stands: "+err.Error())
				break
			}
			why = decision.Reason
		}
		if decision.ID != "" {
			last.DecisionIDs = append(last.DecisionIDs, decision.ID)
		}
		if decision.Verdict == DoneAccepted {
			break
		}
		if decision.Verdict != DoneReopen {
			panic("turn: unknown done verdict " + string(decision.Verdict))
		}
		if _, err := t.roster.Reopen(agent.ID, why); err != nil {
			last.Warnings = append(last.Warnings, err.Error())
			break
		}
		t.roster.Reached(agent.ID, subagent.Working, "sent back: "+why)
		sentBack = append(sentBack, why)
		subAgent.History = held.history
		subAgent.Task = "You reported this finished and the done review did not believe you: " + decision.Reason
		reRow, reErr := Run(subAgentCtx, subAgent)
		claims = append(claims, reRow)
		held.remember(reRow)
		if reErr != nil {
			claims[len(claims)-1].Warnings = append(claims[len(claims)-1].Warnings,
				"the sub-agent was re-opened and did not run again, so its earlier claim stands: "+reErr.Error())
			state = subagent.Errored
		} else {
			state = roundState(outerCtx, nil)
		}
	}
	return claims, sentBack, state, firstErr
}

func (t *SpawnTool) reportTaken(task string) string {
	agents := t.roster.SubAgents()
	at := slices.IndexFunc(agents, func(agent subagent.SubAgent) bool { return agent.State == subagent.Finished && agent.Report == task })
	if at < 0 {
		return ""
	}
	return "the person has not asked tofu to check a sub-agent's work, so take " + agents[at].ID + "'s report as the result: " +
		"do not read its files again or rerun its build, tests or type check, unless the report says something failed."
}

func (t *SpawnTool) cleanReport(task string) string {
	spawned := t.Spawned()
	if len(spawned) != 1 {
		return ""
	}
	id := spawned[0].ID
	agents := t.roster.SubAgents()
	at := slices.IndexFunc(agents, func(agent subagent.SubAgent) bool { return agent.ID == id })
	definition, err := t.SubAgents.Named(spawned[0].Agent)
	if at < 0 || agents[at].State != subagent.Finished || agents[at].Report != task || err != nil || definition.Language == "" || len(definition.Gate) == 0 {
		return ""
	}
	var rows []Row
	unseen := false
	t.tree.mu.Lock()
	for _, row := range t.tree.rows {
		if row.ID == id {
			rows = append(rows, row)
		}
		unseen = unseen || strings.HasPrefix(row.ID, id+"-f") || (row.ID == id && len(row.Steps) == 0)
	}
	t.tree.mu.Unlock()
	if unseen || len(rows) == 0 || completionOf(subagent.Finished, findings(wholeRun(rows), subagent.Finished)) != subagent.Done ||
		len(gateMissed(t.Project, projectRecipes(t.Project), definition, agents[at].Owns, rows)) > 0 {
		return ""
	}
	edited := false
	for _, row := range rows {
		for _, step := range row.Steps {
			for _, call := range step.ToolCalls {
				path := writtenPath(call)
				if slices.Contains([]string{".vue", ".svelte", ".tsx", ".jsx", ".html", ".css", ".scss"}, strings.ToLower(filepath.Ext(path))) {
					return ""
				}
				edited = edited || (path != "" && rule.LanguageOf(path) == definition.Language)
			}
		}
	}
	if !edited {
		return ""
	}
	return id + "'s report is clean, as tofu read it from the run: its completion is done, " + strings.Join(definition.Gate, " and ") +
		" ran after its last edit and passed, it is the only sub-agent this request delegated to, and it changed no file a browser shows. " +
		"so this turn checks nothing again: answer the person from the report in a few lines, and call no tool."
}
