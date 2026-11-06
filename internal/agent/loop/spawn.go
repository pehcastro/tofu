package loop

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"tofu/internal/agent/context/recall"
	"tofu/internal/agent/hooks"
	"tofu/internal/agent/subagents"
	"tofu/internal/judge/points"
	"tofu/internal/konst"
	"tofu/internal/librarian/rules"
	"tofu/internal/models/llm/catalog"
	"tofu/internal/models/llm/core"
	"tofu/internal/models/llm/wire/anthropic"
	"tofu/internal/platform/project"
	"tofu/internal/platform/safe"
	platformscratch "tofu/internal/platform/scratch"
	"tofu/internal/project/boardy"
	"tofu/internal/store/memory/entries"
	"tofu/internal/store/session"
	"tofu/internal/store/settings"
	"tofu/internal/tools/contract"
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
	Cache    catalog.PromptCache
	Effort   core.Effort
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
	Effort  core.Effort
}

type SubAgents struct {
	Defined []subagents.Definition
	Open    func(subagents.Definition, core.Effort) (SubAgentModel, error)
	Takes   func(subagents.Definition) ([]core.Effort, error)
	Prompt  ComposeSpec
	Root    string
	Brief   func(definition subagents.Definition, task string) string
	Ended   func(definition subagents.Definition, task string, rounds []Row, report SubAgentReport, finished bool)
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

func enabledSubAgents(defined []subagents.Definition) []subagents.Definition {
	var enabled []subagents.Definition
	for _, definition := range defined {
		if definition.Runs == subagents.RunsModel || definition.Runs == subagents.RunsInherit {
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

func (s SubAgents) Named(name string) (subagents.Definition, error) {
	if name == "" {
		return subagents.Definition{}, nil
	}
	why := "is no sub-agent tofu found"
	for _, definition := range s.Defined {
		if definition.Name != name {
			continue
		}
		switch definition.Runs {
		case subagents.RunsModel, subagents.RunsInherit:
			return definition, nil
		case subagents.RunsDisabled:
			why = "is disabled by " + cmp.Or(definition.AssignedIn, definition.Path)
		case subagents.RunsRefused:
			why = "cannot run: " + strings.Join(definition.Refused, "; ")
		default:
			panic("turn: unknown sub-agent state " + string(definition.Runs))
		}
	}
	return subagents.Definition{}, AgentRefusedError{Name: name, Why: why, Enabled: s.enabledNames()}
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
	Methods        points.Table
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
	roster         *subagents.Roster
	warmups        map[string]*warmup
	naming         *namingOrder
	plans          map[string]agentPlan
	built          *builtOncePerTurn
	writes         *subagents.Writes
	stepped        CalledAsASubAgentStepIsRecorded
}

type builtOncePerTurn struct {
	offer       sync.Once
	effort      map[string]any
	agent       map[string]any
	tools       sync.Once
	definitions []core.Tool
}

func (b *builtOncePerTurn) offered(s SubAgents) (effort, agent map[string]any) {
	b.offer.Do(func() {
		b.effort = s.effortProperty()
		if names := s.enabledNames(); len(names) > 0 {
			b.agent = map[string]any{"type": "string", "enum": names,
				"description": "the sub-agent that does the work, on its own model with its own instructions. left out, the sub-agent runs on the orchestrator's model"}
		}
	})
	return b.effort, b.agent
}

func (b *builtOncePerTurn) defined(tools Registry) []core.Tool {
	b.tools.Do(func() { b.definitions = tools.Definitions() })
	return b.definitions
}

type agentPlan struct {
	tool  contract.Tool
	shown func() string
}

func (t *SpawnTool) ownPlan(id string, lead contract.Tool) (agentPlan, bool) {
	if memoised, cached := lead.(interface{ Uncached() contract.Tool }); cached {
		lead = memoised.Uncached()
	}
	separable, isPlan := lead.(interface {
		ForAnotherAgent() (contract.Tool, func() string)
	})
	if !isPlan {
		return agentPlan{}, false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if kept, made := t.plans[id]; made {
		return kept, true
	}
	if t.plans == nil {
		t.plans = map[string]agentPlan{}
	}
	tool, shown := separable.ForAnotherAgent()
	t.plans[id] = agentPlan{tool: tool, shown: shown}
	return t.plans[id], true
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

func NewSpawnTool(orchestratorID string, base Config, roster *subagents.Roster) *SpawnTool {
	return &SpawnTool{Inbox: NewInbox(), orchestratorID: orchestratorID, base: base, roster: roster, tree: &spawnTree{}, built: &builtOncePerTurn{}, writes: &subagents.Writes{}}
}

type CalledAsASubAgentStepIsRecorded func(agent string, step StepRow)

func (t *SpawnTool) Announce(sent CalledAsEachRequestIsSent, stepped CalledAsASubAgentStepIsRecorded) {
	t.base.Sent, t.stepped = sent, stepped
}

const SpawnToolName = "spawn"

func (t *SpawnTool) Name() string { return SpawnToolName }

func (t *SpawnTool) SubAgentRows() []Row {
	t.tree.mu.Lock()
	defer t.tree.mu.Unlock()
	return slices.Clone(t.tree.rows)
}

func (t *SpawnTool) Spawned() []Spawned {
	if t.tree == nil {
		return nil
	}
	t.tree.mu.Lock()
	defer t.tree.mu.Unlock()
	return slices.Clone(t.tree.ran)
}

func onlyStartedSpawns(tools Registry, calls []ToolCallRow) bool {
	return len(calls) > 0 && !slices.ContainsFunc(calls, func(call ToolCallRow) bool {
		_, spawning := tools.byName[call.Tool].(*SpawnTool)
		return !spawning || call.SubAgentID == "" || call.Error != ""
	})
}

func (t *SpawnTool) limits() SubAgentLimits {
	if t.Limits == nil {
		return SubAgentLimits{Running: konst.SubAgentsPerTurnDefault, Depth: konst.SubAgentDepthDefault, CheckIn: konst.SubAgentCheckSecondsDefault * time.Second}
	}
	return t.Limits()
}

func (t *SpawnTool) Definition() core.Tool {
	limits := t.limits()
	raise := "the person can raise either in the settings menu."
	cite := ""
	if t.Remembered != nil {
		cite = "a sub-agent opens on every entry of your memory; cite the entries this task turns on as [memory#id] in the task, and each line is copied above the brief word for word. "
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
	effort, agent := t.built.offered(t.SubAgents)
	if effort != nil {
		properties["effort"] = effort
	}
	if agent != nil {
		properties["agent"] = agent
	}
	return core.Tool{
		Name: SpawnToolName,
		Description: "hands one piece of work to a sub-agent with its own context and its own conversation, and returns at once with its name while it works in the background. task is the whole brief the sub-agent starts from. " +
			"its report, rather than its transcript, comes to you later as a message naming it. a reply whose calls are all spawns ends your turn once they start. " +
			"owns lists the paths the sub-agent may write, every other path is refused at the write, and no two sub-agents may hold overlapping paths; leave owns out for a sub-agent that only reads, runs commands or researches, and every write it tries is refused. " + t.ticketNeed() + cite +
			fmt.Sprintf("At most %d sub-agents running at once, nested at most %d deep. These are the person's settings %s and %s: %s",
				limits.Running, limits.Depth, settings.SubAgentsPerTurn, settings.SubAgentDepth, raise),
		Parameters: map[string]any{
			"type":       "object",
			"properties": properties,
			"required":   []string{"task"},
		},
	}
}

func (t *SpawnTool) ticketNeed() string {
	var readers, editors []string
	for _, definition := range enabledSubAgents(t.SubAgents.Defined) {
		if len(definition.Tools) > 0 && !slices.Contains(definition.Tools, "edit") {
			readers = append(readers, definition.Name)
		} else {
			editors = append(editors, definition.Name)
		}
	}
	said := ""
	if len(editors) > 0 && t.base.Board != nil {
		said = "agents that edit need a ticket naming them: " + strings.Join(editors, ", ") + ". "
	}
	if len(readers) > 0 {
		said += "agents with no edit tool only read and start with no ticket and no owns: " + strings.Join(readers, ", ") + ". "
	}
	return said
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
	for i := 0; i+1 < len(first); i++ {
		if strings.ContainsRune(".!?", rune(first[i])) && first[i+1] == ' ' {
			first = first[:i]
			break
		}
	}
	first = strings.TrimRight(first, ".!?")
	runes := []rune(first)
	if len(runes) <= konst.SubAgentMissionChars {
		return first
	}
	cut := string(runes[:konst.SubAgentMissionChars])
	if space := strings.LastIndexByte(cut, ' '); space > 0 {
		cut = cut[:space]
	}
	return strings.TrimRight(cut, " ,;:") + "..."
}

func (t *SpawnTool) Run(ctx context.Context, raw json.RawMessage) (contract.Result, error) {
	timing := startSpawnTiming()
	var args spawnArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return contract.Result{}, fmt.Errorf("spawn: arguments are not the expected shape: %w", err)
	}
	site, _ := ctx.Value(spawnSiteKey{}).(spawnSite)
	t.mu.Lock()
	warm, naming := t.warmups[site.call], t.naming
	t.mu.Unlock()
	defer naming.passed(site.call)
	if strings.TrimSpace(args.Task) == "" {
		return contract.Result{}, errors.New("spawn: task is required")
	}
	cited, err := t.citedMemory(args.Task)
	if err != nil {
		return contract.Result{}, err
	}
	timing.took("memory")
	if t.writesNothing && (len(args.Owns) > 0 || args.Ticket != "") {
		return contract.Result{}, ReadOnlyError{Tool: t.Name()}
	}
	var effort core.Effort
	if args.Effort != "" {
		if effort, err = core.ParseEffort(args.Effort); err != nil {
			return contract.Result{}, fmt.Errorf("spawn: %w", err)
		}
	}
	var boardSizes map[string]int64
	revision := 0
	if args.Ticket != "" && t.base.Board != nil {
		boardSizes = t.base.Board.Store.EventSizes()
		if err := t.base.Board.Fire(rules.WhenBefore, rules.HookAgentSpawn, "", args.Ticket, t.base.Session); err != nil {
			return contract.Result{}, fmt.Errorf("spawn: %w%s", err, BoardNudges(t.base.Board.Store, boardSizes))
		}
		ticket, err := t.base.Board.Get(args.Ticket)
		if err != nil {
			return contract.Result{}, fmt.Errorf("spawn: %w", err)
		}
		thread, err := t.base.Board.Store.Thread(args.Ticket)
		if err != nil {
			return contract.Result{}, fmt.Errorf("spawn: %w", err)
		}
		revision = len(ticket.Acceptance)
		if err := raisedTwice(args.Ticket, thread, revision, effort); err != nil {
			return contract.Result{}, err
		}
		args.Owns = ticket.Owns
	}
	timing.took("ticket grant")
	limits := t.limits()
	if t.depth+1 > limits.Depth {
		return contract.Result{}, DepthLimitError{Depth: t.depth + 1, Limit: limits.Depth}
	}
	if err := t.Inbox.reserve(limits.Running); err != nil {
		return contract.Result{}, fmt.Errorf("spawn %w", err)
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
		return contract.Result{}, err
	}
	narrowed := ""
	if fire, firing := ctx.Value(hookFireKey{}).(func(context.Context, hooks.Input) hooks.Verdict); firing {
		spawning := fire(ctx, hooks.Input{Event: hooks.SubagentSpawn, CallID: site.call, Spawn: &hooks.SpawnFacts{Definition: definition.Name, Mission: args.mission(), Task: args.Task, Owns: append([]string{}, args.Owns...), Ticket: args.Ticket}})
		if spawning.Block != "" {
			return contract.Result{}, errors.New("spawn refused by a SubagentSpawn hook: " + spawning.Block)
		}
		if spawning.Narrowed {
			narrowed = "a SubagentSpawn hook narrowed owns from [" + strings.Join(args.Owns, ", ") + "] to [" + strings.Join(spawning.Owns, ", ") + "]. "
			args.Owns = spawning.Owns
		}
	}
	timing.took("hook")
	if err := sideHeldOwns(t.Name(), t.base.Sessions, t.base.Session, args.Owns); err != nil {
		return contract.Result{}, err
	}
	system, parts, environment, err := t.SubAgents.prompt(t.base, definition, args.Task, args.Owns)
	if err != nil {
		return contract.Result{}, fmt.Errorf("spawn: the sub-agent's prompt did not compose: %w", err)
	}
	timing.took("prompt")
	if opened, err = t.open(definition, effort); err != nil {
		return contract.Result{}, fmt.Errorf("spawn: %w", err)
	}
	timing.took("account pick")
	var recorded []string
	if site.log != nil {
		for _, run := range site.log.Header().Agents {
			recorded = append(recorded, run.Agent)
		}
	}
	if err := naming.waitForEarlier(ctx, site.call); err != nil {
		return contract.Result{}, fmt.Errorf("spawn stopped before it started: %w", err)
	}
	timing.took("earlier siblings")
	t.tree.mu.Lock()
	agent := subagents.SubAgent{
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
		var collision subagents.CollisionError
		if errors.As(err, &collision) {
			return contract.Result{}, fmt.Errorf("spawn refused: %s already holds %q, which overlaps %q. send this work to %s with message, or free its paths with message and do release, or do kill if it runs",
				collision.Holder, collision.HolderGlob, collision.Glob, collision.Holder)
		}
		return contract.Result{}, fmt.Errorf("spawn: %w", err)
	}
	if args.Ticket != "" && t.base.Board != nil {
		lead := t.speaker()
		if _, err := t.base.TicketGrant(t.boardAs(t.base.Session, lead), args.Ticket, t.base.Session, subAgentID); err != nil {
			t.roster.Release(subAgentID)
			return contract.Result{}, fmt.Errorf("spawn: %w", err)
		}
		spawned := boardy.Event{Ticket: args.Ticket, Kind: boardy.Spawned, Actor: t.base.Session, Author: lead, Session: t.base.Session, Agent: subAgentID, Call: site.call,
			Text: subAgentID + effortWords(effort) + ", " + agent.Mission, Brief: agent.Brief, Assignee: subAgentID, To: boardy.Doing, Effort: string(effort), Revision: revision}
		if err := t.base.Board.Store.RecordTicket(spawned); err != nil {
			t.roster.Release(subAgentID)
			return contract.Result{}, fmt.Errorf("spawn: %s's spawn was not written to %s's thread: %w", subAgentID, args.Ticket, err)
		}
	}

	scratch, err := t.scratch(ctx, subAgentID)
	if err != nil {
		t.roster.Release(subAgentID)
		return contract.Result{}, fmt.Errorf("spawn: %s's scratch folder was not made: %w", subAgentID, err)
	}
	timing.took("scratch")
	held := &heldSubAgent{agent: agent, definition: definition, effort: opened.Effort, askedEffort: effort, system: system, parts: parts,
		environment: environment + scratchWords(scratch),
		briefFiles:  t.briefFiles(context.WithValue(ctx, contract.SubAgentKey{}, subAgentID), args, site.conversation),
		boundary:    subagents.NewBoundary(subAgentID, scratch.Dir(), args.Owns), scratch: scratch, inbox: NewInbox(),
		trace: spawnTrace{definition: agent.Agent, model: agent.Model, mission: agent.Mission, owns: args.Owns, ticket: args.Ticket, depth: t.depth + 1}}
	runCtx, cancel := outlivingTheWave(ctx)
	t.Inbox.keep(held, cancel)
	t.tree.mu.Lock()
	t.tree.ran = append(t.tree.ran, Spawned{ID: subAgentID, Call: site.call, Agent: definition.Name, Slug: opened.Slug, Windows: opened.Windows, Effort: opened.Effort})
	t.tree.mu.Unlock()
	task := cited + args.Task
	if t.SubAgents.Brief != nil {
		task += t.SubAgents.Brief(definition, args.Task)
	}
	timing.took("brief")
	site.notice(site.agent, timing.words(subAgentID))
	started = true
	t.Inbox.hold(held, site.log)
	safe.Go(project.Crashes(t.Project), "sub-agent "+subAgentID, func() {
		t.background(runCtx, cancel, held, opened, site, task, core.Origin{Source: core.SourceBrief, Name: t.orchestratorID}, warm)
	})
	said := narrowed + clampedWords(effort, opened) + held.runningWords()
	if boardSizes != nil {
		if err := t.base.Board.Fire(rules.WhenAfter, rules.HookAgentSpawn, "", args.Ticket, t.base.Session); err != nil {
			said += "\n\na board hook after agent.spawn failed: " + err.Error()
		}
		said += BoardNudges(t.base.Board.Store, boardSizes)
	}
	return contract.Result{Content: said, Command: subAgentID + " running: " + agent.Mission, SubAgent: subAgentID}, nil
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
	places, err := t.base.ResolvedPlaces()
	return filepath.Join(places.State, "artifacts"), err
}

const resumedWords = "resumed by a message"

func (t *SpawnTool) background(ctx context.Context, cancel context.CancelFunc, held *heldSubAgent, opened SubAgentModel, site spawnSite, task string, taskOrigin core.Origin, warm *warmup) {
	defer cancel()
	defer warm.leaderDone(site.call)
	if opened.Close != nil {
		defer opened.Close()
	}
	ctx = context.WithValue(context.WithValue(ctx, contract.SubAgentKey{}, held.agent.ID), boardActorKey{}, t.ticketActor(cmp.Or(held.agent.Ticket, held.agent.ID)))
	if held.scratch.Root != "" {
		ctx = platformscratch.WithScratch(ctx, held.scratch)
	}
	limits := t.limits()
	if limits.CacheTTL != "" {
		ctx = anthropic.WithCacheTTL(ctx, limits.CacheTTL)
	}
	check := &checkIn{id: held.agent.ID, started: t.clock(), missed: func(run Row) []string {
		return gateMissed(t.Project, projectRecipes(t.Project), held.definition, held.boundary.Owns(), []Row{run})
	}}
	defer t.watch(held, check)()
	defer func() {
		if value := recover(); value != nil {
			told := "sub-agent " + held.agent.ID + " is " + subagents.Errored.String() + ": " + panicked{value: value, stack: debug.Stack()}.Error()
			held.agent.State = subagents.Errored
			t.roster.Reached(held.agent.ID, subagents.Errored, told)
			trace := held.trace
			trace.site = site
			_ = trace.end(held.agent.ID, subagents.Errored)
			t.settle(ctx, held, told, core.SourceReportFor, false, site.log)
		}
	}()
	var items []core.Message
	for {
		subAgent := opened.onto(t.subAgentConfig(held, site, check, limits))
		subAgent.Task, subAgent.TaskOrigin, subAgent.Arrived, subAgent.NewID = task, taskOrigin, items, func() string { return held.agent.ID }
		subAgent, held.trace.stagger = warm.stagger(ctx, site.call, subAgent)
		report, completion := t.converse(ctx, held, subAgent, site)
		next, _ := held.inbox.next(ctx, nil)
		stopping := ctx.Err() != nil
		if stopping {
			_ = held.inbox.settle()
		}
		source := core.SourceReportFor
		if completion == subagents.Done && taskOrigin.Source == core.SourceBrief {
			source = core.SourceReport
		}
		if stopping || len(next) == 0 {
			next = t.settle(ctx, held, report, source, stopping, site.log)
		}
		if len(next) == 0 {
			return
		}
		task, taskOrigin, items, warm = strings.Join(textsOf(next), taskItemsApart), next[0].origin, arrived(next), nil
		t.roster.Reached(held.agent.ID, subagents.Working, resumedWords)
	}
}

func (t *SpawnTool) settle(ctx context.Context, held *heldSubAgent, report string, source core.Source, stopping bool, log *session.Log) []inboxItem {
	stopped := stopShells(ctx, []string{held.agent.ID})
	told := stopped.told()
	if stopped.failed != nil {
		told = append(told, "a shell of its was not killed: "+stopped.failed.Error())
	}
	if len(told) > 0 && report != "" {
		report += "\n\nas it ended, " + strings.Join(told, ", ") + ". a server you need after a sub-agent reports, start yourself with bash and background: true."
	}
	next := t.Inbox.ended(held, report, source, stopping, log)
	if len(next) > 0 {
		return next
	}
	t.Inbox.mu.Lock()
	defer t.Inbox.mu.Unlock()
	if held.running {
		return nil
	}
	_ = held.scratch.Settle(report)
	if log != nil && t.base.Sessions != nil {
		held.shed(readBack(t.base.Sessions, log.ID(), held.agent.ID))
	}
	return nil
}

func (t *SpawnTool) RecordedSpawn(definition subagents.Definition, task string) (Config, error) {
	_, parts, _, err := t.SubAgents.prompt(t.base, definition, task, nil)
	if err != nil {
		return Config{}, err
	}
	held := &heldSubAgent{agent: subagents.SubAgent{ID: definition.Name}, definition: definition, parts: parts, boundary: subagents.NewBoundary(definition.Name, "", nil), inbox: NewInbox()}
	spawned := t.subAgentConfig(held, spawnSite{}, nil, SubAgentLimits{})
	spawned.Sessions = t.base.Sessions
	return spawned, nil
}

func (t *SpawnTool) offeredTools(held *heldSubAgent, site spawnSite, limits SubAgentLimits) ([]contract.Tool, agentPlan) {
	offered := func(name string) bool {
		if name == ShellToolName {
			name = bashToolName
		}
		return len(held.definition.Tools) == 0 || slices.Contains(held.definition.Tools, name)
	}
	var owned []contract.Tool
	var plan agentPlan
	defined := t.built.defined(t.base.Tools)
	for i, tool := range t.base.Tools.tools {
		definition := defined[i]
		if own, isPlan := t.ownPlan(held.agent.ID, tool); isPlan {
			tool, plan = own.tool, own
			definition = tool.Definition()
		}
		switch {
		case !offered(tool.Name()) && !strings.HasPrefix(tool.Name(), "scratch_"):
			continue
		case tool.Name() == "write" || tool.Name() == "edit":
			tool = ownedTool{tool: wroteBySubAgent{tool: tool, writes: t.writes}, boundary: held.boundary}
			definition = tool.Definition()
		case tool.Name() == "bash":
			tool = ownedShell{tool: tool, boundary: held.boundary}
			definition = tool.Definition()
		}
		owned = append(owned, watchedTool{tool: tool, held: held, definition: definition})
	}
	if held.agent.Ticket != "" && t.base.Board != nil {
		actor := t.ticketActor(held.agent.Ticket)
		if ticketTools, err := t.base.TicketTools(t.boardAs(actor, t.engineer(held.agent.ID)), actor); err == nil {
			owned = append(owned, ticketTools...)
		}
	}
	if offered(t.Name()) {
		owned = append(owned, &SpawnTool{Review: t.Review, Methods: t.Methods, SubAgents: t.SubAgents, Limits: func() SubAgentLimits { return limits }, ChecksWork: t.ChecksWork, Project: t.Project, Inbox: held.inbox, Remembered: t.Remembered,
			orchestratorID: held.agent.ID, depth: t.depth + 1, writesNothing: len(held.boundary.Owns()) == 0, base: t.base, roster: t.roster, tree: t.tree, built: t.built, writes: t.writes, stepped: t.stepped})
	}
	ask := askTool{orchestrator: t, asking: held.agent, conversation: site.conversation}
	return append(owned, watchedTool{tool: ask, held: held, definition: ask.Definition()}), plan
}

func (t *SpawnTool) subAgentConfig(held *heldSubAgent, site spawnSite, check *checkIn, limits SubAgentLimits) Config {
	owned, plan := t.offeredTools(held, site, limits)
	subAgent := t.base
	if subAgent.Plan != nil {
		subAgent.Plan = plan.shown
	}
	subAgent.Tools = claimedTools(NewRegistry(owned...), t.base.Sessions, t.base.Session)
	subAgent.Caps.MaxSteps, subAgent.Caps.MaxForks = cmp.Or(subAgent.Caps.MaxSteps, konst.SubAgentMaxSteps), konst.SubAgentMaxForks
	subAgent.Caps.WallClock = cmp.Or(limits.WallClock, konst.SubAgentWallClockSeconds*time.Second)
	held.kept.Lock()
	subAgent.System, subAgent.Parts, subAgent.Environment, subAgent.History, subAgent.Prefix = held.system, held.parts, held.environment+held.briefFiles, held.history, &held.prefix
	held.briefFiles = ""
	held.kept.Unlock()
	subAgent.References = heldReferences(t.base.References, subAgent.Parts)
	subAgent.Memory = ""
	if t.Remembered != nil {
		block, err := entries.Block(t.Project)
		if err != nil && t.base.Notify != nil {
			t.base.Notify("sub-agent " + held.agent.ID + " starts without your memory: " + err.Error())
		}
		subAgent.Memory = block
	}
	subAgent.SpawnedFrom, subAgent.Boundary, subAgent.Inbox, subAgent.Steering = t.orchestratorID, held.boundary, held.inbox, nil
	subAgent.Person = t.orchestratorAnswers(held, site)
	subAgent.AgentType = held.definition.Name
	subAgent.Session, subAgent.SessionSource, subAgent.Log, subAgent.Turn, subAgent.SpawnedBy = "", "", site.log, site.turn, site.call
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
		t.tree.mu.Lock()
		defer t.tree.mu.Unlock()
		if t.base.Step != nil {
			t.base.Step(step)
		}
		if t.stepped != nil {
			t.stepped(held.agent.ID, step)
		}
	}
	return subAgent
}

func (m SubAgentModel) onto(subAgent Config) Config {
	if m.Accounts.Pick != nil {
		subAgent.Model, subAgent.Accounts, subAgent.Spend, subAgent.Wire, subAgent.PromptCache = nil, m.Accounts, m.Spend, m.Wire, m.Cache
	}
	return subAgent
}

func (t *SpawnTool) converse(ctx context.Context, held *heldSubAgent, subAgent Config, site spawnSite) (string, subagents.Completion) {
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
	claims, sentBack, state, unbelievedAtCap, runErr := t.runRounds(ctx, subAgentCtx, held, subAgent)
	forked := held.forkedSoFar()
	asked := subAgent.Boundary.Reported()
	last := &claims[len(claims)-1]
	if err := errors.Join(began, trace.end(id, state)); err != nil {
		last.Warnings = append(last.Warnings, "this sub-agent run was not wholly recorded: "+err.Error())
	}
	report := reportOf(agent, forked, []Row{wholeRun(claims)}, state)
	report.Asked, report.Impact = asked, boardy.ImpactOf(t.SubAgents.Root, report.Wrote)
	ruledOut := report.Completion == subagents.NotPossible
	if len(asked) > 0 && state == subagents.Finished && !ruledOut {
		report.Completion = subagents.NeedsContext
	}
	if len(sentBack) > 0 {
		times := "once"
		if len(sentBack) > 1 {
			times = strconv.Itoa(len(sentBack)) + " times"
		}
		report.Findings = append(report.Findings, subagents.Finding{Bucket: subagents.Noted, Text: "sent back " + times + ": " + strings.Join(sentBack, "; then ")})
	}
	if unbelievedAtCap != "" && !ruledOut {
		report.Completion = subagents.Blocked
		report.Findings = append(report.Findings, subagents.Finding{Bucket: subagents.ActOn, Text: "not done: " + unbelievedAtCap})
	}
	t.tree.mu.Lock()
	t.tree.retain(claims)
	for _, claim := range append(slices.Clone(forked), claims...) {
		t.tree.spend += claim.TotalCostUSD
	}
	t.tree.mu.Unlock()
	if t.SubAgents.Ended != nil {
		t.SubAgents.Ended(held.definition, agent.Brief, append(forked, claims...), report, runErr == nil && state != subagents.Errored && !stoppedEarly(state, last.Outcome) && unbelievedAtCap == "")
	}
	text := report.Text(t.proseStore())
	if agent.Ticket != "" && t.base.Board != nil {
		text += t.reportedOnBoard(ctx, agent, site.call, report, stoppedEarly(state, last.Outcome))
	}
	if runs := held.runsAs(); runs != "" {
		text = agent.ID + " ran" + runs + "\n\n" + text
	}
	t.roster.Reached(agent.ID, state, text)
	held.agent.State = state
	if runErr != nil && state != subagents.Parked {
		return fmt.Sprintf("sub-agent %s is %s: %v\n\n%s", agent.ID, state, runErr, text), report.Completion
	}
	return text, report.Completion
}

func (t *SpawnTool) reportedOnBoard(ctx context.Context, agent subagents.SubAgent, call string, report SubAgentReport, stopped bool) string {
	board := t.base.Board
	sizes := board.Store.EventSizes()
	ticket, err := board.Get(agent.Ticket)
	var lines []boardy.AcceptanceLine
	var thread []boardy.Event
	if err == nil {
		lines, err = board.Store.Acceptance(ticket, nil)
	}
	if err == nil {
		thread, err = board.Store.Thread(ticket.ID)
	}
	if err != nil {
		return "\n\nthis report was not written to " + agent.Ticket + "'s thread: " + err.Error()
	}
	actor := t.ticketActor(agent.Ticket)
	said := ""
	for _, event := range thread {
		switch {
		case event.Kind == boardy.Spawned && event.Agent == agent.ID:
			said = ""
		case event.Kind == boardy.Commented && event.Actor == actor:
			said = event.Text
		}
	}
	lastLine, _, _ := strings.Cut(strings.TrimSpace(report.Prose), "\n")
	reported := boardy.Event{Ticket: agent.Ticket, Kind: boardy.Reported, Actor: actor, Author: t.engineer(agent.ID), Session: t.base.Session, Agent: agent.ID, Call: call,
		Text: firstSentences(cmp.Or(said, lastLine, report.State)), Files: report.Wrote, Verdict: verdictOf(report.Completion, stopped, lines), Acceptance: lines}
	if ticket.Status != boardy.Doing {
		reported.To = ticket.Status
	}
	written := "\n\n" + acceptancePrefix + ticket.ID + ", " + string(reported.Verdict) + ":"
	for i, line := range lines {
		written += fmt.Sprintf("\nline %d %s: %s", i+1, line.State, line.Line)
	}
	if err := board.Store.RecordTicket(reported); err != nil {
		written += "\n\nthis report was not written to " + agent.Ticket + "'s thread: " + err.Error()
	}
	if q, _, held, err := board.NotPossibleOpen(ticket); err != nil {
		written += "\n\nwhether " + ticket.ID + " holds a not-possible question could not be read: " + err.Error()
	} else if held {
		written += t.askThePerson(ctx, agent.ID, ticket, q)
	}
	if err := board.Fire(rules.WhenAfter, rules.HookAgentReport, "", agent.Ticket, actor); err != nil {
		written += "\n\na board hook after agent.report failed: " + err.Error()
	}
	return written + BoardNudges(board.Store, sizes)
}

func verdictOf(completion subagents.Completion, stopped bool, lines []boardy.AcceptanceLine) boardy.Verdict {
	switch {
	case completion == subagents.NotPossible:
		return boardy.VerdictNotPossible
	case stopped || completion == subagents.Blocked:
		return boardy.VerdictBlocked
	case completion == subagents.NeedsContext:
		return boardy.VerdictNeedsYou
	case completion == subagents.DoneWithConcerns || slices.ContainsFunc(lines, func(line boardy.AcceptanceLine) bool { return line.State == boardy.LineUnmet }):
		return boardy.VerdictConcerns
	}
	return boardy.VerdictDone
}

const acceptancePrefix = "acceptance "

func firstSentences(text string) string {
	text, most := strings.TrimSpace(text), konst.BoardReportedSentencesMost
	for i := 1; i < len(text); i++ {
		if text[i] == ' ' && strings.ContainsRune(".!?", rune(text[i-1])) {
			if most--; most == 0 {
				return text[:i]
			}
		}
	}
	return text
}

func PMAuthor(sessions *session.Store, id, slug string) (*boardy.Author, bool) {
	identity, err := sessions.Identity(id)
	if err != nil {
		return boardy.LeadAuthor(id, "", "", slug), false
	}
	return boardy.LeadAuthor(id, identity.Handle(), identity.Name, slug), true
}

func (t *SpawnTool) speaker() *boardy.Author {
	switch {
	case t.depth > 0:
		return t.engineer(t.orchestratorID)
	case t.base.Sessions == nil:
		return boardy.LeadAuthor(t.base.Session, "", "", t.base.Slug)
	}
	lead, _ := PMAuthor(t.base.Sessions, t.base.Session, t.base.Slug)
	return lead
}

func (t *SpawnTool) engineer(id string) *boardy.Author {
	agent, _ := t.roster.SubAgent(id)
	handle := ""
	if t.base.Sessions != nil {
		if identity, err := t.base.Sessions.Identity(t.base.Session); err == nil {
			handle = identity.Handle()
		}
	}
	return boardy.EngineerAuthor(t.base.Session, handle, id, agent.Model)
}

func (t *SpawnTool) boardAs(actor string, author *boardy.Author) boardy.Managed {
	board := *t.base.Board
	board.Store.Speakers = map[string]boardy.Author{actor: *author}
	return board
}

func (t *SpawnTool) onThread(ticket string, event boardy.Event) string {
	if ticket == "" || t.base.Board == nil {
		return ""
	}
	event.Ticket = ticket
	if event.Actor == t.ticketActor(ticket) {
		event.Author = t.engineer(event.Agent)
	}
	if err := t.base.Board.Store.RecordTicket(event); err != nil {
		return "\n\nthis was not written to " + ticket + "'s thread: " + err.Error()
	}
	return ""
}

func (t *SpawnTool) ticketActor(ticket string) string {
	family := t.base.Session
	if t.base.Sessions != nil {
		if identity, err := t.base.Sessions.Identity(t.base.Session); err == nil {
			family = identity.Family
		}
	}
	return family + "/" + ticket
}
