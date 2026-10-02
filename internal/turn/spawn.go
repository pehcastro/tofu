package turn

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"tofu/internal/judge/method"
	"tofu/internal/judge/state"
	"tofu/internal/konst"
	"tofu/internal/llm"
	"tofu/internal/rule"
	"tofu/internal/session"
	"tofu/internal/settings"
	"tofu/internal/shell"
	"tofu/internal/subagent"
	shipped "tofu/library"
)

type DepthLimitError struct {
	Depth int
	Limit int
}

func (e DepthLimitError) Error() string {
	return fmt.Sprintf("spawn refused: a sub-agent at depth %d would pass the %s setting of %d, which the person can raise in the settings menu",
		e.Depth, settings.SubAgentDepth, e.Limit)
}

type BreadthLimitError struct {
	Spawned int
	Limit   int
}

func (e BreadthLimitError) Error() string {
	return fmt.Sprintf("spawn refused: this turn has already spawned %d sub-agents and the %s setting is %d, which the person can raise in the settings menu",
		e.Spawned, settings.SubAgentsPerTurn, e.Limit)
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
	Effort   llm.Effort
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
	log          *session.Log
	turn         string
	agent        string
	call         string
	conversation []llm.Message
}

func (r *record) site(call string, conversation []llm.Message) spawnSite {
	sofar := conversation[:len(conversation):len(conversation)]
	if r == nil {
		return spawnSite{call: call, conversation: sofar}
	}
	return spawnSite{log: r.log, turn: r.turn, agent: r.agent, call: call, conversation: sofar}
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
	Open    func(subagent.Definition, llm.Effort) (SubAgentModel, error)
	Prompt  ComposeSpec
	Brief   func(definition subagent.Definition, task string) string
	Ended   func(definition subagent.Definition, task string, rounds []Row, report SubAgentReport, finished bool)
}

