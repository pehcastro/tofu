package turn

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"tofu/internal/judge/method"
	"tofu/internal/judge/state"
	"tofu/internal/konst"
	"tofu/internal/llm"
	"tofu/internal/rule"
	"tofu/internal/session"
	"tofu/internal/subagent"
	shipped "tofu/library"
)

type DepthLimitError struct {
	Depth int
	Limit int
}

func (e DepthLimitError) Error() string {
	return fmt.Sprintf("spawn refused: a sub-agent at depth %d would pass the sub-agent depth limit of %d", e.Depth, e.Limit)
}

type BreadthLimitError struct {
	Spawned int
	Limit   int
}

func (e BreadthLimitError) Error() string {
	return fmt.Sprintf("spawn refused: this turn has already spawned %d sub-agents and the sub-agent breadth limit is %d", e.Spawned, e.Limit)
}

const (
	subAgentDepthVar            = "TOFU_SUBAGENT_DEPTH"
	shippedSubAgentProcessDepth = 1
)

type ProcessDepthLimitError struct {
	Depth int
	Limit int
}

func (e ProcessDepthLimitError) Error() string {
	return fmt.Sprintf(
		"turn refused: this tofu process is %d deep inside another tofu turn and the sub-agent process limit is %d. "+
			"a turn whose shell runs tofu, whose shell runs tofu, has no bound and is a fork bomb: hand the work to spawn instead",
		e.Depth, e.Limit)
}

func processDepth() int {
	depth, _ := strconv.Atoi(os.Getenv(subAgentDepthVar))
	return depth
}

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

type SubAgentModel struct {
	Slug     string
	Windows  string
	Wire     string
	Spend    Spend
	Accounts Accounts
	Close    func()
}

type Spawned struct {
	ID      string
	Call    string
	Agent   string
	Slug    string
	Windows string
}

type spawnSiteKey struct{}

type spawnSite struct {
	log   *session.Log
	turn  string
	agent string
	call  string
}

func (r *record) site(call string) spawnSite {
	if r == nil {
		return spawnSite{call: call}
	}
	return spawnSite{log: r.log, turn: r.turn, agent: r.agent, call: call}
}

type spawnTrace struct {
	site       spawnSite
	definition string
	model      string
	mission    string
	owns       []string
	depth      int
}

func (s spawnTrace) run(outer, ctx context.Context, subAgent Config) (Row, error) {
	id, log, site := subAgent.NewID(), s.site.log, s.site
	if log == nil {
		return Run(ctx, subAgent)
	}
	var failed []error
	_, err := log.Append(session.Event{Turn: site.turn, Agent: site.agent, Call: site.call, Kind: session.EventSpawn},
		session.SpawnBody{Agent: id, Definition: s.definition, Model: s.model, Mission: s.mission, Owns: s.owns, Depth: s.depth})
	failed = append(failed, err, log.Edit(func(header *session.Header) {
		header.Agents = append(header.Agents, session.AgentRun{Agent: id, Definition: s.definition, Model: s.model, ParentAgent: site.agent,
			SpawnCall: site.call, SpawnTurn: site.turn, Depth: s.depth, Status: subagent.Working.String(), StartedAt: time.Now()})
	}))
	row, runErr := Run(ctx, subAgent)
	status := roundState(outer, runErr).String()
	ended := session.AgentEndBody{Status: status}
	for _, run := range log.Header().Agents {
		if run.Agent == id {
			ended.Usage, ended.CostUSD = run.Usage, run.CostUSD
		}
	}
	_, err = log.Append(session.Event{Turn: site.turn, Agent: id, Kind: session.EventAgentEnd}, ended)
	failed = append(failed, err, s.settle(id, status))
	if err := errors.Join(failed...); err != nil {
		row.Warnings = append(row.Warnings, "this sub-agent run was not wholly recorded: "+err.Error())
	}
	return row, runErr
}

func (s spawnTrace) settle(id, status string) error {
	if s.site.log == nil {
		return nil
	}
	at := time.Now()
	return s.site.log.Edit(func(header *session.Header) {
		for i := range header.Agents {
			if header.Agents[i].Agent == id {
				header.Agents[i].Status, header.Agents[i].EndedAt = status, &at
			}
		}
	})
}

