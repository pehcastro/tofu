package turn

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"tofu/internal/hook"
	"tofu/internal/judge/method"
	"tofu/internal/konst"
	"tofu/internal/llm"
	"tofu/internal/llm/wire/anthropic"
	"tofu/internal/recall"
	"tofu/internal/settings"
	"tofu/internal/subagent"
	"tofu/internal/sys"
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
	Running int
	Limit   int
}

func (e BreadthLimitError) Error() string {
	return fmt.Sprintf("refused: %d sub-agents are running and the %s setting is %d, which the person can raise in the settings menu; wait for a report, or send the work to a running sub-agent with message",
		e.Running, settings.SubAgentsPerTurn, e.Limit)
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
	Effort  llm.Effort
}

type SubAgents struct {
	Defined []subagent.Definition
	Open    func(subagent.Definition, llm.Effort) (SubAgentModel, error)
	Prompt  ComposeSpec
	Root    string
	Brief   func(definition subagent.Definition, task string) string
	Ended   func(definition subagent.Definition, task string, rounds []Row, report SubAgentReport, finished bool)
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
	Running   int
	Depth     int
	WallClock time.Duration
	CheckIn   time.Duration
	CacheTTL  string
}

type SpawnTool struct {
	Review         DoneReview
	Methods        method.Table
	SubAgents      SubAgents
	Limits         func() SubAgentLimits
	SettingsTool   bool
	ChecksWork     bool
	Project        string
	Inbox          *Inbox
	Remembered     func() (map[string]string, error)
	orchestratorID string
	depth          int
	writesNothing  bool
	mu             sync.Mutex
	tree           *spawnTree
	base           Config
	roster         *subagent.Roster
	warmups        map[string]*warmup
	naming         *namingOrder
}

type spawnTree struct {
	mu     sync.Mutex
	rows   []Row
	ran    []Spawned
	spend  float64
	billed int
	paid   float64
}

func (t *spawnTree) bill() ([]string, float64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	var ended []string
	for _, row := range t.rows[t.billed:] {
		if !slices.Contains(ended, row.ID) {
			ended = append(ended, row.ID)
		}
	}
	spent := t.spend - t.paid
	t.billed, t.paid = len(t.rows), t.spend
	return ended, spent
}

func (t *spawnTree) retain(rows []Row) {
	t.rows = append(t.rows, rows...)
	for i := range len(t.rows) - konst.SubAgentRetainedRows {
		released := t.rows[i].Summary()
		released.Conversation = nil
		t.rows[i] = released
	}
}

func NewSpawnTool(orchestratorID string, base Config, roster *subagent.Roster) *SpawnTool {
	return &SpawnTool{Inbox: NewInbox(), orchestratorID: orchestratorID, base: base, roster: roster, tree: &spawnTree{}}
}

func (t *SpawnTool) Name() string { return "spawn" }

func (t *SpawnTool) SubAgentRows() []Row {
	t.tree.mu.Lock()
	defer t.tree.mu.Unlock()
	return slices.Clone(t.tree.rows)
}

func (t *SpawnTool) Spawned() []Spawned {
	t.tree.mu.Lock()
	defer t.tree.mu.Unlock()
	return slices.Clone(t.tree.ran)
}

func startedSpawnsOnly(tools Registry, calls []ToolCallRow) []string {
	var started []string
	for _, call := range calls {
		if _, spawning := tools.byName[call.Tool].(*SpawnTool); !spawning || call.SubAgentID == "" || call.Error != "" {
			return nil
		}
		started = append(started, call.Command)
	}
	return started
}

func (t *SpawnTool) limits() SubAgentLimits {
	if t.Limits == nil {
		return SubAgentLimits{Running: konst.SubAgentsPerTurnDefault, Depth: konst.SubAgentDepthDefault, CheckIn: konst.SubAgentCheckSecondsDefault * time.Second}
	}
	return t.Limits()
}