func (s SubAgents) prompt(inherited Config, definition subagent.Definition, task string, owns []string) (string, string, error) {
	system, environment := inherited.System, inherited.Environment
	switch {
	case s.Prompt.Environment != "":
		spec := s.Prompt
		spec.Task, spec.Paths, spec.Agent, spec.Role = task, owns, definition, rule.RoleSubAgent
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

type SubAgentLimits struct {
	PerTurn int
	Depth   int
}

type SpawnTool struct {
	Review         DoneReview
	Methods        method.Table
	SubAgents      SubAgents
	Limits         func() SubAgentLimits
	SettingsTool   bool
	orchestratorID string
	depth          int
	mu             sync.Mutex
	tree           *sync.Mutex
	spawned        int
	spend          float64
	base           Config
	roster         *subagent.Roster
	subAgentRows   []Row
	reports        []SubAgentReport
	ran            []Spawned
	warmups        map[string]*warmup
	held           map[string]*heldSubAgent
}

type warmup struct {
	leader string
	once   sync.Once
	ready  chan struct{}
}

func (w *warmup) warmed() { w.once.Do(func() { close(w.ready) }) }

type warmingModel struct {
	model  Model
	warmup *warmup
}

func (m warmingModel) Ask(ctx context.Context, request llm.Request) (llm.Decision, error) {
	decision, err := m.model.Ask(ctx, request)
	m.warmup.warmed()
	return decision, err
}

func (w *warmup) stagger(ctx context.Context, call string, subAgent Config) Config {
	switch {
	case w == nil:
	case w.leader == call:
		if subAgent.Model != nil {
			subAgent.Model = warmingModel{model: subAgent.Model, warmup: w}
		}
		if pick := subAgent.Accounts.Pick; pick != nil {
			subAgent.Accounts.Pick = func(ctx context.Context) (Account, error) {
				account, err := pick(ctx)
				if account.Model != nil {
					account.Model = warmingModel{model: account.Model, warmup: w}
				}
				return account, err
			}
		}
	default:
		select {
		case <-w.ready:
		case <-time.After(konst.SubAgentWarmMillis * time.Millisecond):
		case <-ctx.Done():
		}
	}
	return subAgent
}

func NewSpawnTool(orchestratorID string, base Config, roster *subagent.Roster) *SpawnTool {
	return &SpawnTool{orchestratorID: orchestratorID, base: base, roster: roster, tree: &sync.Mutex{}}
}

func (t *SpawnTool) Name() string { return "spawn" }

func (t *SpawnTool) SubAgentRows() []Row {
	t.mu.Lock()
	defer t.mu.Unlock()
	return slices.Clone(t.subAgentRows)
}

func (t *SpawnTool) Reports() []SubAgentReport {
	t.mu.Lock()
	defer t.mu.Unlock()
	return slices.Clone(t.reports)
}

func (t *SpawnTool) Spawned() []Spawned {
	t.mu.Lock()
	defer t.mu.Unlock()
	return slices.Clone(t.ran)
}

func (t *SpawnTool) reserve(limit int) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.spawned >= limit {
		return BreadthLimitError{Spawned: t.spawned, Limit: limit}
	}
	t.spawned++
	return nil
}

func (t *SpawnTool) disjointPrefix(calls []llm.ToolCall) int {
	limit := t.limits().PerTurn
	var wave subagent.Roster
	width, firsts, warmups := len(calls), map[string]*warmup{}, map[string]*warmup{}
	for i, call := range calls {
		var args spawnArgs
		if i == limit || call.Name != t.Name() || json.Unmarshal(call.Arguments, &args) != nil ||
			wave.Hold(subagent.SubAgent{ID: call.ID, Owns: args.Owns}) != nil {
			width = i
			break
		}
		first, sibling := firsts[args.Agent]
		switch {
		case args.Agent == "":
		case sibling:
			warmups[first.leader], warmups[call.ID] = first, first
		default:
			firsts[args.Agent] = &warmup{leader: call.ID, ready: make(chan struct{})}
		}
	}
	t.mu.Lock()
	t.warmups = warmups
	t.mu.Unlock()
	return width
}

func writesPaths(tool string) bool { return tool == "write" || tool == "edit" || tool == "bash" }

func (t *SpawnTool) limits() SubAgentLimits {
	if t.Limits == nil {
		return SubAgentLimits{PerTurn: konst.SubAgentsPerTurnDefault, Depth: konst.SubAgentDepthDefault}
	}
	return t.Limits()
}

func (t *SpawnTool) Definition() llm.Tool {
	limits := t.limits()
	raise := "the person can raise either in the settings menu."
	if t.SettingsTool {
		raise = "the person can raise either in the settings menu, and you can ask to with the settings tool, which the person answers."
	}
	properties := map[string]any{
		"task":    map[string]any{"type": "string"},
		"mission": map[string]any{"type": "string", "description": "the work in a handful of words, as a board entry reads: work on BOJI-395. the task is the brief and is kept whole"},
		"owns":    map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
	}
	var levels []string
	for _, level := range llm.Efforts() {
		levels = append(levels, string(level))
	}
	properties["effort"] = map[string]any{"type": "string", "enum": levels,
		"description": "how hard the sub-agent thinks. low unless it must make design decisions the brief does not settle. left out, it thinks as hard as you do"}
	if names := t.SubAgents.enabledNames(); len(names) > 0 {
		properties["agent"] = map[string]any{"type": "string", "enum": names,
			"description": "the sub-agent that does the work, on its own model with its own instructions. left out, the sub-agent runs on the orchestrator's model"}
	}
	return llm.Tool{
		Name: "spawn",
		Description: "you plan, spawn and verify, and implementation goes to a sub-agent: spawn one per separable piece of work as soon as the piece is known, rather than writing the code yourself first. " +
			"hands one piece of work to a sub-agent with its own context and its own conversation, and returns the sub-agent's report rather than its transcript. " +
			"owns lists the paths the sub-agent may write, every other path is refused at the write, and no two sub-agents may hold overlapping paths; a sub-agent offered no write, edit or bash needs none. " +
			fmt.Sprintf("At most %d sub-agents per turn, nested at most %d deep. These are the person's settings %s and %s: %s",
				limits.PerTurn, limits.Depth, settings.SubAgentsPerTurn, settings.SubAgentDepth, raise),
		Parameters: map[string]any{
			"type":       "object",
			"properties": properties,
			"required":   []string{"task"},
		},
	}
}

type spawnArgs struct {
	Task    string   `json:"task"`
	Mission string   `json:"mission,omitempty"`
	Owns    []string `json:"owns"`
	Agent   string   `json:"agent,omitempty"`
	Effort  string   `json:"effort,omitempty"`
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
	site, _ := ctx.Value(spawnSiteKey{}).(spawnSite)
	t.mu.Lock()
	warm := t.warmups[site.call]
	t.mu.Unlock()
	if warm != nil && warm.leader == site.call {
		defer warm.warmed()
	}
	if strings.TrimSpace(args.Task) == "" {
		return Result{}, errors.New("spawn: task is required")
	}
	limits := t.limits()
	if t.depth+1 > limits.Depth {
		return Result{}, DepthLimitError{Depth: t.depth + 1, Limit: limits.Depth}
	}
	if err := t.reserve(limits.PerTurn); err != nil {
		return Result{}, err
	}
	started := false
	defer func() {
		if !started {
			t.mu.Lock()
			t.spawned--
			t.mu.Unlock()
		}
	}()
	definition, err := t.SubAgents.Named(args.Agent)
	if err != nil {
		return Result{}, err
	}
	if len(args.Owns) == 0 && (len(definition.Tools) == 0 || slices.ContainsFunc(definition.Tools, writesPaths)) {
		return Result{}, errors.New("spawn: owns is required, and a sub-agent holding no paths could write nothing")
	}
	system, environment, err := t.SubAgents.prompt(t.base, definition, args.Task, args.Owns)
	if err != nil {
		return Result{}, fmt.Errorf("spawn: the sub-agent's prompt did not compose: %w", err)
	}
	var effort llm.Effort
	if args.Effort != "" {
		if effort, err = llm.ParseEffort(args.Effort); err != nil {
			return Result{}, fmt.Errorf("spawn: %w", err)
		}
	}
	opened, err := t.open(definition, effort)
	if err != nil {
		return Result{}, fmt.Errorf("spawn: %w", err)
	}
	if opened.Close != nil {
		defer opened.Close()
	}

	clock := t.base.Now
	if clock == nil {
		clock = time.Now
	}
	var recorded []string
	if site.log != nil {
		for _, run := range site.log.Header().Agents {
			recorded = append(recorded, run.Agent)
		}
	}
	t.tree.Lock()
	agent := subagent.SubAgent{
		ID:      t.roster.NextID(definition.Name, recorded),
		Agent:   definition.Name,
		Model:   opened.Slug,
		Mission: args.mission(),
		Brief:   args.Task,
		Owns:    args.Owns,
		Started: clock(),
	}
	subAgentID, holding := agent.ID, t.roster.Hold(agent)
	t.tree.Unlock()
	if err := holding; err != nil {
		var collision subagent.CollisionError
		if errors.As(err, &collision) && collision.HolderReport != "" {
			return Result{Command: "handback " + collision.Holder, Content: fmt.Sprintf(
				"no sub-agent was started: %s already holds %q, and %q overlaps it. Send this work to %s with the message tool rather than starting a rival.\n\n%s has reported:\n%s",
				collision.Holder, collision.HolderGlob, collision.Glob, collision.Holder, collision.Holder, collision.HolderReport)}, nil
		}
		return Result{}, fmt.Errorf("spawn: %w", err)
	}

	boundary := &subagent.Boundary{Ticket: subAgentID, Owns: args.Owns}
	offered := func(name string) bool { return len(definition.Tools) == 0 || slices.Contains(definition.Tools, name) }
	var owned []Tool
	for _, tool := range t.base.Tools.tools {
		switch {
		case !offered(tool.Name()):
			continue
		case tool.Name() == "write" || tool.Name() == "edit":
			tool = ownedTool{tool: tool, boundary: boundary}
		case tool.Name() == "bash":
			tool = ownedShell{tool: tool, boundary: boundary}
		}
		owned = append(owned, tool)
	}
	nested := &SpawnTool{Review: t.Review, Methods: t.Methods, SubAgents: t.SubAgents, Limits: t.Limits, orchestratorID: subAgentID, depth: t.depth + 1, base: t.base, roster: t.roster, tree: t.tree}
	if offered(t.Name()) {
		owned = append(owned, nested)
	}
	subAgent := t.base
	subAgent.Caps.MaxSteps, subAgent.Caps.MaxForks = cmp.Or(subAgent.Caps.MaxSteps, konst.SubAgentMaxSteps), konst.SubAgentMaxForks
	subAgent.System, subAgent.Environment = system, environment+t.briefFiles(ctx, args.Task)
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
			t.tree.Lock()
			t.base.Step(step)
			t.tree.Unlock()
		}
	}
	held := &heldSubAgent{agent: agent, definition: definition, effort: opened.Effort, config: subAgent, tools: owned, nested: nested,
		trace: spawnTrace{definition: agent.Agent, model: agent.Model, mission: agent.Mission, owns: args.Owns, depth: t.depth + 1}}
	t.mu.Lock()
	if t.held == nil {
		t.held = map[string]*heldSubAgent{}
	}
	t.held[subAgentID] = held
	t.ran = append(t.ran, Spawned{ID: subAgentID, Call: site.call, Agent: definition.Name, Slug: opened.Slug, Windows: opened.Windows})
	t.mu.Unlock()
	subAgent.Task = args.Task
	if t.SubAgents.Brief != nil {
		subAgent.Task += t.SubAgents.Brief(definition, args.Task)
	}
	subAgent.NewID = func() string { return subAgentID }
	started = true
	return t.converse(ctx, held, warm.stagger(ctx, site.call, opened.onto(subAgent)), site)
}