type SubAgents struct {
	Defined []subagent.Definition
	Open    func(subagent.Definition) (SubAgentModel, error)
	Prompt  ComposeSpec
}

func (s SubAgents) prompt(inherited Config, definition subagent.Definition, task string, owns []string) (string, string, error) {
	system, environment := inherited.System, inherited.Environment
	switch {
	case s.Prompt.Environment != "":
		spec := s.Prompt
		spec.Task, spec.Paths, spec.Agent = task, owns, definition
		composed, err := Compose(spec)
		if err != nil {
			return "", "", err
		}
		system, environment = composed.Head(), composed.WithTaskRules(spec.Environment)
	case definition.Name != "":
		system += "\n\n" + agentPart(definition).Text
	}
	held := "the paths you hold, and the only ones write, edit and bash may change: " + strings.Join(owns, ", ")
	return system, strings.TrimSpace(environment + "\n\n" + held), nil
}

type AgentRefusedError struct {
	Name    string
	Why     string
	Enabled []string
}

func (e AgentRefusedError) Error() string {
	enabled := "no sub-agent is enabled, so leave agent out"
	if len(e.Enabled) > 0 {
		enabled = "the enabled sub-agents are " + strings.Join(e.Enabled, ", ")
	}
	return "spawn refused: " + e.Name + " " + e.Why + "; " + enabled
}

func enabledSubAgents(defined []subagent.Definition) []subagent.Definition {
	var enabled []subagent.Definition
	for _, definition := range defined {
		if definition.Runs == subagent.RunsModel || definition.Runs == subagent.RunsInherit {
			enabled = append(enabled, definition)
		}
	}
	return enabled
}

func (s SubAgents) enabledNames() []string {
	var names []string
	for _, definition := range enabledSubAgents(s.Defined) {
		names = append(names, definition.Name)
	}
	return names
}

func (s SubAgents) Named(name string) (subagent.Definition, error) {
	if name == "" {
		return subagent.Definition{}, nil
	}
	why := "is no sub-agent tofu found"
	for _, definition := range s.Defined {
		if definition.Name != name {
			continue
		}
		switch definition.Runs {
		case subagent.RunsModel, subagent.RunsInherit:
			return definition, nil
		case subagent.RunsDisabled:
			why = "is disabled by " + cmp.Or(definition.AssignedIn, definition.Path)
		case subagent.RunsRefused:
			why = "cannot run: " + strings.Join(definition.Refused, "; ")
		default:
			panic("turn: unknown sub-agent state " + string(definition.Runs))
		}
	}
	return subagent.Definition{}, AgentRefusedError{Name: name, Why: why, Enabled: s.enabledNames()}
}

type SpawnTool struct {
	Review         DoneReview
	Methods        method.Table
	SubAgents      SubAgents
	orchestratorID string
	depth          int
	spawned        int
	spend          float64
	base           Config
	roster         *subagent.Roster
	subAgentRows   []Row
	reports        []SubAgentReport
	ran            []Spawned
}

func NewSpawnTool(orchestratorID string, base Config, roster *subagent.Roster) *SpawnTool {
	return &SpawnTool{orchestratorID: orchestratorID, base: base, roster: roster}
}

func (t *SpawnTool) Name() string { return "spawn" }

func (t *SpawnTool) SubAgentRows() []Row { return t.subAgentRows }

func (t *SpawnTool) Reports() []SubAgentReport { return t.reports }

func (t *SpawnTool) Spawned() []Spawned { return t.ran }

