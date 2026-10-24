package turn

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"tofu/internal/boardy"
	"tofu/internal/judge/method"
	"tofu/internal/judge/state"
	"tofu/internal/llm"
	"tofu/internal/rule"
	"tofu/internal/session"
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
	lines := acceptanceOf(subAgent)
	var unmet []string
	for i, line := range lines {
		if line.State == boardy.LineUnmet {
			unmet = append(unmet, fmt.Sprintf("line %d, %s", i+1, line.Line))
		}
	}
	switch {
	case lines == nil:
		return DoneDecision{Verdict: DoneAccepted, Reason: "no ticket, so no acceptance lines and nothing to evaluate"}, nil
	case outcomeStoppedEarly(subAgent.Outcome):
		return DoneDecision{Verdict: DoneAccepted, Reason: "the sub-agent was stopped before it finished, so its unmet lines are not held against it"}, nil
	case len(unmet) > 0:
		return DoneDecision{Verdict: DoneReopen, Reason: fmt.Sprintf("%d of %d acceptance lines have no evidence entry naming them and are not left for the lead: %s",
			len(unmet), len(lines), strings.Join(unmet, "; "))}, nil
	}
	return DoneDecision{Verdict: DoneAccepted, Reason: fmt.Sprintf("all %d acceptance lines are met or left for the lead", len(lines))}, nil
}

const (
	TicketLogToolName  = "ticket_log"
	TicketMoveToolName = "ticket_move"
)

func acceptanceOf(subAgent Row) []boardy.AcceptanceLine {
	lines := subagent.AcceptanceLines(subAgent.Task)
	if subagent.TicketID(subAgent.Task) == "" || len(lines) == 0 {
		return nil
	}
	var entries []boardy.Event
	for _, step := range subAgent.Steps {
		for _, call := range step.ToolCalls {
			var args struct {
				Kind, To string
				Line     int
				Leave    []int
			}
			if call.Error != "" || json.Unmarshal(call.Args, &args) != nil {
				continue
			}
			switch {
			case call.Tool == TicketLogToolName && args.Kind == string(boardy.Evidence):
				entries = append(entries, boardy.Event{Kind: boardy.Evidence, Line: args.Line})
			case call.Tool == TicketMoveToolName && args.To == string(boardy.Review):
				entries = append(entries, boardy.Event{Kind: boardy.Moved, To: boardy.Review, Leave: args.Leave})
			}
		}
	}
	return boardy.Accounted(lines, entries, nil)
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
	case OutcomeStepCap, OutcomeRetiredCostCap, OutcomeRetiredWallClockCap, OutcomeDecisionCap, OutcomeCancelled:
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
	spawned := t.writeMoves(subAgent, agent.ID)
	first, firstErr := Run(subAgentCtx, subAgent)
	first.Warnings = append(first.Warnings, spawned...)
	claims, state := []Row{first}, roundState(outerCtx, firstErr)
	held.remember(first)
	var sentBack []string
	recipes := projectRecipes(t.Project)
	for state == subagent.Finished {
		last := &claims[len(claims)-1]
		why, task := t.doneReview(subAgentCtx, held, claims, recipes)
		last.Warnings = append(last.Warnings, t.writeMoves(subAgent, agent.ID)...)
		if why == "" {
			break
		}
		if _, err := t.roster.Reopen(agent.ID, why); err != nil {
			last.Warnings = append(last.Warnings, err.Error())
			break
		}
		last.Warnings = append(last.Warnings, t.writeMoves(subAgent, agent.ID)...)
		t.roster.Reached(agent.ID, subagent.Working, "sent back: "+why)
		sentBack = append(sentBack, why)
		subAgent.History = held.history
		subAgent.Task = task
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

func (t *SpawnTool) doneReview(ctx context.Context, held *heldSubAgent, claims []Row, recipes map[string]string) (why, task string) {
	const notBelieved = "You reported this finished and the done review did not believe you: "
	last := &claims[len(claims)-1]
	if missed := gateMissed(t.Project, recipes, held.definition, held.boundary.Owns(), claims); len(missed) > 0 {
		why = strings.Join(missed, "; ")
		return why, notBelieved + fmt.Sprintf("you changed a %s file, and your gate is %s, each run after your last edit and passing: %s",
			held.definition.Language, strings.Join(held.definition.Gate, " and "), why)
	}
	if t.Review == nil {
		return "", ""
	}
	t.roster.Reached(held.agent.ID, subagent.InReview, reportOf(held.agent, held.forkedSoFar(), []Row{wholeRun(claims)}, subagent.InReview).Text(t.proseStore()))
	t.roster.Moved(held.agent.ID, subagent.Reviewed, "")
	reviewed := *last
	reviewed.Task = held.agent.Brief
	decision, err := t.decided(ctx, reviewed)
	if err != nil {
		last.Warnings = append(last.Warnings, "the done review did not run, so the sub-agent's own claim stands: "+err.Error())
		return "", ""
	}
	if decision.ID != "" {
		last.DecisionIDs = append(last.DecisionIDs, decision.ID)
	}
	switch decision.Verdict {
	case DoneAccepted:
		t.roster.Moved(held.agent.ID, subagent.Accepted, decision.Reason)
		return "", ""
	case DoneReopen:
		return decision.Reason, notBelieved + decision.Reason
	}
	panic("turn: unknown done verdict " + string(decision.Verdict))
}

type transitionNotice struct {
	session.NoticeBody
	Transition subagent.Transition `json:"transition"`
}

func (t *SpawnTool) writeMoves(subAgent Config, id string) []string {
	moves := t.roster.Unwritten(id)
	if subAgent.Log == nil {
		return nil
	}
	spawner := ""
	if t.depth > 0 {
		spawner = t.orchestratorID
	}
	var failed []string
	for _, move := range moves {
		text := fmt.Sprintf("%s %s, round %d", move.Agent, move.Move, move.Round)
		if move.Reason != "" {
			text += ": " + move.Reason
		}
		notice := transitionNotice{NoticeBody: session.NoticeBody{Text: text, From: RecordedOrigin(llm.Origin{Source: llm.SourceNotice})}, Transition: move}
		if _, err := subAgent.Log.Append(session.Event{Turn: subAgent.Turn, Agent: spawner, Kind: session.EventNotice}, notice); err != nil {
			failed = append(failed, "the "+string(move.Move)+" transition was not recorded: "+err.Error())
		}
	}
	return failed
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