type heldSubAgent struct {
	agent      subagent.SubAgent
	definition subagent.Definition
	effort     llm.Effort
	config     Config
	tools      []Tool
	nested     *SpawnTool
	trace      spawnTrace
	history    []llm.Message
	messages   int
	nestedRows int
	nestedRan  int
	forking    sync.Mutex
	forked     []Row
}

func (h *heldSubAgent) forkedSoFar() []Row {
	h.forking.Lock()
	defer h.forking.Unlock()
	return slices.Clone(h.forked)
}

func (h *heldSubAgent) remember(round Row) {
	if len(round.Conversation) > 0 {
		h.history = resumable(round.Conversation)
	}
}

func (t *SpawnTool) open(definition subagent.Definition, effort llm.Effort) (SubAgentModel, error) {
	switch {
	case t.SubAgents.Open == nil && effort != "":
		return SubAgentModel{}, fmt.Errorf("effort %s: this run opens no model of its own for a sub-agent, so the level would be dropped without a word", effort)
	case t.SubAgents.Open == nil:
		return SubAgentModel{}, nil
	}
	opened, err := t.SubAgents.Open(definition, effort)
	if err != nil {
		return SubAgentModel{}, fmt.Errorf("the model for %s did not open: %w", cmp.Or(definition.Name, "the sub-agent"), err)
	}
	return opened, nil
}