func (t *SpawnTool) Definition() llm.Tool {
	properties := map[string]any{
		"task":    map[string]any{"type": "string"},
		"mission": map[string]any{"type": "string", "description": "the work in a handful of words, as a board entry reads: work on BOJI-395. the task is the brief and is kept whole"},
		"owns":    map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
	}
	if names := t.SubAgents.enabledNames(); len(names) > 0 {
		properties["agent"] = map[string]any{"type": "string", "enum": names,
			"description": "the sub-agent that does the work, on its own model with its own instructions. left out, the sub-agent runs on the orchestrator's model"}
	}
	return llm.Tool{
		Name: "spawn",
		Description: "you plan, spawn and verify, and implementation goes to a sub-agent: spawn one per separable piece of work as soon as the piece is known, rather than writing the code yourself first. " +
			"hands one piece of work to a sub-agent with its own context and its own conversation, and returns the sub-agent's report rather than its transcript. " +
			"owns lists the paths the sub-agent may write, every other path is refused at the write, and no two sub-agents may hold overlapping paths. " +
			"At most " + strconv.Itoa(konst.SubAgentMaxBreadth) + " sub-agents per turn, nested at most " + strconv.Itoa(konst.SubAgentMaxDepth) + " deep.",
		Parameters: map[string]any{
			"type":       "object",
			"properties": properties,
			"required":   []string{"task", "owns"},
		},
	}
}

type spawnArgs struct {
	Task    string   `json:"task"`
	Mission string   `json:"mission,omitempty"`
	Owns    []string `json:"owns"`
	Agent   string   `json:"agent,omitempty"`
}

func (a spawnArgs) mission() string {
	if given := strings.TrimSpace(a.Mission); given != "" {
		return given
	}
	first, _, _ := strings.Cut(strings.TrimSpace(a.Task), "\n")
	if runes := []rune(first); len(runes) > konst.SubAgentMissionChars {
		return strings.TrimSpace(string(runes[:konst.SubAgentMissionChars])) + "..."
	}
	return first
}

