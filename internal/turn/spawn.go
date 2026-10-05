package turn

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
	Effort  llm.Effort
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

type subAgentKey struct{}

func SubAgentAsking(ctx context.Context) string {
	id, _ := ctx.Value(subAgentKey{}).(string)
	return id
}

func (s spawnTrace) begin(id string) error {
	log, site := s.site.log, s.site
	if log == nil {
		return nil
	}
	if slices.ContainsFunc(log.Header().Agents, func(run session.AgentRun) bool { return run.Agent == id }) {
		return markRun(log, id, subagent.Working, nil)
	}
	_, err := log.Append(session.Event{Turn: site.turn, Agent: site.agent, Call: site.call, Kind: session.EventSpawn},
		session.SpawnBody{Agent: id, Definition: s.definition, Model: s.model, Mission: s.mission, Owns: s.owns, Depth: s.depth})
	return errors.Join(err, log.Edit(func(header *session.Header) {
		header.Agents = append(header.Agents, session.AgentRun{Agent: id, Definition: s.definition, Model: s.model, ParentAgent: site.agent,
			SpawnCall: site.call, SpawnTurn: site.turn, Depth: s.depth, Status: subagent.Working.String(), StartedAt: time.Now()})
	}))
}

func (s spawnTrace) end(id string, state subagent.State) error {
	log := s.site.log
	if log == nil {
		return nil
	}
	ended := session.AgentEndBody{Status: state.String()}
	for _, run := range log.Header().Agents {
		if run.Agent == id {
			ended.Usage, ended.CostUSD = run.Usage, run.CostUSD
		}
	}
	_, err := log.Append(session.Event{Turn: s.site.turn, Agent: id, Kind: session.EventAgentEnd}, ended)
	at := time.Now()
	return errors.Join(err, markRun(log, id, state, &at))
}

func markRun(log *session.Log, id string, state subagent.State, ended *time.Time) error {
	return log.Edit(func(header *session.Header) {
		for i := range header.Agents {
			if header.Agents[i].Agent == id {
				header.Agents[i].Status, header.Agents[i].EndedAt = state.String(), ended
			}
		}
	})
}

type SubAgents struct {
	Defined []subagent.Definition
	Open    func(subagent.Definition, llm.Effort) (SubAgentModel, error)
	Prompt  ComposeSpec
	Root    string
	Brief   func(definition subagent.Definition, task string) string
	Ended   func(definition subagent.Definition, task string, rounds []Row, report SubAgentReport, finished bool)
}