func (m SubAgentModel) onto(subAgent Config) Config {
	if m.Accounts.Pick != nil {
		subAgent.Model, subAgent.Accounts, subAgent.Spend, subAgent.Wire = nil, m.Accounts, m.Spend, m.Wire
	}
	return subAgent
}

func (t *SpawnTool) converse(ctx context.Context, held *heldSubAgent, subAgent Config, site spawnSite) (Result, error) {
	agent, trace := held.agent, held.trace
	trace.site = site
	subAgent.Tools = NewRegistry(append(slices.Clone(held.tools), askTool{orchestrator: t, asking: agent, conversation: site.conversation})...)
	subAgentCtx, release := context.WithCancel(ctx)
	defer release()
	held.forked = nil
	written := subAgent.EndedSession
	subAgent.EndedSession = func(ended Row) error {
		held.forking.Lock()
		held.forked = append(held.forked, ended)
		held.forking.Unlock()
		if written == nil {
			return nil
		}
		return written(ended)
	}
	claims, state, runErr := t.runRounds(ctx, subAgentCtx, held, subAgent, trace)
	forked := held.forkedSoFar()
	asked := subAgent.Boundary.Asked()
	if len(asked) > 0 && state != subagent.Errored && state != subagent.Parked {
		state = subagent.WaitingAnswer
	}
	last := &claims[len(claims)-1]
	if err := trace.settle(last.ID, state.String()); err != nil {
		last.Warnings = append(last.Warnings, "the sub-agent's last state was not recorded: "+err.Error())
	}
	report := reportOf(agent, forked, claims, state)
	report.Asked = asked
	nestedRows, nestedRan := held.nested.SubAgentRows(), held.nested.Spawned()
	t.mu.Lock()
	t.retain(append(claims, nestedRows[held.nestedRows:]...))
	t.ran = append(t.ran, nestedRan[held.nestedRan:]...)
	held.nestedRows, held.nestedRan = len(nestedRows), len(nestedRan)
	for _, claim := range append(slices.Clone(forked), claims...) {
		t.spend += claim.TotalCostUSD
	}
	t.reports = append(t.reports, report)
	t.mu.Unlock()
	if t.SubAgents.Ended != nil {
		t.SubAgents.Ended(held.definition, agent.Brief, append(forked, claims...), report, runErr == nil && state != subagent.Errored && !stoppedEarly(state, last.Outcome))
	}
	contract := subagent.BuildContract(agent.Brief, report.Prose, stoppedEarly(state, last.Outcome))
	contract.Wrote = report.Wrote
	text := report.Text() + "\n\n" + contract.Block()
	if agent.Model != "" {
		ran := agent.ID + " ran as " + cmp.Or(agent.Agent, "the unnamed sub-agent") + " on " + agent.Model
		if held.effort != "" {
			ran += " at effort " + string(held.effort)
		}
		text = ran + "\n\n" + text
	}
	t.roster.Reached(agent.ID, state, text)
	if runErr != nil && state != subagent.Parked {
		return Result{}, fmt.Errorf("sub-agent %s is %s: %w", agent.ID, state, runErr)
	}
	return Result{Content: text, Command: agent.ID + " " + state.String() + ": " + agent.Mission, SubAgent: agent.ID}, nil
}