func (t *SpawnTool) Definition() llm.Tool {
	limits := t.limits()
	raise, duties := "the person can raise either in the settings menu.", "you plan and spawn"
	if t.ChecksWork {
		duties = "you plan, spawn and verify"
	}
	cite := ""
	if t.Remembered != nil {
		cite = "a sub-agent sees none of your memory: cite each entry it needs as [memory#id] in the task, and that line is copied above the brief word for word. "
	}
	if t.SettingsTool {
		raise = "the person can raise either in the settings menu, and you can ask to with the settings tool, which the person answers."
	}
	properties := map[string]any{
		"task":    map[string]any{"type": "string"},
		"mission": map[string]any{"type": "string", "description": "the work in a handful of words, as a board entry reads: work on BOJI-395. the task is the brief and is kept whole"},
		"owns":    map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
	}
	if t.base.Board != nil {
		properties["ticket"] = map[string]any{"type": "string", "description": "the board ticket this sub-agent works, such as DEMO-2: its owns become the sub-agent's paths and owns given here are ignored"}
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
		Description: duties + ", and implementation goes to a sub-agent: spawn one per separable piece of work as soon as the piece is known, rather than writing the code yourself first. " +
			"hands one piece of work to a sub-agent with its own context and its own conversation, and returns at once with its name while it works in the background. " +
			"its report, rather than its transcript, comes to you later as a message naming it. a reply whose calls are all spawns ends your turn once they start, so spawn every piece you know in that one reply. " +
			"owns lists the paths the sub-agent may write, every other path is refused at the write, and no two sub-agents may hold overlapping paths; leave owns out for a sub-agent that only reads, runs commands or researches, and every write it tries is refused. " + cite +
			fmt.Sprintf("At most %d sub-agents running at once, nested at most %d deep. These are the person's settings %s and %s: %s",
				limits.Running, limits.Depth, settings.SubAgentsPerTurn, settings.SubAgentDepth, raise),
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
	Ticket  string   `json:"ticket,omitempty"`
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
	warm, naming := t.warmups[site.call], t.naming
	t.mu.Unlock()
	defer naming.passed(site.call)
	if strings.TrimSpace(args.Task) == "" {
		return Result{}, errors.New("spawn: task is required")
	}
	cited, err := t.citedMemory(args.Task)
	if err != nil {
		return Result{}, err
	}
	if t.writesNothing && (len(args.Owns) > 0 || args.Ticket != "") {
		return Result{}, ReadOnlyError{Tool: t.Name()}
	}
	if args.Ticket != "" && t.base.Board != nil {
		if args.Owns, err = t.base.TicketGrant(*t.base.Board, args.Ticket, t.base.Session, t.ticketActor(args.Ticket)); err != nil {
			return Result{}, fmt.Errorf("spawn: %w", err)
		}
	}
	limits := t.limits()
	if t.depth+1 > limits.Depth {
		return Result{}, DepthLimitError{Depth: t.depth + 1, Limit: limits.Depth}
	}
	if err := t.Inbox.reserve(limits.Running); err != nil {
		return Result{}, fmt.Errorf("spawn %w", err)
	}
	opened, started := SubAgentModel{}, false
	defer func() {
		if started {
			return
		}
		t.Inbox.unreserve()
		warm.leaderDone(site.call)
		if opened.Close != nil {
			opened.Close()
		}
	}()
	definition, err := t.SubAgents.Named(args.Agent)
	if err != nil {
		return Result{}, err
	}
	narrowed := ""
	if fire, firing := ctx.Value(hookFireKey{}).(func(hook.Input) hook.Verdict); firing {
		spawning := fire(hook.Input{Event: hook.SubagentSpawn, CallID: site.call, Spawn: &hook.SpawnFacts{Definition: definition.Name, Mission: args.mission(), Task: args.Task, Owns: append([]string{}, args.Owns...), Ticket: args.Ticket}})
		if spawning.Block != "" {
			return Result{}, errors.New("spawn refused by a SubagentSpawn hook: " + spawning.Block)
		}
		if spawning.Narrowed {
			narrowed = "a SubagentSpawn hook narrowed owns from [" + strings.Join(args.Owns, ", ") + "] to [" + strings.Join(spawning.Owns, ", ") + "]. "
			args.Owns = spawning.Owns
		}
	}
	if err := sideHeldOwns(t.Name(), t.base.Sessions, t.base.Session, args.Owns); err != nil {
		return Result{}, err
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
	if opened, err = t.open(definition, effort); err != nil {
		return Result{}, fmt.Errorf("spawn: %w", err)
	}
	var recorded []string
	if site.log != nil {
		for _, run := range site.log.Header().Agents {
			recorded = append(recorded, run.Agent)
		}
	}
	naming.waitForEarlier(site.call)
	t.tree.mu.Lock()
	agent := subagent.SubAgent{
		ID:      t.roster.NextID(definition.Name, recorded),
		Agent:   definition.Name,
		Model:   opened.Slug,
		Mission: args.mission(),
		Brief:   args.Task,
		Owns:    args.Owns,
		Ticket:  args.Ticket,
		Started: t.clock(),
	}
	subAgentID, holding := agent.ID, t.roster.Hold(agent)
	t.tree.mu.Unlock()
	naming.passed(site.call)
	if err := holding; err != nil {
		var collision subagent.CollisionError
		if errors.As(err, &collision) {
			return Result{}, fmt.Errorf("spawn refused: %s already holds %q, which overlaps %q. send this work to %s with message, or free its paths with message and do release, or do kill if it runs",
				collision.Holder, collision.HolderGlob, collision.Glob, collision.Holder)
		}
		return Result{}, fmt.Errorf("spawn: %w", err)
	}

	scratch, err := t.scratch(ctx, subAgentID)
	if err != nil {
		t.roster.Release(subAgentID)
		return Result{}, fmt.Errorf("spawn: %s's scratch folder was not made: %w", subAgentID, err)
	}
	held := &heldSubAgent{agent: agent, definition: definition, effort: opened.Effort, askedEffort: effort, system: system,
		environment: environment + scratchWords(scratch) + t.briefFiles(ctx, args, site.conversation),
		boundary:    subagent.NewBoundary(subAgentID, scratch.Dir(), args.Owns), scratch: scratch, inbox: NewInbox(),
		trace: spawnTrace{definition: agent.Agent, model: agent.Model, mission: agent.Mission, owns: args.Owns, ticket: args.Ticket, depth: t.depth + 1}}
	runCtx, cancel := context.WithCancel(ctx)
	t.Inbox.keep(held, cancel)
	t.tree.mu.Lock()
	t.tree.ran = append(t.tree.ran, Spawned{ID: subAgentID, Call: site.call, Agent: definition.Name, Slug: opened.Slug, Windows: opened.Windows, Effort: opened.Effort})
	t.tree.mu.Unlock()
	task := cited + args.Task
	if t.SubAgents.Brief != nil {
		task += t.SubAgents.Brief(definition, args.Task)
	}
	started = true
	t.Inbox.hold(held, site.log)
	go t.background(runCtx, cancel, held, opened, site, task, warm)
	return Result{Content: narrowed + held.runningWords(), Command: subAgentID + " running: " + agent.Mission, SubAgent: subAgentID}, nil
}

func (t *SpawnTool) proseStore() *recall.Store {
	dir, err := t.artifactDir()
	if t.base.TruncateResults || err != nil {
		return nil
	}
	return recall.NewStore(dir)
}

func (t *SpawnTool) clock() time.Time {
	if t.base.Now == nil {
		return time.Now()
	}
	return t.base.Now()
}

func (t *SpawnTool) artifactDir() (string, error) {
	if t.base.ArtifactDir != "" {
		return t.base.ArtifactDir, nil
	}
	state, err := sys.ProjectStateDir()
	return filepath.Join(state, "artifacts"), err
}

const resumedWords = "resumed by a message"

func (t *SpawnTool) background(ctx context.Context, cancel context.CancelFunc, held *heldSubAgent, opened SubAgentModel, site spawnSite, task string, warm *warmup) {
	defer cancel()
	defer warm.leaderDone(site.call)
	if opened.Close != nil {
		defer opened.Close()
	}
	ctx = context.WithValue(context.WithValue(ctx, subAgentKey{}, held.agent.ID), boardActorKey{}, t.ticketActor(cmp.Or(held.agent.Ticket, held.agent.ID)))
	if held.scratch.Root != "" {
		ctx = sys.WithScratch(ctx, held.scratch)
	}
	if ttl := t.limits().CacheTTL; ttl != "" {
		ctx = anthropic.WithCacheTTL(ctx, ttl)
	}
	check := &checkIn{id: held.agent.ID, started: t.clock(), missed: func(run Row) []string {
		return gateMissed(t.Project, projectRecipes(t.Project), held.definition, held.boundary.Owns(), []Row{run})
	}}
	defer t.watch(held, check)()
	var taskOrigin llm.Origin
	for {
		subAgent := opened.onto(t.subAgentConfig(held, site, check))
		subAgent.Task, subAgent.TaskOrigin, subAgent.NewID = task, taskOrigin, func() string { return held.agent.ID }
		report := t.converse(ctx, held, warm.stagger(ctx, site.call, subAgent), site)
		next, _ := held.inbox.next(ctx, nil)
		stopping := ctx.Err() != nil
		if stopping {
			_ = held.inbox.settle()
		}
		if stopping || len(next) == 0 {
			next = t.Inbox.ended(held, report, stopping, site.log)
		}
		if len(next) == 0 {
			return
		}
		task, taskOrigin, warm = strings.Join(textsOf(next), "\n\n"), originOf(next), nil
		t.roster.Reached(held.agent.ID, subagent.Working, resumedWords)
	}
}

func (t *SpawnTool) subAgentConfig(held *heldSubAgent, site spawnSite, check *checkIn) Config {
	offered := func(name string) bool {
		if name == ShellToolName {
			name = bashToolName
		}
		return len(held.definition.Tools) == 0 || slices.Contains(held.definition.Tools, name)
	}
	var owned []Tool
	for _, tool := range t.base.Tools.tools {
		switch {
		case !offered(tool.Name()) && !strings.HasPrefix(tool.Name(), "scratch_"):
			continue
		case tool.Name() == "write" || tool.Name() == "edit":
			tool = ownedTool{tool: tool, boundary: held.boundary}
		case tool.Name() == "bash":
			tool = ownedShell{tool: tool, boundary: held.boundary}
		}
		owned = append(owned, watchedTool{tool: tool, held: held})
	}
	if held.agent.Ticket != "" && t.base.Board != nil {
		if ticketTools, err := t.base.TicketTools(*t.base.Board, t.ticketActor(held.agent.Ticket)); err == nil {
			owned = append(owned, ticketTools...)
		}
	}
	if offered(t.Name()) {
		owned = append(owned, &SpawnTool{Review: t.Review, Methods: t.Methods, SubAgents: t.SubAgents, Limits: t.Limits, ChecksWork: t.ChecksWork, Project: t.Project, Inbox: held.inbox, Remembered: t.Remembered,
			orchestratorID: held.agent.ID, depth: t.depth + 1, writesNothing: len(held.boundary.Owns()) == 0, base: t.base, roster: t.roster, tree: t.tree})
	}
	subAgent := t.base
	subAgent.Memory = ""
	subAgent.Tools = claimedTools(NewRegistry(append(owned, watchedTool{tool: askTool{orchestrator: t, asking: held.agent, conversation: site.conversation}, held: held})...), t.base.Sessions, t.base.Session)
	subAgent.Caps.MaxSteps, subAgent.Caps.MaxForks = cmp.Or(subAgent.Caps.MaxSteps, konst.SubAgentMaxSteps), konst.SubAgentMaxForks
	subAgent.Caps.WallClock = cmp.Or(t.limits().WallClock, konst.SubAgentWallClockSeconds*time.Second)
	held.kept.Lock()
	subAgent.System, subAgent.Environment, subAgent.History, subAgent.Prefix = held.system, held.environment, held.history, &held.prefix
	held.kept.Unlock()
	subAgent.SpawnedFrom, subAgent.Boundary, subAgent.Inbox, subAgent.Steering = t.orchestratorID, held.boundary, held.inbox, nil
	subAgent.Person = t.orchestratorAnswers(held, site)
	subAgent.AgentType = held.definition.Name
	subAgent.Session, subAgent.Log, subAgent.Turn, subAgent.SpawnedBy = "", site.log, site.turn, site.call
	if site.log == nil {
		subAgent.Sessions = nil
	}
	subAgent.Step = func(step StepRow) {
		called := make([]string, len(step.ToolCalls))
		for i, call := range step.ToolCalls {
			called[i] = call.Tool
		}
		now := t.clock()
		sofar, _ := t.roster.SubAgent(held.agent.ID)
		t.roster.Stepped(held.agent.ID, sofar.Steps+1, now, called...)
		check.stepped(step, now)
		if t.base.Step != nil {
			t.tree.mu.Lock()
			t.base.Step(step)
			t.tree.mu.Unlock()
		}
	}
	return subAgent
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

func (t *SpawnTool) converse(ctx context.Context, held *heldSubAgent, subAgent Config, site spawnSite) string {
	agent, trace := held.agent, held.trace
	trace.site = site
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
	id := subAgent.NewID()
	began := trace.begin(id)
	claims, sentBack, state, runErr := t.runRounds(ctx, subAgentCtx, held, subAgent)
	forked := held.forkedSoFar()
	asked := subAgent.Boundary.Asked()
	last := &claims[len(claims)-1]
	if err := errors.Join(began, trace.end(id, state)); err != nil {
		last.Warnings = append(last.Warnings, "this sub-agent run was not wholly recorded: "+err.Error())
	}
	report := reportOf(agent, forked, []Row{wholeRun(claims)}, state)
	report.Asked = asked
	if len(asked) > 0 && state == subagent.Finished {
		report.Completion = subagent.NeedsContext
	}
	if len(sentBack) > 0 {
		times := "once"
		if len(sentBack) > 1 {
			times = strconv.Itoa(len(sentBack)) + " times"
		}
		report.Findings = append(report.Findings, subagent.Finding{Bucket: subagent.Noted, Reason: "sent back " + times + ": " + strings.Join(sentBack, "; then ")})
	}
	t.tree.mu.Lock()
	t.tree.retain(claims)
	for _, claim := range append(slices.Clone(forked), claims...) {
		t.tree.spend += claim.TotalCostUSD
	}
	t.tree.mu.Unlock()
	if t.SubAgents.Ended != nil {
		t.SubAgents.Ended(held.definition, agent.Brief, append(forked, claims...), report, runErr == nil && state != subagent.Errored && !stoppedEarly(state, last.Outcome))
	}
	contract := subagent.BuildContract(agent.Brief, report.Prose, stoppedEarly(state, last.Outcome))
	if agent.Ticket != "" && t.base.Board != nil {
		if ticket, err := t.base.Board.Get(agent.Ticket); err == nil {
			contract = subagent.TicketContract(ticket.ID, ticket.Acceptance, report.Prose, stoppedEarly(state, last.Outcome))
		}
	}
	contract.Wrote = report.Wrote
	text := report.Text(t.proseStore()) + "\n\n" + contract.Block()
	if runs := held.runsAs(); runs != "" {
		text = agent.ID + " ran" + runs + "\n\n" + text
	}
	t.roster.Reached(agent.ID, state, text)
	held.agent.State = state
	if runErr != nil && state != subagent.Parked {
		return fmt.Sprintf("sub-agent %s is %s: %v\n\n%s", agent.ID, state, runErr, text)
	}
	return text
}

func (t *SpawnTool) ticketActor(ticket string) string {
	return t.base.Session + "/" + ticket
}