func (t *SpawnTool) Run(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args spawnArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, fmt.Errorf("spawn: arguments are not the expected shape: %w", err)
	}
	if strings.TrimSpace(args.Task) == "" {
		return Result{}, errors.New("spawn: task is required")
	}
	if len(args.Owns) == 0 {
		return Result{}, errors.New("spawn: owns is required, and a sub-agent holding no paths could write nothing")
	}
	if t.depth+1 > konst.SubAgentMaxDepth {
		return Result{}, DepthLimitError{Depth: t.depth + 1, Limit: konst.SubAgentMaxDepth}
	}
	if t.spawned >= konst.SubAgentMaxBreadth {
		return Result{}, BreadthLimitError{Spawned: t.spawned, Limit: konst.SubAgentMaxBreadth}
	}
	definition, err := t.SubAgents.Named(args.Agent)
	if err != nil {
		return Result{}, err
	}
	system, environment, err := t.SubAgents.prompt(t.base, definition, args.Task, args.Owns)
	if err != nil {
		return Result{}, fmt.Errorf("spawn: the sub-agent's prompt did not compose: %w", err)
	}
	var opened SubAgentModel
	if t.SubAgents.Open != nil {
		if opened, err = t.SubAgents.Open(definition); err != nil {
			return Result{}, fmt.Errorf("spawn: the model for %s did not open: %w", cmp.Or(definition.Name, "the sub-agent"), err)
		}
	}
	if opened.Close != nil {
		defer opened.Close()
	}

	clock := t.base.Now
	if clock == nil {
		clock = time.Now
	}
	site, _ := ctx.Value(spawnSiteKey{}).(spawnSite)
	var recorded []string
	if site.log != nil {
		for _, run := range site.log.Header().Agents {
			recorded = append(recorded, run.Agent)
		}
	}
	agent := subagent.SubAgent{
		ID:      t.roster.NextID(definition.Name, recorded),
		Agent:   definition.Name,
		Model:   opened.Slug,
		Mission: args.mission(),
		Brief:   args.Task,
		Owns:    args.Owns,
		Started: clock(),
	}
	subAgentID := agent.ID
	if err := t.roster.Hold(agent); err != nil {
		var collision subagent.CollisionError
		if errors.As(err, &collision) && collision.HolderReport != "" {
			return Result{Command: "handback " + collision.Holder, Content: fmt.Sprintf(
				"no sub-agent was started: %s already holds %q, and %q overlaps it. Send this work to %s rather than starting a rival.\n\n%s has reported:\n%s",
				collision.Holder, collision.HolderGlob, collision.Glob, collision.Holder, collision.Holder, collision.HolderReport)}, nil
		}
		return Result{}, fmt.Errorf("spawn: %w", err)
	}

	boundary := &subagent.Boundary{Ticket: subAgentID, Owns: args.Owns}
	offered := func(name string) bool { return len(definition.Tools) == 0 || slices.Contains(definition.Tools, name) }
	var owned []Tool
	for _, tool := range t.base.Tools.tools {
		switch {
		case tool.Name() == "write" || tool.Name() == "edit":
			tool = ownedTool{tool: tool, boundary: boundary}
		case !offered(tool.Name()):
			continue
		case tool.Name() == "bash":
			tool = ownedShell{tool: tool, boundary: boundary}
		}
		owned = append(owned, tool)
	}
	nested := &SpawnTool{Review: t.Review, Methods: t.Methods, SubAgents: t.SubAgents, orchestratorID: subAgentID, depth: t.depth + 1, base: t.base, roster: t.roster}
	if offered(t.Name()) {
		owned = append(owned, nested)
	}
	subAgent := t.base
	if opened.Accounts.Pick != nil {
		subAgent.Model, subAgent.Accounts, subAgent.Spend, subAgent.Wire = nil, opened.Accounts, opened.Spend, opened.Wire
	}
	subAgent.System, subAgent.Environment = system, environment
	trace := spawnTrace{site: site, definition: agent.Agent, model: agent.Model, mission: agent.Mission, owns: args.Owns, depth: t.depth + 1}
	subAgent.Task = args.Task
	subAgent.Tools = NewRegistry(owned...)
	subAgent.NewID = func() string { return subAgentID }
	subAgent.SpawnedFrom = t.orchestratorID
	subAgent.Session, subAgent.Log, subAgent.Turn, subAgent.SpawnedBy = "", site.log, site.turn, site.call
	if site.log == nil {
		subAgent.Sessions = nil
	}
	subAgent.Boundary = boundary
	subAgent.Step = func(step StepRow) {
		called := make([]string, len(step.ToolCalls))
		for i, call := range step.ToolCalls {
			called[i] = call.Tool
		}
		t.roster.Stepped(subAgentID, step.Index, clock(), called...)
		if t.base.Step != nil {
			t.base.Step(step)
		}
	}

	subAgentCtx, release := context.WithCancel(ctx)
	defer release()
	t.spawned++
	claims, state, runErr := t.runRounds(ctx, subAgentCtx, agent, subAgentID, subAgent, trace)
	asked := boundary.Asked()
	if len(asked) > 0 && state != subagent.Errored && state != subagent.Parked {
		state = subagent.WaitingAnswer
		t.publish(agent, claims, state)
	}
	if err := trace.settle(claims[len(claims)-1].ID, state.String()); err != nil {
		claims[len(claims)-1].Warnings = append(claims[len(claims)-1].Warnings, "the sub-agent's last state was not recorded: "+err.Error())
	}
	t.retain(append(claims, nested.subAgentRows...))
	t.ran = append(append(t.ran, Spawned{ID: subAgentID, Call: site.call, Agent: definition.Name, Slug: opened.Slug, Windows: opened.Windows}), nested.ran...)
	for _, claim := range claims {
		t.spend += claim.TotalCostUSD
	}

	report := reportOf(agent, claims, state)
	report.Asked = asked
	t.reports = append(t.reports, report)
	contract := subagent.BuildContract(agent.Brief, report.Prose, stoppedEarly(state, claims[len(claims)-1].Outcome))
	contract.Wrote = report.Wrote
	text := report.Text() + "\n\n" + contract.Block()
	if opened.Slug != "" {
		text = subAgentID + " ran as " + cmp.Or(definition.Name, "the unnamed sub-agent") + " on " + opened.Slug + "\n\n" + text
	}
	t.roster.Reached(subAgentID, state, text)
	if runErr != nil && state != subagent.Parked {
		return Result{}, fmt.Errorf("spawn: sub-agent %s is %s: %w", subAgentID, state, runErr)
	}
	return Result{Content: text, Command: subAgentID + " " + state.String() + ": " + agent.Mission}, nil
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

func (t *SpawnTool) retain(rows []Row) {
	t.subAgentRows = append(t.subAgentRows, rows...)
	for i := range len(t.subAgentRows) - konst.SubAgentRetainedRows {
		released := t.subAgentRows[i].Summary()
		released.Conversation = nil
		t.subAgentRows[i] = released
	}
}