type messageTool struct {
	orchestrator *SpawnTool
}

type messageArgs struct {
	To   string `json:"to"`
	Text string `json:"text"`
}

func (messageTool) Name() string { return "message" }

func (messageTool) Definition() llm.Tool {
	text := map[string]any{"type": "string"}
	return llm.Tool{
		Name: "message",
		Description: "sends a sub-agent that has stopped more work or a correction, and returns its answer as spawn returns a report. " +
			"it resumes with its whole conversation and the paths it held, so use it rather than spawning a new sub-agent for those paths. " +
			"to is the sub-agent's name as its report gives it, such as ts-dev-1",
		Parameters: map[string]any{
			"type":       "object",
			"properties": map[string]any{"to": text, "text": text},
			"required":   []string{"to", "text"},
		},
	}
}

func (m messageTool) Run(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args messageArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, fmt.Errorf("message: arguments are not the expected shape: %w", err)
	}
	if strings.TrimSpace(args.Text) == "" {
		return Result{}, errors.New("message: text is required")
	}
	t := m.orchestrator
	t.mu.Lock()
	held := t.held[args.To]
	t.mu.Unlock()
	if held == nil {
		return Result{}, t.unknown(args.To)
	}
	opened, err := t.open(held.definition, held.effort)
	if err != nil {
		return Result{}, fmt.Errorf("message: %w", err)
	}
	if opened.Close != nil {
		defer opened.Close()
	}
	t.tree.Lock()
	err = t.rehold(held.agent)
	t.tree.Unlock()
	if err != nil {
		return Result{}, fmt.Errorf("message refused: %w", err)
	}
	held.messages++
	round := held.agent.ID + "-m" + strconv.Itoa(held.messages)
	site, _ := ctx.Value(spawnSiteKey{}).(spawnSite)
	subAgent := opened.onto(held.config)
	subAgent.History, subAgent.Task, subAgent.SpawnedBy = held.history, args.Text, site.call
	subAgent.NewID = func() string { return round }
	return t.converse(ctx, held, subAgent, site)
}

func (t *SpawnTool) unknown(name string) error {
	var names []string
	for _, known := range t.roster.SubAgents() {
		if known.ID == name {
			return fmt.Errorf("message refused: %s was spawned in an earlier turn, and this turn does not hold its conversation to resume", name)
		}
		names = append(names, known.ID)
	}
	if len(names) == 0 {
		return fmt.Errorf("message refused: no sub-agent is named %q, and none has been spawned", name)
	}
	return fmt.Errorf("message refused: no sub-agent is named %q; the sub-agents are %s", name, strings.Join(names, ", "))
}