func (s SubAgents) prompt(inherited Config, definition subagent.Definition, task string, owns []string) (string, string, error) {
	system, environment := inherited.System, inherited.Environment
	switch {
	case s.Prompt.Environment != "":
		spec := s.Prompt
		spec.Task, spec.Paths, spec.Agent, spec.Role = task, owns, definition, rule.RoleSubAgent
		owned, err := rule.Frameworks(s.Root, owns)
		if err != nil {
			return "", "", err
		}
		spec.Frameworks = append(slices.Clone(spec.Frameworks), owned...)
		slices.Sort(spec.Frameworks)
		spec.Frameworks = slices.Compact(spec.Frameworks)
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
	Running   int
	Depth     int
	WallClock time.Duration
}

type SpawnTool struct {
	Review         DoneReview
	Methods        method.Table
	SubAgents      SubAgents
	Limits         func() SubAgentLimits
	SettingsTool   bool
	Project        string
	Inbox          *Inbox
	orchestratorID string
	depth          int
	mu             sync.Mutex
	tree           *spawnTree
	base           Config
	roster         *subagent.Roster
	warmups        map[string]*warmup
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

type warmup struct {
	leader string
	once   sync.Once
	ready  chan struct{}
}

func (w *warmup) warmed() { w.once.Do(func() { close(w.ready) }) }

func (w *warmup) leaderDone(call string) {
	if w != nil && w.leader == call {
		w.warmed()
	}
}

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

func (t *SpawnTool) disjointPrefix(calls []llm.ToolCall) int {
	limit := t.limits().Running
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
		return SubAgentLimits{Running: konst.SubAgentsPerTurnDefault, Depth: konst.SubAgentDepthDefault}
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
			"hands one piece of work to a sub-agent with its own context and its own conversation, and returns at once with its name while it works in the background. " +
			"its report, rather than its transcript, comes to you later as a message naming it, and your turn may end before it does. " +
			"owns lists the paths the sub-agent may write, every other path is refused at the write, and no two sub-agents may hold overlapping paths; a sub-agent offered no write, edit or bash needs none. " +
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
	if strings.TrimSpace(args.Task) == "" {
		return Result{}, errors.New("spawn: task is required")
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
	if opened, err = t.open(definition, effort); err != nil {
		return Result{}, fmt.Errorf("spawn: %w", err)
	}
	var recorded []string
	if site.log != nil {
		for _, run := range site.log.Header().Agents {
			recorded = append(recorded, run.Agent)
		}
	}
	t.tree.mu.Lock()
	agent := subagent.SubAgent{
		ID:      t.roster.NextID(definition.Name, recorded),
		Agent:   definition.Name,
		Model:   opened.Slug,
		Mission: args.mission(),
		Brief:   args.Task,
		Owns:    args.Owns,
		Started: t.clock(),
	}
	subAgentID, holding := agent.ID, t.roster.Hold(agent)
	t.tree.mu.Unlock()
	if err := holding; err != nil {
		var collision subagent.CollisionError
		if errors.As(err, &collision) && collision.HolderReport != "" {
			return Result{Command: "handback " + collision.Holder, Content: fmt.Sprintf(
				"no sub-agent was started: %s already holds %q, and %q overlaps it. Send this work to %s with the message tool rather than starting a rival.\n\n%s has reported:\n%s",
				collision.Holder, collision.HolderGlob, collision.Glob, collision.Holder, collision.Holder, collision.HolderReport)}, nil
		}
		return Result{}, fmt.Errorf("spawn: %w", err)
	}

	held := &heldSubAgent{agent: agent, definition: definition, effort: opened.Effort, system: system, environment: environment + t.briefFiles(ctx, args, site.conversation),
		boundary: &subagent.Boundary{Ticket: subAgentID, Owns: args.Owns}, inbox: NewInbox(),
		trace: spawnTrace{definition: agent.Agent, model: agent.Model, mission: agent.Mission, owns: args.Owns, depth: t.depth + 1}}
	runCtx, cancel := context.WithCancel(ctx)
	t.Inbox.keep(held, cancel)
	t.tree.mu.Lock()
	t.tree.ran = append(t.tree.ran, Spawned{ID: subAgentID, Call: site.call, Agent: definition.Name, Slug: opened.Slug, Windows: opened.Windows, Effort: opened.Effort})
	t.tree.mu.Unlock()
	task := args.Task
	if t.SubAgents.Brief != nil {
		task += t.SubAgents.Brief(definition, args.Task)
	}
	started = true
	t.Inbox.hold(site.log)
	go t.background(runCtx, cancel, held, opened, site, task, warm)
	return Result{Content: held.runningWords(), Command: subAgentID + " running: " + agent.Mission, SubAgent: subAgentID}, nil
}

func (t *SpawnTool) clock() time.Time {
	if t.base.Now == nil {
		return time.Now()
	}
	return t.base.Now()
}

const resumedWords = "resumed by a message"

func (t *SpawnTool) background(ctx context.Context, cancel context.CancelFunc, held *heldSubAgent, opened SubAgentModel, site spawnSite, task string, warm *warmup) {
	defer cancel()
	defer warm.leaderDone(site.call)
	if opened.Close != nil {
		defer opened.Close()
	}
	ctx = context.WithValue(ctx, subAgentKey{}, held.agent.ID)
	for {
		subAgent := opened.onto(t.subAgentConfig(held, site))
		subAgent.Task, subAgent.NewID = task, func() string { return held.agent.ID }
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
		task, warm = strings.Join(next, "\n\n"), nil
		t.roster.Reached(held.agent.ID, subagent.Working, resumedWords)
	}
}

func (t *SpawnTool) subAgentConfig(held *heldSubAgent, site spawnSite) Config {
	offered := func(name string) bool {
		return len(held.definition.Tools) == 0 || slices.Contains(held.definition.Tools, name)
	}
	var owned []Tool
	for _, tool := range t.base.Tools.tools {
		switch {
		case !offered(tool.Name()):
			continue
		case tool.Name() == "write" || tool.Name() == "edit":
			tool = ownedTool{tool: tool, boundary: held.boundary}
		case tool.Name() == "bash":
			tool = ownedShell{tool: tool, boundary: held.boundary}
		}
		owned = append(owned, tool)
	}
	if offered(t.Name()) {
		owned = append(owned, &SpawnTool{Review: t.Review, Methods: t.Methods, SubAgents: t.SubAgents, Limits: t.Limits, Project: t.Project, Inbox: held.inbox,
			orchestratorID: held.agent.ID, depth: t.depth + 1, base: t.base, roster: t.roster, tree: t.tree})
	}
	subAgent := t.base
	subAgent.Tools = NewRegistry(append(owned, askTool{orchestrator: t, asking: held.agent, conversation: site.conversation})...)
	subAgent.Caps.MaxSteps, subAgent.Caps.MaxForks = cmp.Or(subAgent.Caps.MaxSteps, konst.SubAgentMaxSteps), konst.SubAgentMaxForks
	subAgent.Caps.WallClock = cmp.Or(t.limits().WallClock, konst.SubAgentWallClockSeconds*time.Second)
	subAgent.System, subAgent.Environment, subAgent.History = held.system, held.environment, held.history
	subAgent.SpawnedFrom, subAgent.Boundary, subAgent.Inbox, subAgent.Steering = t.orchestratorID, held.boundary, held.inbox, nil
	subAgent.Session, subAgent.Log, subAgent.Turn, subAgent.SpawnedBy = "", site.log, site.turn, site.call
	if site.log == nil {
		subAgent.Sessions = nil
	}
	subAgent.Step = func(step StepRow) {
		called := make([]string, len(step.ToolCalls))
		for i, call := range step.ToolCalls {
			called[i] = call.Tool
		}
		t.roster.Stepped(held.agent.ID, step.Index, t.clock(), called...)
		if t.base.Step != nil {
			t.tree.mu.Lock()
			t.base.Step(step)
			t.tree.mu.Unlock()
		}
	}
	return subAgent
}

type heldSubAgent struct {
	agent       subagent.SubAgent
	definition  subagent.Definition
	effort      llm.Effort
	system      string
	environment string
	boundary    *subagent.Boundary
	inbox       *Inbox
	trace       spawnTrace
	history     []llm.Message
	running     bool
	cancel      context.CancelFunc
	forking     sync.Mutex
	forked      []Row
}

func (h *heldSubAgent) runsAs() string {
	if h.agent.Model == "" {
		return ""
	}
	runs := " as " + cmp.Or(h.agent.Agent, "the unnamed sub-agent") + " on " + h.agent.Model
	if h.effort != "" {
		runs += " at effort " + string(h.effort)
	}
	return runs
}

func (h *heldSubAgent) runningWords() string {
	said := h.agent.ID + " is running in the background" + h.runsAs()
	if len(h.agent.Owns) > 0 {
		said += ", holding " + strings.Join(h.agent.Owns, ", ")
	}
	return said + ". its report comes to you as a message naming it when it ends, and message reaches it at its next step while it runs."
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
	if len(asked) > 0 && state != subagent.Errored && state != subagent.Parked {
		state = subagent.WaitingAnswer
	}
	last := &claims[len(claims)-1]
	if err := errors.Join(began, trace.end(id, state)); err != nil {
		last.Warnings = append(last.Warnings, "this sub-agent run was not wholly recorded: "+err.Error())
	}
	report := reportOf(agent, forked, []Row{wholeRun(claims)}, state)
	report.Asked = asked
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
	contract.Wrote = report.Wrote
	text := report.Text() + "\n\n" + contract.Block()
	if runs := held.runsAs(); runs != "" {
		text = agent.ID + " ran" + runs + "\n\n" + text
	}
	t.roster.Reached(agent.ID, state, text)
	if runErr != nil && state != subagent.Parked {
		return fmt.Sprintf("sub-agent %s is %s: %v\n\n%s", agent.ID, state, runErr, text)
	}
	return text
}

type messageTool struct {
	orchestrator *SpawnTool
}

type messageArgs struct {
	To   string `json:"to"`
	Text string `json:"text"`
	Stop bool   `json:"stop,omitempty"`
}

func (messageTool) Name() string { return "message" }

func (messageTool) Definition() llm.Tool {
	text := map[string]any{"type": "string"}
	return llm.Tool{
		Name: "message",
		Description: "sends a sub-agent more work or a correction, from this turn or any earlier one, and returns at once. " +
			"a running sub-agent reads it at its next step; one that has ended resumes in the background with its whole conversation and the paths it held, " +
			"so use it rather than spawning a new sub-agent for those paths. either way its answer comes to you later as a report, as spawn's does. " +
			"to is the sub-agent's name as its report gives it, such as ts-dev-1. " +
			"stop true, with no text, stops a running sub-agent instead: it reports where it stopped and keeps its conversation, so a later message resumes it",
		Parameters: map[string]any{
			"type":       "object",
			"properties": map[string]any{"to": text, "text": text, "stop": map[string]any{"type": "boolean"}},
			"required":   []string{"to"},
		},
	}
}

func (m messageTool) Run(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args messageArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, fmt.Errorf("message: arguments are not the expected shape: %w", err)
	}
	t := m.orchestrator
	if args.Stop {
		held, stopped := t.Inbox.stop(args.To)
		switch {
		case held == nil:
			return Result{}, t.unknown(args.To)
		case !stopped:
			return Result{}, fmt.Errorf("message refused: %s is not running, so there is nothing to stop", args.To)
		}
		return Result{Content: args.To + " is stopping. its report, saying where it stopped, comes to you as a message.", Command: "stop " + args.To, SubAgent: args.To}, nil
	}
	if strings.TrimSpace(args.Text) == "" {
		return Result{}, errors.New("message: text is required unless stop is true")
	}
	runCtx, cancel := context.WithCancel(ctx)
	held, posted, err := t.Inbox.resume(args.To, args.Text, t.limits().Running, cancel)
	if held == nil || err != nil || posted {
		cancel()
	}
	switch {
	case held == nil:
		return Result{}, t.unknown(args.To)
	case err != nil:
		return Result{}, fmt.Errorf("message %w", err)
	case posted:
		return Result{Content: args.To + " is running and reads this at its next step. its report comes to you as a message when it ends.", Command: "message " + args.To, SubAgent: args.To}, nil
	}
	opened, err := t.open(held.definition, held.effort)
	if err == nil {
		t.tree.mu.Lock()
		err = t.rehold(held.agent)
		t.tree.mu.Unlock()
	}
	if err != nil {
		if opened.Close != nil {
			opened.Close()
		}
		t.Inbox.ended(held, "", true, nil)
		cancel()
		return Result{}, fmt.Errorf("message refused: %w", err)
	}
	site, _ := ctx.Value(spawnSiteKey{}).(spawnSite)
	t.Inbox.hold(site.log)
	go t.background(runCtx, cancel, held, opened, site, args.Text, nil)
	return Result{Content: args.To + " resumes in the background with its conversation. its report comes to you as a message when it ends.", Command: "message " + args.To, SubAgent: args.To}, nil
}

type subAgentsTool struct {
	orchestrator *SpawnTool
}

func (subAgentsTool) Name() string { return "subagents" }

func (subAgentsTool) Definition() llm.Tool {
	return llm.Tool{
		Name: "subagents",
		Description: "lists every sub-agent of this session and what it is doing now, and returns at once without waiting for any of them: " +
			"its name, definition, state, effort, how long it has run or ran, its step and tool call counts, and the last tool it called. " +
			"use it rather than guessing whether a sub-agent still runs",
		Parameters: map[string]any{"type": "object", "properties": map[string]any{}},
	}
}

func (l subAgentsTool) Run(context.Context, json.RawMessage) (Result, error) {
	t := l.orchestrator
	effort := map[string]llm.Effort{}
	for _, ran := range t.Spawned() {
		effort[ran.ID] = ran.Effort
	}
	now, listed := t.clock(), "no sub-agent has been spawned in this session"
	var lines []string
	for _, agent := range t.roster.SubAgents() {
		state, ran, until := agent.State.String(), "ran", agent.Active
		switch agent.State {
		case subagent.Working, subagent.InReview, subagent.Reopened:
			state, ran, until = "running", "has run", now
			if agent.State != subagent.Working {
				state += " " + agent.State.String()
			}
		case subagent.WaitingAnswer, subagent.Parked, subagent.Errored, subagent.Finished:
		}
		line := fmt.Sprintf("%s: %s, %s, %s %s, %d steps, %d tool calls", agent.ID, cmp.Or(agent.Agent, "no definition"), state, ran,
			until.Sub(agent.Started).Round(time.Second), agent.Steps, len(agent.Calling)+agent.CallsDropped)
		if level := effort[agent.ID]; level != "" {
			line += ", effort " + string(level)
		}
		if len(agent.Calling) > 0 {
			line += ", last tool " + agent.Calling[len(agent.Calling)-1]
		}
		lines = append(lines, line+": "+agent.Mission)
	}
	if len(lines) > 0 {
		listed = strings.Join(lines, "\n")
	}
	return Result{Content: listed, Command: "subagents"}, nil
}

func (t *SpawnTool) unknown(name string) error {
	var names []string
	for _, known := range t.roster.SubAgents() {
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
		if other.ID != agent.ID && other.State != subagent.Finished && other.State != subagent.Errored {
			_ = others.Hold(other)
		}
	}
	if err := others.Hold(agent); err != nil {
		return err
	}
	t.roster.Reached(agent.ID, subagent.Working, resumedWords)
	return nil
}

var briefPath = regexp.MustCompile(`[\w./-]*\w\.[A-Za-z0-9]+`)

type briefCandidate struct {
	path   string
	byLead bool
}

func (t *SpawnTool) briefFiles(ctx context.Context, args spawnArgs, lead []llm.Message) string {
	read, readable := t.base.Tools.byName["read"]
	if !readable {
		return ""
	}
	if memoised, cached := read.(interface{ Uncached() Tool }); cached {
		read = memoised.Uncached()
	}
	var candidates []briefCandidate
	for _, path := range briefPath.FindAllString(args.Task, -1) {
		candidates = append(candidates, briefCandidate{path: path})
	}
	for _, message := range lead {
		for _, call := range message.ToolCalls {
			var leadRead readArgs
			if call.Name == "read" && json.Unmarshal(call.Arguments, &leadRead) == nil {
				candidates = append(candidates, briefCandidate{path: leadRead.Path, byLead: true})
			}
		}
	}
	var text strings.Builder
	var placed, skipped []string
	for _, candidate := range candidates {
		raw, _ := json.Marshal(readArgs{Path: candidate.path})
		result, err := read.Run(ctx, raw)
		if err != nil {
			continue
		}
		file := ledgerKey(result.Command)
		if owned, _ := subagent.Matches(file, args.Owns); slices.Contains(placed, file) || candidate.byLead && !owned {
			continue
		}
		placed = append(placed, file)
		body := result.Content
		if repaired := ledgerKey(candidate.path) != file; repaired {
			_, body, _ = strings.Cut(body, "\n")
		}
		if len(skipped) > 0 || text.Len()+len(body) > konst.SubAgentReferenceBytes {
			skipped = append(skipped, file)
			continue
		}
		heading := "the brief names " + file + ", so it is read for you:\n"
		if candidate.byLead {
			heading = "the lead read " + file + ", inside the paths you hold, so it is read for you as it is now:\n"
		}
		text.WriteString("\n\n" + heading + body)
	}
	if len(skipped) > 0 {
		fmt.Fprintf(&text, "\n\nthese files, named in the brief or read by the lead inside the paths you hold, are not read for you to stay within %d bytes, so read them before you change them: %s", konst.SubAgentReferenceBytes, strings.Join(skipped, ", "))
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

func (t *spawnTree) retain(rows []Row) {
	t.rows = append(t.rows, rows...)
	for i := range len(t.rows) - konst.SubAgentRetainedRows {
		released := t.rows[i].Summary()
		released.Conversation = nil
		t.rows[i] = released
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
		missed := gateMissed(t.Project, recipes, held.definition, held.boundary.Owns, claims)
		if len(missed) == 0 && t.Review == nil {
			break
		}
		last := &claims[len(claims)-1]
		why := strings.Join(missed, "; ")
		decision := DoneDecision{Verdict: DoneReopen, Reason: fmt.Sprintf("you changed a %s file, and your gate is %s, each run after your last edit and passing: %s",
			held.definition.Language, strings.Join(held.definition.Gate, " and "), why)}
		if len(missed) == 0 {
			t.roster.Reached(agent.ID, subagent.InReview, reportOf(agent, held.forkedSoFar(), []Row{wholeRun(claims)}, subagent.InReview).Text())
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

func gateMissed(project string, recipes map[string]string, definition subagent.Definition, owns []string, rounds []Row) []string {
	if definition.Language == "" {
		return nil
	}
	var sinceEdit []ToolCallRow
	lastEdit := ""
	for _, round := range rounds {
		for _, step := range round.Steps {
			for _, call := range step.ToolCalls {
				sinceEdit = append(sinceEdit, call)
				if path := writtenPath(call); path != "" && rule.LanguageOf(path) == definition.Language {
					sinceEdit, lastEdit = nil, path
				}
			}
		}
	}
	if lastEdit == "" {
		return nil
	}
	if !filepath.IsAbs(lastEdit) {
		lastEdit = filepath.Join(project, lastEdit)
	}
	noTests := false
	if project != "" && slices.Contains(definition.Gate, "test") {
		_, refused := vitestPackage(filepath.Dir(lastEdit))
		noTests = strings.HasPrefix(refused.FailureText, testNoTests)
	}
	var missed []string
	for _, check := range definition.Gate {
		said := check + " did not run"
		if check == "test" && noTests {
			said = ""
		}
		for _, call := range sinceEdit {
			switch {
			case !gateRan(check, call, recipes), call.Error == typecheckUnanswered:
			case call.ExitCode != nil && *call.ExitCode != 0:
				said = fmt.Sprintf("%s exited %d", check, *call.ExitCode)
			case call.Tool == "typecheck" && call.Error != "" && !typecheckNamesOwnFile(project, owns, call, rounds):
				said = ""
			case call.Error != "" && !strings.HasPrefix(call.Error, testNoTests):
				said = check + " failed"
			default:
				said = ""
			}
		}
		if said != "" {
			missed = append(missed, said)
		}
	}
	return missed
}

func typecheckNamesOwnFile(project string, owns []string, call ToolCallRow, rounds []Row) bool {
	printed := ""
	for _, round := range rounds {
		for _, message := range round.Conversation {
			if message.Role == llm.RoleTool && message.ToolCallID == call.Call {
				printed = message.Content
			}
		}
	}
	_, listed, hasList := strings.Cut(printed, ":\n")
	scope := call.Command
	if !filepath.IsAbs(scope) {
		scope = filepath.Join(project, scope)
	}
	tsconfig, found := findUp(scope, tsconfigName)
	if len(owns) == 0 || !hasList || !found {
		return true
	}
	for _, line := range strings.Split(listed, "\n") {
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, " ") {
			continue
		}
		path, position, _ := strings.Cut(line, ":")
		if located, _, tsc := strings.Cut(line, "): error TS"); tsc {
			path = located[:max(strings.LastIndex(located, "("), 0)]
		} else if position == "" || position[0] < '0' || position[0] > '9' {
			return true
		}
		relative, err := filepath.Rel(project, filepath.Join(filepath.Dir(tsconfig), path))
		if mine, _ := subagent.Matches(filepath.ToSlash(relative), owns); path == "" || err != nil || mine {
			return true
		}
	}
	return false
}

func gateRan(check string, call ToolCallRow, recipes map[string]string) bool {
	switch {
	case call.Tool == check:
		return true
	case call.Tool != "bash":
		return false
	}
	toolCheck := check == "test" || check == "typecheck"
	if !toolCheck && commandRuns(call.Command, check) {
		return true
	}
	for invocation, recipe := range recipes {
		meetsCheck := commandRuns(recipe, check)
		if toolCheck {
			meetsCheck = strings.HasSuffix(invocation, " "+check)
		}
		if meetsCheck && commandRuns(call.Command, invocation) {
			return true
		}
	}
	return false
}

func commandRuns(command, check string) bool {
	want := strings.Fields(check)
	for _, segment := range strings.FieldsFunc(command, func(r rune) bool { return r == '&' || r == '|' || r == ';' }) {
		words := strings.Fields(segment)
		for len(words) > 0 && strings.Contains(words[0], "=") {
			words = words[1:]
		}
		for stripped := true; stripped; words, stripped = withoutRunner(words) {
			prefixed := len(words) >= len(want) && slices.Equal(words[:len(want)], want)
			made := len(want) == 2 && want[0] == "make" && len(words) > 1 && words[0] == "make" && slices.Contains(words[1:], want[1])
			if prefixed || made {
				return true
			}
		}
	}
	return false
}

func withoutRunner(words []string) ([]string, bool) {
	if len(words) > 1 && words[0] == "cargo" && strings.HasPrefix(words[1], "+") {
		return append([]string{"cargo"}, words[2:]...), true
	}
	for _, runner := range []string{"uv run", "poetry run", "pdm run", "hatch run", "rye run", "pipenv run", "python -m", "python3 -m", "py -m", "npx", "pnpm exec", "pnpm dlx", "bunx", "yarn", "rtk proxy", "rtk"} {
		prefix := strings.Fields(runner)
		if len(words) <= len(prefix) || !slices.Equal(words[:len(prefix)], prefix) {
			continue
		}
		rest := words[len(prefix):]
		for len(rest) > 1 && strings.HasPrefix(rest[0], "-") {
			rest = rest[1:]
		}
		return rest, true
	}
	return nil, false
}

func projectRecipes(project string) map[string]string {
	if project == "" {
		return nil
	}
	recipes := map[string]string{}
	add := func(invokers []string, name, command string) {
		for _, invoker := range invokers {
			recipes[invoker+" "+name] += command + " ; "
		}
	}
	read := func(file string) string {
		raw, _ := os.ReadFile(filepath.Join(project, file))
		return strings.ReplaceAll(string(raw), "\r", "")
	}
	var targets []string
	for _, line := range strings.Split(read("Makefile"), "\n") {
		names, _, isRule := strings.Cut(line, ":")
		switch {
		case strings.HasPrefix(line, "\t"):
			for _, target := range targets {
				add([]string{"make"}, target, strings.TrimLeft(strings.TrimSpace(line), "@-+"))
			}
		case isRule && !strings.Contains(names, "=") && !strings.HasPrefix(line[len(names)+1:], "="):
			targets = strings.Fields(names)
		default:
			targets = nil
		}
	}
	var pkg struct {
		Scripts map[string]string `json:"scripts"`
	}
	if json.Unmarshal([]byte(read("package.json")), &pkg) == nil {
		for name, command := range pkg.Scripts {
			add([]string{"npm run", "npm", "pnpm run", "pnpm", "yarn", "bun run"}, name, command)
		}
	}
	scripts := false
	for _, line := range strings.Split(read("pyproject.toml"), "\n") {
		line = strings.TrimSpace(line)
		name, command, entry := strings.Cut(line, "=")
		switch {
		case strings.HasPrefix(line, "["):
			scripts = line == "[project.scripts]" || strings.HasPrefix(line, "[tool.") && strings.HasSuffix(line, ".scripts]")
		case scripts && entry:
			add([]string{"uv run", "poetry run", "pdm run", "hatch run", "rye run", "pipenv run"}, strings.TrimSpace(name), strings.Trim(strings.TrimSpace(command), `"'[]`))
		}
	}
	return recipes
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
	definition.Description += ", and every file the command writes, through a redirect, tee, cp or mv, has to be inside the paths your first message says you hold. " +
		"A command writing outside them is refused before it runs, and that work goes back to the orchestrator. A source file changes with edit or write, never through the shell. Reading anything is fine."
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