func roundState(outerCtx context.Context, runErr error) subagent.State {
	switch {
	case outerCtx.Err() != nil:
		return subagent.Parked
	case runErr != nil:
		return subagent.Errored
	default:
		return subagent.InReview
	}
}

func resumable(messages []llm.Message) []llm.Message {
	stripped := slices.Clone(messages)
	for i := range stripped {
		stripped[i].Thinking = llm.Thinking{}
	}
	return Sendable(stripped)
}

func (t *SpawnTool) publish(agent subagent.SubAgent, claims []Row, state subagent.State) {
	t.roster.Reached(agent.ID, state, reportOf(agent, claims, state).Text())
}

func (t *SpawnTool) runRounds(outerCtx, subAgentCtx context.Context, agent subagent.SubAgent, subAgentID string, subAgent Config, trace spawnTrace) ([]Row, subagent.State, error) {
	first, firstErr := trace.run(outerCtx, subAgentCtx, subAgent)
	claims := []Row{first}
	state := roundState(outerCtx, firstErr)
	t.publish(agent, claims, state)
	history := append(slices.Clone(subAgent.History), resumable(first.Conversation)...)
	for state == subagent.InReview && t.Review != nil {
		last := &claims[len(claims)-1]
		reviewed := *last
		reviewed.Task = agent.Brief
		decision, err := t.decided(subAgentCtx, reviewed)
		if err != nil {
			last.Warnings = append(last.Warnings, "the done review did not run, so the sub-agent's own claim stands: "+err.Error())
			return claims, subagent.InReview, firstErr
		}
		if decision.ID != "" {
			last.DecisionIDs = append(last.DecisionIDs, decision.ID)
		}
		if decision.Verdict == DoneAccepted {
			state = subagent.Finished
			t.publish(agent, claims, state)
			return claims, state, firstErr
		}
		if decision.Verdict != DoneReopen {
			panic("turn: unknown done verdict " + string(decision.Verdict))
		}
		next, reopenErr := t.roster.Reopen(subAgentID, decision.Reason)
		if reopenErr != nil {
			last.Warnings = append(last.Warnings, reopenErr.Error())
			t.publish(agent, claims, subagent.InReview)
			return claims, subagent.InReview, firstErr
		}
		t.roster.Reached(subAgentID, subagent.Working, "")
		subAgent.History = history
		subAgent.Task = "You reported this finished and the done review did not believe you: " + decision.Reason
		subAgent.NewID = func() string { return subAgentID + "-r" + strconv.Itoa(next) }
		reRow, reErr := trace.run(outerCtx, subAgentCtx, subAgent)
		claims = append(claims, reRow)
		history = append(slices.Clone(history), resumable(reRow.Conversation)...)
		if reErr != nil {
			claims[len(claims)-1].Warnings = append(claims[len(claims)-1].Warnings,
				"the sub-agent was re-opened and did not run again, so its earlier claim stands: "+reErr.Error())
			state = subagent.Errored
		} else {
			state = roundState(outerCtx, nil)
		}
		t.publish(agent, claims, state)
	}
	return claims, state, firstErr
}

type ownedTool struct {
	tool     Tool
	boundary *subagent.Boundary
}

func (t ownedTool) Name() string { return t.tool.Name() }

func (t ownedTool) Definition() llm.Tool {
	definition := t.tool.Definition()
	definition.Description += ", and only inside the paths your first message says you hold"
	return definition
}

func (t ownedTool) Run(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, fmt.Errorf("%s: arguments are not the expected shape: %w", t.Name(), err)
	}
	if err := t.boundary.Write(args.Path); err != nil {
		return Result{}, fmt.Errorf("%s: %w", t.Name(), err)
	}
	return t.tool.Run(ctx, raw)
}

type ownedShell struct {
	tool     Tool
	boundary *subagent.Boundary
}

func (t ownedShell) Name() string { return t.tool.Name() }

func (t ownedShell) Definition() llm.Tool {
	definition := t.tool.Definition()
	definition.Description += ", and every file the command writes, through a redirect, tee, cp, mv or sed -i, has to be inside the paths your first message says you hold. " +
		"Reading anything is fine. A command writing outside them is refused before it runs, and that work goes back to the orchestrator."
	return definition
}