func (t *SpawnTool) rehold(agent subagent.SubAgent) error {
	var others subagent.Roster
	for _, other := range t.roster.SubAgents() {
		switch {
		case other.ID != agent.ID:
			if other.State != subagent.Finished && other.State != subagent.Errored {
				_ = others.Hold(other)
			}
		case other.State == subagent.Working || other.State == subagent.InReview || other.State == subagent.Reopened:
			return fmt.Errorf("%s is %s, and a running sub-agent cannot take a message until spawns run in the background", agent.ID, other.State)
		}
	}
	if err := others.Hold(agent); err != nil {
		return err
	}
	t.roster.Reached(agent.ID, subagent.Working, "")
	return nil
}

var briefPath = regexp.MustCompile(`[\w./-]*\w\.[A-Za-z0-9]+`)

func (t *SpawnTool) briefFiles(ctx context.Context, brief string) string {
	read, readable := t.base.Tools.byName["read"]
	if !readable {
		return ""
	}
	var text strings.Builder
	var skipped, named []string
	for _, path := range briefPath.FindAllString(brief, -1) {
		if slices.Contains(named, path) {
			continue
		}
		named = append(named, path)
		raw, _ := json.Marshal(readArgs{Path: path})
		result, err := read.Run(ctx, raw)
		if err != nil {
			continue
		}
		if len(skipped) > 0 || text.Len()+len(result.Content) > konst.SubAgentReferenceBytes {
			skipped = append(skipped, path)
			continue
		}
		text.WriteString("\n\nthe brief names " + path + ", so it is read for you, as a read call shows it:\n" + result.Content)
	}
	if len(skipped) > 0 {
		fmt.Fprintf(&text, "\n\nthe brief also names these files, not read for you to stay within %d bytes, so read them before you change them: %s", konst.SubAgentReferenceBytes, strings.Join(skipped, ", "))
	}
	return text.String()
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
		return subagent.Finished
	}
}

func resumable(messages []llm.Message) []llm.Message {
	stripped := slices.Clone(messages)
	for i := range stripped {
		stripped[i].Thinking = llm.Thinking{}
	}
	return Sendable(stripped)
}

func (t *SpawnTool) runRounds(outerCtx, subAgentCtx context.Context, held *heldSubAgent, subAgent Config, trace spawnTrace) ([]Row, subagent.State, error) {
	agent, subAgentID := held.agent, held.agent.ID
	first, firstErr := trace.run(outerCtx, subAgentCtx, subAgent)
	claims := []Row{first}
	state := roundState(outerCtx, firstErr)
	held.remember(first)
	for state == subagent.Finished && t.Review != nil {
		t.roster.Reached(subAgentID, subagent.InReview, reportOf(agent, held.forkedSoFar(), claims, subagent.InReview).Text())
		last := &claims[len(claims)-1]
		reviewed := *last
		reviewed.Task = agent.Brief
		decision, err := t.decided(subAgentCtx, reviewed)
		if err != nil {
			last.Warnings = append(last.Warnings, "the done review did not run, so the sub-agent's own claim stands: "+err.Error())
			break
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
		next, reopenErr := t.roster.Reopen(subAgentID, decision.Reason)
		if reopenErr != nil {
			last.Warnings = append(last.Warnings, reopenErr.Error())
			break
		}
		t.roster.Reached(subAgentID, subagent.Working, "")
		subAgent.History = held.history
		subAgent.Task = "You reported this finished and the done review did not believe you: " + decision.Reason
		subAgent.NewID = func() string { return subAgentID + "-r" + strconv.Itoa(next) }
		reRow, reErr := trace.run(outerCtx, subAgentCtx, subAgent)
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

type askTool struct {
	orchestrator *SpawnTool
	asking       subagent.SubAgent
	conversation []llm.Message
}

type askArgs struct {
	Question string `json:"question"`
	Why      string `json:"why"`
	Default  string `json:"default"`
}

func (askTool) Name() string { return "ask" }

func (askTool) Definition() llm.Tool {
	text := map[string]any{"type": "string"}
	return llm.Tool{
		Name: "ask",
		Description: "asks the orchestrator one question and waits for its answer, which comes back as this result. " +
			"ask before you start a server or a watcher, bind a port, kill a process you did not start, or change a file outside the paths you hold. " +
			"default is what you would do with no answer: when the orchestrator cannot answer in time it stands, and the result says it was assumed",
		Parameters: map[string]any{
			"type":       "object",
			"properties": map[string]any{"question": text, "why": text, "default": text},
			"required":   []string{"question", "why", "default"},
		},
	}
}

func (a askTool) Run(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args askArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, fmt.Errorf("ask: arguments are not the expected shape: %w", err)
	}
	if strings.TrimSpace(args.Question) == "" || strings.TrimSpace(args.Default) == "" {
		return Result{}, errors.New("ask: question and default are both required")
	}
	roster := a.orchestrator.roster
	roster.Reached(a.asking.ID, subagent.WaitingAnswer, "asks: "+args.Question)
	answer, err := a.orchestrator.answer(ctx, a.asking, a.conversation, args)
	roster.Reached(a.asking.ID, subagent.Working, "")
	if err != nil {
		return Result{Content: "the orchestrator did not answer, so your default stands, assumed and not confirmed: " + args.Default +
			"\n\nwhy there was no answer: " + err.Error(), Command: "ask assumed: " + args.Question}, nil
	}
	return Result{Content: "the orchestrator answers: " + answer, Command: "ask answered: " + args.Question}, nil
}

func (t *SpawnTool) answer(ctx context.Context, asking subagent.SubAgent, conversation []llm.Message, args askArgs) (string, error) {
	model := t.base.Model
	if t.base.Accounts.Pick != nil {
		account, err := t.base.Accounts.Pick(ctx)
		if err != nil {
			return "", err
		}
		model = account.Model
	}
	if model == nil {
		return "", errors.New("the orchestrator has no model to answer with")
	}
	rules, err := rule.LoadFS(shipped.Files(), "library")
	if err != nil {
		return "", err
	}
	system := t.base.System
	for _, loaded := range rules {
		if loaded.ID == "answer_asks" && !strings.Contains(system, loaded.Text) {
			system = strings.TrimSpace(loaded.Text + "\n\n" + system)
		}
	}
	var running []string
	if registry := ShellRegistryFrom(ctx); registry != nil {
		shells, err := registry.List()
		if err != nil {
			return "", err
		}
		for _, held := range shells {
			if held.State != shell.Running {
				continue
			}
			line := held.Name + " runs " + held.Command
			if port := shell.NamedPort(held.Dir, held.Command, nil); port > 0 {
				line += " on port " + strconv.Itoa(port)
			}
			running = append(running, line)
		}
	}
	for _, held := range t.roster.SubAgents() {
		running = append(running, fmt.Sprintf("sub-agent %s is %s on %q, holding %s", held.ID, held.State, held.Mission, strings.Join(held.Owns, ", ")))
	}
	question := fmt.Sprintf("sub-agent %s, whose brief is %q, asks you: %s\nwhy: %s\nwhat it does with no answer: %s\n\nwhat runs now: %s\n\nanswer it in one line.",
		asking.ID, asking.Brief, args.Question, args.Why, args.Default, strings.Join(running, "; "))
	spoken := slices.DeleteFunc(slices.Clone(conversation), func(message llm.Message) bool { return message.Role == llm.RoleSystem })
	messages := append([]llm.Message{{Role: llm.RoleSystem, Content: system}}, resumable(spoken)...)
	messages = append(messages, llm.Message{Role: llm.RoleUser, Content: question})
	within, cancel := context.WithTimeout(ctx, konst.SubAgentAskMillis*time.Millisecond)
	defer cancel()
	decision, err := model.Ask(within, llm.Request{Messages: messages})
	if err != nil {
		return "", err
	}
	if decision.Outcome != llm.OutcomeMessage || strings.TrimSpace(decision.Content) == "" {
		return "", errors.New("the orchestrator's model returned no answer text")
	}
	return strings.TrimSpace(decision.Content), nil
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