func (t ownedShell) Run(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args bashArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, fmt.Errorf("%s: arguments are not the expected shape: %w", t.Name(), err)
	}
	if err := t.boundary.Shell(args.Command); err != nil {
		return Result{}, fmt.Errorf("%s: %w", t.Name(), err)
	}
	return t.tool.Run(ctx, raw)
}

type SourceBudgetError struct {
	Tool       string
	Path       string
	Lines      int
	Spent      int
	Spawn      []string
	ShellWrite bool
}

func (e SourceBudgetError) Error() string {
	if e.ShellWrite {
		return fmt.Sprintf("%s: %s is source code, and the orchestrator never writes source through the shell: use edit for a fix of up to %d lines, or spawn %s with this work",
			e.Tool, e.Path, konst.OrchestratorSourceLinesPerTurn, strings.Join(e.Spawn, " or "))
	}
	return fmt.Sprintf("%s: %s is source code, and this change of %d lines would pass the orchestrator's budget of %d changed source lines in one turn, %d of which are spent: spawn %s with this work instead",
		e.Tool, e.Path, e.Lines, konst.OrchestratorSourceLinesPerTurn, e.Spent, strings.Join(e.Spawn, " or "))
}

func sourceLanguage(path string) string {
	language := rule.LanguageOf(path)
	if language == "markdown" || language == "yaml" {
		return ""
	}
	return language
}

func linesIn(text string) int {
	if text == "" {
		return 0
	}
	return strings.Count(strings.TrimSuffix(text, "\n"), "\n") + 1
}

type sourceBudget struct {
	spent   int
	enabled []subagent.Definition
}

type budgetedTool struct {
	tool   Tool
	budget *sourceBudget
}

func WithSourceBudget(tools []Tool, defined []subagent.Definition) []Tool {
	budget := &sourceBudget{enabled: enabledSubAgents(defined)}
	wrapped := slices.Clone(tools)
	for i, tool := range wrapped {
		if slices.Contains([]string{"write", "edit", "bash"}, tool.Name()) {
			wrapped[i] = budgetedTool{tool: tool, budget: budget}
		}
	}
	return wrapped
}

func (t budgetedTool) Name() string { return t.tool.Name() }

func (t budgetedTool) Definition() llm.Tool {
	definition := t.tool.Definition()
	definition.Description += fmt.Sprintf(". as the orchestrator you change at most %d lines of source code in one turn, through write and edit only, and never write source through the shell; "+
		"markdown, notes, plans, configuration and data are free, and past the budget a source write is refused and goes to a sub-agent", konst.OrchestratorSourceLinesPerTurn)
	return definition
}

func (t budgetedTool) Run(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		Path    string `json:"path"`
		Content string `json:"content"`
		Old     string `json:"old_string"`
		New     string `json:"new_string"`
		Command string `json:"command"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return t.tool.Run(ctx, raw)
	}
	lines, shellWrite := max(linesIn(args.Content), linesIn(args.Old), linesIn(args.New)), false
	if t.Name() == "bash" {
		written, err := subagent.ShellWrites(args.Command)
		if err != nil {
			return Result{}, fmt.Errorf("%s: %w", t.Name(), err)
		}
		for _, path := range written {
			if sourceLanguage(path) != "" {
				args.Path, shellWrite = path, true
			}
		}
	}
	language := sourceLanguage(args.Path)
	if language == "" {
		return t.tool.Run(ctx, raw)
	}
	if shellWrite || t.budget.spent+lines > konst.OrchestratorSourceLinesPerTurn {
		refusal := SourceBudgetError{Tool: t.Name(), Path: args.Path, Lines: lines, Spent: t.budget.spent, ShellWrite: shellWrite}
		for _, definition := range t.budget.enabled {
			if definition.Language == language {
				refusal.Spawn = append(refusal.Spawn, definition.Name)
			}
		}
		if len(refusal.Spawn) == 0 {
			refusal.Spawn = []string{"a sub-agent"}
		}
		return Result{}, refusal
	}
	result, err := t.tool.Run(ctx, raw)
	if err == nil && result.FailureText == "" {
		t.budget.spent += lines
	}
	return result, err
}
