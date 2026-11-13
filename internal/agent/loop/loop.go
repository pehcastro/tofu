package loop

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"math"
	"path/filepath"
	"runtime/debug"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"tofu/internal/agent/context/recall"
	hook "tofu/internal/agent/hooks"
	"tofu/internal/agent/subagents"
	"tofu/internal/judge/ledger"
	"tofu/internal/konst"
	skill "tofu/internal/librarian/skills"
	"tofu/internal/models/llm/core"
	"tofu/internal/models/transport"
	"tofu/internal/platform/credential"
	"tofu/internal/platform/outside"
	"tofu/internal/platform/project"
	"tofu/internal/platform/safe"
	"tofu/internal/store/session"
	"tofu/internal/store/walls"
	"tofu/internal/tools/agents/done"
	"tofu/internal/tools/agents/forget"
	"tofu/internal/tools/agents/note"
	"tofu/internal/tools/code/search"
	"tofu/internal/tools/contract"
	"tofu/internal/tools/history/shadow"
	"tofu/internal/tools/human/plan"
	"tofu/internal/tools/shell/process"
	shipped "tofu/library"
)

const theResponseHitTheOutputTokenLimit = "the response hit the output token limit, so its arguments may be truncated. " +
	"Re-issue the tool call with complete arguments."

const nobodyAnswersThisTurn = "nobody is here to answer this turn: tofu run has no person and a cron job runs unattended, so take the recommended option where it can be undone, or end the turn on the question"

type ListedOnly struct {
	Tool contract.Tool
	Why  string
}

func (t ListedOnly) Name() string { return t.Tool.Name() }

func (t ListedOnly) Definition() core.Tool { return t.Tool.Definition() }

func (t ListedOnly) Run(context.Context, json.RawMessage) (contract.Result, error) {
	return contract.Result{}, fmt.Errorf("%s refused: %s. it stays listed so the tool list matches the one this session's cache was written with", t.Tool.Name(), t.Why)
}

const memoryAddedSince = "added to memory outside this session since its memory was sent, and held from now on:\n"

func memoryUnsent(memory string, carried []core.Message, inbox *Inbox) []string {
	said := make([]string, 0, len(carried))
	for _, message := range carried {
		said = append(said, message.Content)
	}
	if inbox != nil {
		inbox.mu.Lock()
		for _, item := range inbox.items {
			said = append(said, item.text)
		}
		inbox.mu.Unlock()
	}
	var unsent []string
	for _, line := range strings.Split(memory, "\n") {
		head, text, cut := strings.Cut(line, "] ")
		at := strings.Index(head, "[memory#")
		ref, text := head[max(at, 0):]+"]", strings.TrimSuffix(strings.TrimSpace(text), ".")
		if !cut || at < 0 || ref == "[memory#"+memoryLineShape+"]" {
			continue
		}
		if !slices.ContainsFunc(said, func(message string) bool { return strings.Contains(message, ref) && strings.Contains(message, text) }) {
			unsent = append(unsent, line)
		}
	}
	return unsent
}

const theTurnEndsOnACleanReport = "this turn ends on a clean sub-agent report, so it runs no tool"

const andThisIsItsLastStep = ", and this is its last step: answer now from what you already have, " +
	"saying what you did, what is left undone, and what to do next. no tool runs on this step."

const theLastStepRunsNoTool = "this was the turn's last step, which runs no tool"

const aMessageArrivedMidTurn = "the person wrote to you while you were working. answer them first, in the reply you write now, before any tool call, board work or spawn: " +
	"answer from what you know now, never wait on your own running work or a sub-agent, and never promise the answer for later. " +
	"research it after that answer, in this turn. take in what they told you in their own terms. " +
	"never call their message irrelevant or say it changes nothing."

type openQuestion struct {
	said      string
	step      int
	toolsHeld bool
	endHeld   bool
	restated  bool
}

func (q openQuestion) refusal() string {
	return "the person's message is answered before any tool call: answer \"" + q.said + "\" first, in the text of your reply"
}

func (q openQuestion) sendBack() string {
	return "the person asked \"" + q.said + "\" at step " + strconv.Itoa(q.step) + " and has no answer yet: answer it now, from what you know now, " +
		"before any tool call, board work or spawn. never wait on your own running work or a sub-agent; research goes after the answer, in this turn."
}

const yourLastReplyWasEmpty = "[your last reply was empty: no text and no tool call. Answer the person, or call a tool]"

const theReplyWasCut = "[the reply above was cut off before it finished, and what it streamed up to the cut is kept]"

const thePlanAsItStands = "[your plan as it stands, shown by tofu before every step, not the person's words]\n"

type SentNow struct{}

func (SentNow) Error() string {
	return "the person sent a queued message now, so this request was dropped and the step asked again with it"
}

type panicked struct {
	value any
	stack []byte
}

func (p panicked) Error() string {
	return fmt.Sprintf("panic: %v\n\n%s", p.value, p.stack)
}

type leadWaits struct {
	mu     sync.Mutex
	now    func() time.Time
	open   int
	since  time.Time
	closed time.Duration
}

func (w *leadWaits) begin() func() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.open == 0 {
		w.since = w.now()
	}
	w.open++
	return func() {
		w.mu.Lock()
		defer w.mu.Unlock()
		if w.open--; w.open == 0 {
			w.closed += w.now().Sub(w.since)
		}
	}
}

func (w *leadWaits) total() time.Duration {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.open == 0 {
		return w.closed
	}
	return w.closed + w.now().Sub(w.since)
}

func Run(ctx context.Context, config Config) (ran Row, runErr error) {
	if config.Model == nil && config.Accounts.Pick == nil {
		return Row{}, errors.New("turn: no model")
	}
	if strings.TrimSpace(config.Task) == "" {
		return Row{}, errors.New("turn: no task")
	}
	if config.ResultBytesCap <= 0 {
		return Row{}, errors.New("turn: no tool result byte cap")
	}
	if config.Person != nil {
		config.Person = config.Person.Clocked
	}
	if config.SpawnedFrom == "" {
		ctx = context.WithValue(ctx, leadPersonKey{}, config.Person)
	}
	if config.Spend != SpendSubscription && config.Spend != SpendAPIKey {
		return Row{}, errors.New("turn: the row has to say which arm paid, subscription or api_key")
	}
	if depth := processDepth(); depth > shippedSubAgentProcessDepth {
		return Row{}, ProcessDepthLimitError{Depth: depth, Limit: shippedSubAgentProcessDepth}
	}
	config, err := sideChat(config)
	if err != nil {
		return Row{}, err
	}
	if config.Places, err = config.ResolvedPlaces(); err != nil {
		return Row{}, err
	}
	dir := cmp.Or(config.ArtifactDir, filepath.Join(config.Places.State, "artifacts"))
	artifacts, err := NewArtifacts(dir, !config.TruncateResults)
	if err != nil {
		return Row{}, err
	}
	artifacts.preview = artifacts.preview.OnWire(config.Wire)
	artifacts.standing = newStandingJudge(config).orders
	artifacts.keep = newKeepJudge(config.Project, config.Places.Log, config.Task, config.Notify)
	artifacts.keep.index = cmp.Or(config.Drops, artifacts.keep.index)
	sifter := siftOrNothing(config.Sift)
	if config.Redactor == nil {
		live := new(credential.HeldKeys).Live()
		config.Redactor = &live
	}
	redactor := *config.Redactor
	source := config.ToolSource
	if source == nil {
		source = func() Registry { return config.Tools }
	}
	var recorded *record
	personAnswers := config.personAnswers()
	references := &loadedReferences{held: map[string]string{}}
	if config.referencesOffered() {
		maps.Copy(references.held, config.References)
	}
	running := func() runningTools {
		return runningTools{fetch: artifacts.FetchTool(), history: historyTool{sessions: config.Sessions, current: func() string { return recorded.session() }}, references: references}
	}
	currentTools := func() Registry {
		return NewRegistry(config.answerable(config.withLoopTools(source().tools, running()))...)
	}
	sending := func(definitions []core.Tool) []core.Tool {
		return slices.DeleteFunc(slices.Clone(config.Prefix.toolsOr(definitions)), func(tool core.Tool) bool { return config.unoffered(tool.Name) })
	}
	now := config.Now
	if now == nil {
		now = time.Now
	}
	newID := config.NewID
	if newID == nil {
		newID = func() string { return NewID(now()) }
	}
	budget := config.Budget
	if budget == (recall.Budget{}) {
		budget = recall.Budget{Bands: recall.ShippedBands(), Source: "this turn was given no context budget"}
	}
	model, account := config.Model, Account{}
	if config.Accounts.Pick != nil {
		if account, err = config.Accounts.Pick(ctx); err != nil {
			return Row{}, err
		}
		config.tookAccount(AccountTaken{ID: account.ID, Reason: AccountPicked})
		if account.Model != nil {
			model = account.Model
		}
	}
	if model == nil {
		return Row{}, errors.New("turn: the account this session was pinned to came with no model")
	}
	start := now()
	waits := &leadWaits{now: now}
	if person := config.Person; config.Caps.WallClock > 0 && person != nil {
		config.Person = func(ctx context.Context, request GateRequest, decision GateDecision) (PersonAnswer, error) {
			defer waits.begin()()
			return person(ctx, request, decision)
		}
	}
	origin := newID()
	headTools := NewRegistry(config.withLoopTools(source().tools, running())...).Definitions()
	keptPrefix := config.Prefix
	config.Prefix = keptPrefix.pending()
	held := config.holdPrefix(headTools, artifacts.preview)
	config.Instructions = held.instructions
	sentTools := sending(currentTools().Definitions())
	usedTools := make([]string, len(sentTools))
	for i, tool := range sentTools {
		usedTools[i] = tool.Name
	}
	sentSchemas, err := json.Marshal(sentTools)
	if err != nil {
		return Row{}, err
	}
	row := Row{ID: origin, Schema: SchemaVersion, At: start, Task: config.Task, Wire: config.Wire, Spend: config.Spend, Root: origin, Account: account.ID, SpawnedFrom: config.SpawnedFrom, SpawnedBy: config.SpawnedBy, Budget: budget, System: held.head, Tools: usedTools, ToolSchemas: string(sentSchemas), Parts: held.parts}

	upgraded := config.upgradeNote()
	recorded, err = openRecord(config, row)
	if err != nil {
		return Row{}, err
	}
	row.Session = recorded.session()
	if config.Sessions != nil {
		artifacts.keep.session = recorded.session
		artifacts.keep.events = func(id string) ([]session.Event, error) {
			read, err := config.Sessions.Events(id)
			if err != nil {
				recorded.notice("the re-read matcher could not read session " + id + " for earlier drops, so the next miss reads it again: " + err.Error())
				return nil, err
			}
			recorded.notice(fmt.Sprintf("the re-read matcher read %d events of session %s for earlier drops", len(read), id))
			return read, nil
		}
		if len(config.History) == 0 {
			artifacts.keep.index.carriesNothing(row.Session)
		}
		defer func() { artifacts.keep.index.remember(recorded.session(), artifacts.keep.dropped) }()
	}
	taskOrigin := config.taskOrigin(start)
	recorded.begin(row, taskOrigin)
	estimated, endedRows := recorded.estimate(config, start), []Row(nil)
	if len(config.History) > 0 {
		recorded.listChange("carried", "the turn starts on the conversation the last turn or the resume left", nil, config.History)
	}
	var turnFiles *shadow.Started
	if config.Project != "" {
		turn := shadow.Turn{ID: row.ID, Session: row.Session, Agent: "lead", SpawnedBy: config.SpawnedBy}
		if config.SpawnedFrom != "" {
			turn.Agent = cmp.Or(config.AgentType, "sub-agent")
		}
		if config.Boundary != nil {
			turn.Agent, turn.Owns = strings.TrimSpace(turn.Agent+" "+config.Boundary.Ticket), config.Boundary.Owns()
		}
		calls := func() []shadow.Call {
			var calls []shadow.Call
			for _, step := range row.Steps {
				for _, call := range step.ToolCalls {
					calls = append(calls, shadow.Call{ID: cmp.Or(call.Call, call.ID), Args: call.Args})
				}
			}
			return calls
		}
		begun := shadow.At(config.Places.State, config.Project).Begin(ctx, turn, calls)
		turnFiles, ctx = &begun, begun.Into(ctx)
	}

	hooks, untrustedHooks := hookEngine(ctx, config, origin)
	ctx = search.WithTurnTask(context.WithValue(ctx, hookEngineKey{}, hooks), config.Task)
	row.Warnings = append(row.Warnings, untrustedHooks...)
	var hookWarnings sync.Mutex
	hear := func(ctx context.Context, in hook.Input) hook.Verdict {
		in.Session, in.Turn = row.Session, row.ID
		if config.SpawnedFrom != "" {
			in.Agent, in.AgentType = origin, config.AgentType
		}
		beforeAnyTool := in.Event == hook.SessionStart || in.Event == hook.UserPromptSubmit
		if beforeAnyTool && slices.ContainsFunc(hooks.Hooks(), func(one hook.Hook) bool { return one.Event == in.Event }) {
			if turnFiles != nil {
				_ = turnFiles.Begun()
			}
			if recorded != nil {
				recorded.undoStarted()
			}
		}
		return hooks.Fire(ctx, in)
	}
	keepHooks := func(call string, verdict hook.Verdict) {
		hookWarnings.Lock()
		defer hookWarnings.Unlock()
		for _, warning := range verdict.Warnings {
			if !slices.Contains(row.Warnings, warning) {
				row.Warnings = append(row.Warnings, warning)
			}
		}
		for _, ran := range verdict.Runs {
			ran.Decision, ran.Problem, ran.Stderr = redactor.Redact(ran.Decision), redactor.Redact(ran.Problem), redactor.Redact(ran.Stderr)
			recorded.add(session.Event{Kind: session.EventHook, Call: call}, ran)
		}
	}
	fire := func(ctx context.Context, in hook.Input) hook.Verdict {
		verdict := hear(ctx, in)
		keepHooks(in.CallID, verdict)
		return verdict
	}
	ctx = context.WithValue(ctx, hookFireKey{}, fire)

	messages := make([]core.Message, 0, len(config.History)+2)
	if held.system != "" {
		messages = append(messages, core.Message{Role: core.RoleSystem, Content: held.system})
	}
	afterSystem := len(messages)
	var keeper episodeKeeper
	for _, tool := range config.Tools.tools {
		if found, keeps := tool.(episodeKeeper); keeps && config.SpawnedFrom == "" {
			keeper = found
		}
	}
	episode := func(kind, text string) {
		if keeper == nil || kind == "" || strings.TrimSpace(text) == "" {
			return
		}
		if err := keeper.Keep(kind, text); err != nil {
			row.Warnings = append(row.Warnings, "the episode log did not keep this "+kind+" item: "+err.Error())
		}
	}
	var recording sync.Mutex
	compacting := config.Inbox.compactionScope()
	compactionAsk := func(into *record, index int, charge func(float64)) RecordedAsk {
		var compactions *record
		if into != nil {
			compactions = &record{log: into.log, scope: into.scope, turn: into.turn, agent: into.agent, spawnedBy: into.spawnedBy, said: map[string]string{}, announce: into.announce, lastSent: &messagesKept{}}
		}
		return func(ctx context.Context, why, wire string, model Model, request core.Request) (core.Decision, error) {
			if err := compacting.Err(); err != nil {
				return core.Decision{}, err
			}
			id, started := session.NewEventID(), time.Now()
			recording.Lock()
			compactions.sent(id, why, request, artifacts.preview.OnWire(wire))
			recording.Unlock()
			tapped, tap := core.Tapped(ctx)
			decision, timing, err := askCountingAttempts(tapped, model, request)
			recording.Lock()
			defer recording.Unlock()
			compactions.exchange(id, why, wire, request, started, tap.Attempts(), decision, err)
			into.wall(walls.OpMemoryRequest, "memory model", started, nil, err)
			if compactions != nil {
				into.failed, compactions.failed = append(into.failed, compactions.failed...), nil
			}
			charge(decision.Usage.Cost)
			if err == nil {
				spent := stepFrom(id, index, timing, decision)
				spent.AssistantText = ""
				into.step(session.EventMemoryRequest, spent)
			}
			return decision, err
		}
	}
	answered := func(reply string) {
		episode(episodeOfLead, reply)
		if keeper == nil {
			return
		}
		const notSummarized = "the episode view was not summarized after this turn: "
		index := len(row.Steps)
		later, holds := recorded.heldAgain()
		release := func() {
			if later != nil {
				later.inbox.letGo(later.log)
			}
		}
		background := laterCompaction{drop: release, run: func() {
			defer release()
			if compacting.Err() != nil {
				return
			}
			if err := keeper.Compact(compacting, compactionAsk(later, index, func(float64) {})); err != nil && compacting.Err() == nil {
				later.notice(notSummarized + err.Error())
			}
		}}
		if holds && config.Inbox.compactLater(background) {
			return
		}
		release()
		if compacting.Err() != nil {
			return
		}
		if err := keeper.Compact(compacting, compactionAsk(recorded, index, func(cost float64) { row.TotalCostUSD += cost })); err != nil {
			row.Warnings = append(row.Warnings, notSummarized+err.Error())
		}
	}
	episodesFor := func(sized recall.Budget, head []core.Message, saidElsewhere []string) string {
		config.Inbox.compacted()
		room := (sized.Bands.Target() - sized.Tokens(artifacts.preview, historyOf(head))) * artifacts.preview.BytesPerThousandTokens / 1000
		allowed := min(room*konst.EpisodeViewPercentOfRoom/100, konst.MemtreeViewBytes)
		if allowed < konst.MemtreeLineBytes {
			return ""
		}
		view, err := keeper.Episodes(allowed, saidElsewhere)
		if err != nil {
			row.Warnings = append(row.Warnings, "the episode view is not carried: "+err.Error())
		}
		return view
	}
	carried, view := slices.DeleteFunc(slices.Clone(config.History), func(message core.Message) bool {
		return message.Origin.Source == core.SourceSpawnLine || held.composedNow && promptChange(message)
	}), []core.Message(nil)
	fresh, was := config.MemoryMessage(), newestMemoryView(carried)
	if config.Prefix.memoryTurnedBackOn(fresh, was) || config.opensGeneration() || len(fresh) == 0 {
		if len(fresh) > 0 && (len(was) == 0 || was[0].Content != fresh[0].Content) {
			recorded.message(fresh[0], "", nil)
		}
		carried = withMemoryViewAtHead(carried, fresh)
	} else {
		carried = withMemoryViewAtHead(carried, was)
		if unsent := memoryUnsent(config.Memory, carried, config.Inbox); len(unsent) > 0 {
			view = []core.Message{{Role: core.RoleUser, Content: memoryAddedSince + strings.Join(unsent, "\n"), Origin: core.Origin{Source: core.SourceMemorySaved, TakenAt: start}}}
		}
	}
	if schemas, err := json.Marshal(headTools); err == nil && keeper != nil && len(config.History) == 0 {
		view = withEpisodes(view, episodesFor(budget.Sending(artifacts.preview, string(schemas)), slices.Concat(messages, carried, view), nil))
	}
	messages = slices.Concat(messages, carried, view)
	if held.changed != "" {
		messages = append(messages, core.Message{Role: core.RoleUser, Content: held.changed, Origin: core.Origin{Source: core.SourceEnvironment, TakenAt: start}})
	}
	concluding, taken := "", ""
	var lead *SpawnTool
	for _, tool := range config.Tools.tools {
		if spawner, spawning := tool.(*SpawnTool); spawning {
			if config.SpawnedFrom == "" {
				lead = spawner
			}
			if spawner.ChecksWork {
				continue
			}
			if concluding = spawner.cleanReport(config.Task); concluding == "" {
				taken = spawner.reportTaken(config.Task)
			}
		}
	}
	var promptRefused string
	var hooked []core.Message
	if config.SpawnedFrom == "" {
		if config.SessionSource != "" {
			hooked = sessionStartMessages(config.SessionSource, fire(ctx, hook.Input{Event: hook.SessionStart, Source: config.SessionSource}).Context, start)
		}
		if kept := withoutOlderCompactContext(config.History, hooked); len(kept) < len(config.History) {
			recorded.listChange("carried", "the compact context this session starts with replaces the one the conversation carried", config.History, kept)
			carried, messages = withoutOlderCompactContext(carried, hooked), withoutOlderCompactContext(messages, hooked)
		}
		prompted := fire(ctx, hook.Input{Event: hook.UserPromptSubmit, Prompt: config.Task})
		promptRefused, hooked = prompted.Block, append(hooked, hookMessages(core.SourcePromptSubmit, prompted.Context, start)...)
	}
	var boardLine string
	arm, _ := ctx.Value(leadCheckInKey{}).(func(*LeadCheckIn))
	if lead == nil {
		arm = nil
	}
	if arm != nil {
		arm(nil)
	}
	if isReport(config.TaskOrigin) {
		boardLine = lead.boardLine(config.Task)
	}
	messages = append(append(messages, config.opening(taskOrigin, concluding+taken, boardLine, upgraded)...), hooked...)
	episode(episodeKind(taskOrigin), config.Task)

	var written, shadowed sync.WaitGroup
	var shadows sync.Mutex
	var shadowIDs, shadowErrs []string
	var forkWrites sync.Mutex
	var forkWriteErrs []string
	sent, answering := afterSystem+len(carried), ""
	results := map[string]ToolCallRow{}
	refuse := func(step *StepRow, calls []core.ToolCall, why string) {
		for _, call := range calls {
			callRow, resultMessage := rejectedCall(call, time.Now(), "tool call "+strconv.Quote(call.Name)+" was not executed: "+why,
				session.EventIDFor(origin, call.ID), step.id, row.author())
			callRow.Refused = true
			step.ToolCalls, results[call.ID] = append(step.ToolCalls, callRow), callRow
			resultMessage.Content = withToolRef(callRow.ID, resultMessage.Content)
			messages = append(messages, resultMessage)
		}
	}
	flush := func() {
		for _, message := range messages[min(sent, len(messages)):] {
			recorded.message(message, answering, results)
		}
		sent = max(sent, len(messages))
	}
	sentTokens := func(messages []core.Message) int {
		return budget.Tokens(artifacts.preview, historyOf(messages))
	}
	var lastAnswered time.Time
	var lastAttempts []core.Attempt
	ask := func(ctx context.Context, why string, on Model, request core.Request) (core.Decision, requestTiming, string, error) {
		id, started := session.NewEventID(), time.Now()
		request.Turn = row.ID
		if tokens := sentTokens(request.Messages); tokens > budget.Ceiling() {
			err := fmt.Errorf("this request is about %d tokens, past the %d token ceiling after the trim and the fork, so it was not sent", tokens, budget.Ceiling())
			recorded.exchange(id, why, config.Wire, request, started, nil, core.Decision{}, err)
			return core.Decision{}, requestTiming{}, id, err
		}
		recorded.sent(id, why, request, artifacts.preview)
		if keptPrefix != nil {
			*keptPrefix = *config.Prefix
		}
		tapped, tap := core.Tapped(ctx)
		decision, timing, err := askCountingAttempts(tapped, on, request)
		lastAttempts = tap.Attempts()
		recorded.exchange(id, why, config.Wire, request, started, lastAttempts, decision, err)
		if err == nil {
			lastAnswered = now()
		}
		return decision, timing, id, err
	}
	inserted := func(origin core.Origin, text string) core.Message {
		origin.TakenAt = now()
		return core.Message{Role: core.RoleUser, Content: text, Origin: origin}
	}
	withPlan := func(messages []core.Message) []core.Message {
		if config.Plan == nil {
			return messages
		}
		shown := config.Plan()
		if shown == "" {
			return messages
		}
		return append(slices.Clip(messages), inserted(core.Origin{Source: core.SourcePlan}, thePlanAsItStands+shown))
	}
	compactContext := func() []core.Message {
		if config.SpawnedFrom != "" {
			return nil
		}
		return sessionStartMessages(hook.SessionCompact, fire(ctx, hook.Input{Event: hook.SessionStart, Source: hook.SessionCompact}).Context, now())
	}
	trims, shownReads := !config.NoCompaction && budget.Automatic, &contract.ReadWindow{}
	trimFrom := func(step int, when string, from int, trigger string, started time.Time) (*Compaction, error) {
		before := slices.Clone(messages)
		compaction, shrink, err := trimRead(artifacts, budget, step, messages, from)
		if compaction != nil {
			shownReads.Rebuilt()
			flush()
			added := compactContext()
			messages = withoutOlderCompactContext(messages, added)
			sent = len(messages)
			recorded.listChange("compaction", when+": the conversation crossed its budget, so tofu "+shrink.String(), before, messages)
			messages = append(messages, added...)
		}
		if compaction != nil || err != nil {
			recorded.wall(walls.OpCompaction, trigger, started, nil, err)
		}
		return compaction, err
	}
	landsFrom := func(step, from, tokens int) bool {
		holdingNothing := Artifacts{preview: artifacts.preview, handles: artifacts.handles}
		reached, _, err := trimRead(holdingNothing, budget, step, slices.Clone(messages), from)
		return err == nil && reached != nil && reached.TokensAfter <= tokens
	}
	compactionOffSaid, forkOffSaid := false, false
	sayCompactionOff := func(when string, crossed func() bool) {
		if config.NoCompaction && !compactionOffSaid && crossed() {
			compactionOffSaid = true
			recorded.notice(when + ": the conversation crossed its budget and compaction is off, so no tool result is trimmed this turn")
		}
	}
	trimUnderTarget := func(step int, when string) (*Compaction, error) {
		started := time.Now()
		if !trims {
			sayCompactionOff(when, func() bool { return sentTokens(messages) > budget.Bands.Target() })
			return nil, nil
		}
		if sentTokens(messages) <= budget.Bands.Target() {
			return nil, nil
		}
		firstMissing := sort.Search(len(messages)+1, func(from int) bool { return !landsFrom(step, from, budget.Bands.Target()) })
		return trimFrom(step, when, max(firstMissing-1, 0), "trim to target", started)
	}
	trim := func(step int, when string) (*Compaction, error) {
		started := time.Now()
		if !trims {
			sayCompactionOff(when, func() bool { return landsFrom(step, 0, budget.Bands.Target()*konst.ContextTrimPercentOfTarget/100) })
			return nil, nil
		}
		from := 0
		cacheWarm := lastAnswered.IsZero() || now().Sub(lastAnswered) < konst.TrimColdAfterIdleMinutes*time.Minute
		if cacheWarm && sentTokens(messages) <= budget.Ceiling() {
			from = warmFrom(artifacts.preview, messages)
		}
		if !landsFrom(step, from, budget.Bands.Target()*konst.ContextTrimPercentOfTarget/100) {
			return nil, nil
		}
		return trimFrom(step, when, from, "trim", started)
	}
	sayForkOff := func(when string) {
		if !forkOffSaid && budget.Crossed(artifacts.preview, historyOf(messages)) {
			forkOffSaid = true
			recorded.notice(when + ": the conversation crossed its fork threshold and fork is off, so it carries on unforked this turn")
		}
	}
	stateCarried := func(fork *Fork, step int, beforeFork, begun []core.Message, tools []core.Tool, moved *Account) ([]core.Message, string) {
		begun = withMemoryViewAtHead(begun, config.MemoryMessage())
		if unended := unendedSubAgents(source().tools); len(unended) > 0 {
			at := slices.IndexFunc(begun, func(message core.Message) bool {
				return message.Origin.Source == core.SourceForkCarry && message.Content == fork.Carry.Text
			})
			fork.Carry = fork.Carry.WithSubAgents(unended)
			begun[at].Content = fork.Carry.Text
		}
		unreported := budget.Reported(0, recall.Occupancy{})
		if keeper != nil {
			begun = withoutEpisodes(begun)
			begun = withEpisodes(begun, episodesFor(unreported, begun, fork.Carry.Said))
			carryState(fork, begun, "", row.Session)
			fork.TokensAfter = unreported.Tokens(artifacts.preview, historyOf(begun))
			return begun, ""
		}
		asked := beforeFork
		if sentTokens(asked) > budget.Ceiling() {
			asked, budget = begun, unreported
		}
		request := core.Request{Messages: append(slices.Clone(asked), inserted(core.Origin{Source: core.SourceForkState}, forkStateAsk())), Tools: tools}
		if len(tools) > 0 {
			request.ToolChoice = core.ToolChoiceNone
		}
		asking, why := model, "the working state the fork carries"
		if moved != nil {
			why += ", asked on account #" + strconv.FormatInt(moved.ID, 10) + ", the one this session moves to"
			if moved.Model != nil {
				asking = moved.Model
			}
		}
		decision, timing, id, err := ask(context.WithValue(ctx, quietAskKey{}, true), why, asking, request)
		row.TotalCostUSD += decision.Usage.Cost
		if err == nil {
			spent := stepFrom(id, step, timing, decision)
			spent.AssistantText = ""
			recorded.step(session.EventRequest, spent)
		}
		state, missing := "", ""
		switch {
		case err != nil:
			missing = "the fork carries no working state, because asking for it failed: " + err.Error()
		case decision.Outcome != core.OutcomeMessage || strings.TrimSpace(decision.Content) == "":
			missing = "the fork carries no working state, because the model answered with none"
		default:
			state = runeSafeHead(strings.TrimSpace(decision.Content), konst.ForkStateBytes)
		}
		carryState(fork, begun, state, row.Session)
		fork.TokensAfter = unreported.Tokens(artifacts.preview, historyOf(begun))
		return begun, missing
	}
	keep := func(step StepRow) {
		step.Phase = PhaseOf(step.ToolCalls)
		flush()
		recorded.step(session.EventRequest, step)
		row.Steps = append(row.Steps, step)
		if config.Step != nil {
			config.Step(step)
		}
	}
	registry := ShellRegistryFrom(ctx)
	beforeShells := registrySnapshot(registry)
	finish := func(outcome Outcome) Row {
		row.Outcome = outcome
		row.Conversation = messages[afterSystem:]
		flush()
		written.Wait()
		shadowed.Wait()
		row.DecisionIDs = append(row.DecisionIDs, shadowIDs...)
		row.Warnings = append(row.Warnings, shadowErrs...)
		if survivors := backgroundSurvivors(registry, beforeShells); survivors != "" {
			row.Warnings = append(row.Warnings, survivors)
		}
		for _, failed := range forkWriteErrs {
			row.Warnings = append(row.Warnings, "the session this one forked from was not written: "+failed)
		}
		for _, tool := range currentTools().tools {
			if spawner, spawning := tool.(*SpawnTool); spawning && spawner.depth == 0 {
				ended, spent := spawner.tree.bill()
				row.SubAgentIDs = append(row.SubAgentIDs, ended...)
				row.TotalCostUSD += spent
			}
		}
		if turnFiles != nil {
			if config.Boundary != nil {
				turnFiles.Turn.Owns = config.Boundary.Owns()
			}
			snapshotStarted := time.Now()
			clocked, clock := outside.Open(context.WithoutCancel(ctx), time.Now)
			timed, cancel := context.WithTimeout(clocked, konst.ShadowSnapshotTimeoutMillis*time.Millisecond)
			notes, err := turnFiles.End(timed)
			cancel()
			recorded.wall(walls.OpUndoSnapshot, "turn end", snapshotStarted, clock, err)
			row.Warnings = append(row.Warnings, notes...)
			switch begun := turnFiles.Begun(); {
			case begun != nil:
				row.Warnings = append(row.Warnings, "the shadow store did not record where this turn started, so changes cannot list what it edits: "+begun.Error())
			case err != nil:
				row.Warnings = append(row.Warnings, "the shadow store did not commit this turn's edits, so changes cannot list them: "+err.Error())
			}
		}
		row.WallClockMS = now().Sub(start).Milliseconds()
		recorded.settle(estimated, outcome, time.Duration(row.WallClockMS)*time.Millisecond, append(endedRows, row))
		if recorded != nil {
			for _, failed := range recorded.failed {
				row.Warnings = append(row.Warnings, "part of this turn was not recorded as it ran: "+failed)
			}
		}
		recorded.end(row)
		return row
	}
	fail := func(err error) (Row, error) {
		if errors.Is(err, context.Canceled) {
			row.Error = redactor.Redact(cmp.Or(context.Cause(ctx), err).Error())
			return finish(OutcomeCancelled), err
		}
		row.Error = redactor.Redact(err.Error())
		return finish(OutcomeError), err
	}
	defer func() {
		if value := recover(); value != nil {
			stack := debug.Stack()
			if caught, rethrown := value.(safe.Panic); rethrown {
				value, stack = caught.Value, caught.Stack
			}
			ran, runErr = fail(panicked{value: value, stack: stack})
		}
	}()
	endAt := func(outcome Outcome, lead string, history []core.Message) Row {
		messages = history
		if config.NoLastWord {
			return finish(outcome)
		}
		messages = append(slices.Clone(history), inserted(core.Origin{Source: core.SourceLastWord}, lead+andThisIsItsLastStep))
		request := core.Request{Messages: messages, Tools: sending(currentTools().Definitions())}
		if len(request.Tools) > 0 {
			request.ToolChoice = core.ToolChoiceNone
		}
		decision, timing, id, err := ask(ctx, "the last word: "+lead, model, request)
		row.TotalCostUSD += decision.Usage.Cost
		if err != nil {
			row.Warnings = append(row.Warnings, lead+", and the last answer was not obtained: "+err.Error())
			return finish(outcome)
		}
		row.Model = decision.Build
		last := stepFrom(id, len(row.Steps)+1, timing, decision)
		answering = last.id
		if decision.Content != "" || len(decision.ToolCalls) > 0 {
			messages = append(messages, core.Message{Role: core.RoleAssistant, Content: decision.Content, ToolCalls: decision.ToolCalls, Thinking: decision.Thinking})
		}
		refuse(&last, decision.ToolCalls, theLastStepRunsNoTool)
		switch {
		case decision.Outcome == core.OutcomeTruncated:
			row.Warnings = append(row.Warnings, lead+", and the last answer hit the output token limit partway through")
		case strings.TrimSpace(decision.Content) == "":
			row.Warnings = append(row.Warnings, lead+", and the last answer carried no text")
		}
		keep(last)
		return finish(outcome)
	}
	flush()
	if promptRefused != "" {
		return fail(errors.New("a UserPromptSubmit hook refused this prompt: " + promptRefused))
	}
	schemas, err := json.Marshal(sending(currentTools().Definitions()))
	if err != nil {
		return fail(err)
	}
	budget = budget.Sending(artifacts.preview, string(schemas))
	if least := sentTokens(append(slices.Clone(messages[:afterSystem]), messages[len(messages)-1])); least > budget.Ceiling() {
		return fail(&recall.CeilingTooLow{CeilingTokens: budget.Ceiling(), PromptTokens: sentTokens(messages[:afterSystem]), LeastTokens: least})
	}
	guard := newLoopGuard(config.Caps)
	forks, stopContinuations, handbacks := 0, 0, 0
	overTargetAfter := 0
	var lastFork *Fork
	lastForkAt := 0
	lastForkWhy := func(fork *Fork, at int) string {
		previous, before, beforeAt := overTargetAfter, lastFork, lastForkAt
		overTargetAfter = 0
		if fork != nil {
			lastFork, lastForkAt = fork, at
		}
		switch {
		case fork == nil:
			return ""
		case config.Caps.MaxForks > 0 && forks+1 >= config.Caps.MaxForks:
			return "this turn reached its cap of " + strconv.Itoa(config.Caps.MaxForks) + " forks, and the work is not finished"
		case fork.Kind != ForkContinuation:
			return ""
		case fork.TokensAfter <= budget.Bands.Target():
			if before == nil || before.Kind != ForkContinuation || at-beforeAt > 1 {
				return ""
			}
			return fmt.Sprintf("this turn stopped forking: fork %d comes one step after fork %d, which left the conversation %d tokens under its %d token target while that step took %d, so a fork does not buy room for one step",
				forks+1, forks, budget.Bands.Target()-before.TokensAfter, budget.Bands.Target(), fork.TokensBefore-before.TokensAfter)
		}
		overTargetAfter = fork.TokensAfter
		if previous == 0 || fork.TokensAfter < previous {
			return ""
		}
		return fmt.Sprintf("this turn stopped forking: fork %d leaves the conversation at %d tokens, over its %d token target and no smaller than the fork before it, so another fork would not help",
			forks+1, fork.TokensAfter, budget.Bands.Target())
	}
	heldForStep, forkStarted := 0, time.Time{}
	forkFrom := func(forced ForkKind) (*Fork, []core.Message, error) {
		forkStarted = time.Now()
		landing := budget
		heldForStep = 0
		if forced == "" && budget.Crossed(artifacts.preview, historyOf(messages)) {
			step := 0
			if last := lastAssistant(messages); last >= 0 {
				step = sentTokens(messages) - sentTokens(messages[:last])
			}
			heldForStep = stepHold(artifacts.preview, budget.Bands, step)
			landing.Bands = budget.Bands.Less(heldForStep)
		}
		fork, begun, err := forkHistory(artifacts, landing, config.forkOpening(taskOrigin), messages, forced, forks+1, config.Caps.MaxForks)
		if err != nil {
			recorded.wall(walls.OpCompaction, "fork "+string(cmp.Or(forced, ForkContinuation)), forkStarted, nil, err)
		}
		return fork, begun, err
	}
	var recordedGrants []subagents.Question
	if config.Boundary != nil {
		recordedGrants = config.Boundary.Asked()
	}
	crashes := project.Crashes(config.Project)
	recordPictureDrops := func(step, tokensBefore int, after []core.Message, drops []recall.Drop) {
		artifacts.keep.remember(drops)
		recorded.add(session.Event{Kind: session.EventCompaction}, compactionRow{Compaction: &Compaction{Step: step,
			TokensBefore: tokensBefore, TokensAfter: sentTokens(after), Drops: drops}})
	}
	forkInto := func(fork *Fork, when string, beforeFork, begun []core.Message, moved *Account) {
		begun = withNotesKept(beforeFork, begun)
		ended := row
		ended.Outcome, ended.ForkedInto, ended.EndedInFork, ended.Conversation = OutcomeForked, fork.Into, fork, messages[afterSystem:]
		ended.WallClockMS = now().Sub(start).Milliseconds()
		endedRows = append(endedRows, ended)
		if config.EndedSession != nil {
			written.Add(1)
			safe.Go(crashes, "ended session write", func() {
				defer written.Done()
				if err := safe.Call(crashes, "ended session write", func() error { return config.EndedSession(ended) }); err != nil {
					forkWrites.Lock()
					forkWriteErrs = append(forkWriteErrs, err.Error())
					forkWrites.Unlock()
				}
			})
		}
		row = Row{
			ID:          fork.Into,
			Schema:      SchemaVersion,
			At:          now(),
			Task:        config.Task,
			Wire:        config.Wire,
			Model:       row.Model,
			Spend:       config.Spend,
			Root:        origin,
			Account:     account.ID,
			ForkedFrom:  ended.ID,
			ForkKind:    fork.Kind,
			SpawnedFrom: config.SpawnedFrom,
			SpawnedBy:   config.SpawnedBy,
			Budget:      budget,
			System:      row.System,
			Tools:       row.Tools,
			ToolSchemas: row.ToolSchemas,
			Parts:       row.Parts,
		}
		if moved != nil {
			row.Account = moved.ID
			row.Warnings = append(row.Warnings, movedAccountWords(account, *moved, fork.TokensAfter))
			config.tookAccount(AccountTaken{ID: moved.ID, From: account.ID, Reason: AccountMoved})
			account = *moved
			if moved.Model != nil {
				model = moved.Model
			}
		}
		recorded.fork(ended, row, fork, row.At)
		recorded.carry(estimated)
		row.Session = recorded.session()
		held := ""
		if heldForStep > 0 {
			held = fmt.Sprintf(", %d of its %d token target held back for the next step and the working state", heldForStep, budget.Bands.Target())
		}
		added := compactContext()
		begun = withoutOlderCompactContext(begun, added)
		recorded.listChange(session.ListChangeFork, string(fork.Kind)+" fork "+strconv.Itoa(forks)+" "+when+" from "+ended.Session+", "+
			strconv.Itoa(fork.TokensBefore)+" to "+strconv.Itoa(fork.TokensAfter)+" tokens, "+strconv.Itoa(fork.TailMessages)+" tail messages"+held, beforeFork, begun)
		messages, sent, answering, budget = begun, 0, "", budget.Reported(0, recall.Occupancy{})
		messages = append(messages, added...)
		shownReads.Rebuilt()
		flush()
		if drops := picturesLeftOut(beforeFork, messages); len(drops) > 0 {
			recordPictureDrops(fork.Step, sentTokens(beforeFork), messages, drops)
		}
		recorded.wall(walls.OpCompaction, "fork "+string(fork.Kind), forkStarted, nil, nil)
	}
	endForked := func(fork *Fork, when string, beforeFork, begun []core.Message, moved *Account, why string) Row {
		forks++
		fork.Into = origin + "-f" + strconv.Itoa(forks+1)
		forkInto(fork, when, beforeFork, begun, moved)
		row.ForkStop = why
		return endAt(OutcomeForkStop, why, messages)
	}
	askedAgainAfterBlank, polled, nudged, graced := false, false, false, false
	steeredAt := 0
	var left []string
	var asked *openQuestion
	holdFor := func(stepRow *StepRow, why string) {
		told := why + ", so it was sent back to answer the person's message from step " + strconv.Itoa(asked.step)
		stepRow.Warnings = append(stepRow.Warnings, told)
		recorded.notice(told)
		messages = append(messages, inserted(core.Origin{Source: core.SourceAnswerFirst}, asked.sendBack()))
	}
	var steer *oneQuestion
	answeredBy := func(reply string) (bool, error) {
		if strings.TrimSpace(reply) == "" || asked.restated {
			return strings.TrimSpace(reply) != "", nil
		}
		if steer == nil {
			places, err := config.ResolvedPlaces()
			if err != nil {
				return false, err
			}
			if steer, err = newSteerJudge(config.Project, places.Log); err != nil {
				return false, err
			}
		}
		return steer.reached(ctx, cmp.Or(row.Session, config.Session), struct {
			Said  string `json:"said"`
			Reply string `json:"reply"`
		}{asked.said, reply})
	}
	noticeStep := config.Caps.MaxSteps - max(1, int(math.Ceil(float64(config.Caps.MaxSteps)*konst.TurnStepCapNoticeShare)))
	underForgetShare := !slices.ContainsFunc(messages, forgetOffered) || sentTokens(messages[:len(messages)-1]) <= forgetShareTokens(budget)
	for step := 1; ; step++ {
		if err := ctx.Err(); err != nil {
			return fail(err)
		}
		if config.Steering != nil {
			for _, steered := range config.Steering() {
				if steeredAt != step {
					messages = append(messages, inserted(core.Origin{Source: core.SourceAnswerFirst}, aMessageArrivedMidTurn))
					first, _, _ := strings.Cut(strings.TrimSpace(steered), "\n")
					asked = &openQuestion{said: runeSafeHead(first, konst.LeadHandbackQuoteBytes), step: step}
					recorded.notice("open question from the person at step " + strconv.Itoa(step) + ": " + asked.said)
				}
				steeredAt = step
				said := inserted(core.Origin{Source: core.SourceSteer}, steered)
				if config.Inbox != nil {
					notes := config.Inbox.notesOn(ctx, config.Sessions, cmp.Or(row.Session, config.Session), steered, messages, config.Task)
					asked.restated = asked.restated || strings.Contains(notes, requestOpenNote)
					said.Content += notesOn([]string{notes})
				}
				if config.ImagesOf != nil {
					said.Images = config.ImagesOf(steered)
				}
				messages = append(messages, said)
				episode(episodeOfPerson, steered)
			}
		}
		if config.Inbox != nil {
			for _, item := range config.Inbox.fold(config.TaskOrigin) {
				if isReport(item.origin) {
					if line := lead.boardLine(item.text); line != "" {
						item.text += "\n\n" + line
					}
				}
				messages = append(messages, inserted(item.origin, item.text))
				episode(episodeKind(item.origin), item.text)
				if item.origin.Source == core.SourceMemorySaved {
					recorded.noted(item.origin, item.text)
				}
			}
		}
		capped := config.Caps.MaxSteps > 0
		switch {
		case config.Caps.WallClock > 0 && now().Sub(start)-waits.total() >= config.Caps.WallClock:
			lead := "this sub-agent retired at its wall clock cap of " + config.Caps.WallClock.String() + ", and the work is not finished"
			row.Warnings = append(row.Warnings, lead)
			return endAt(OutcomeRetiredWallClockCap, lead, messages), nil
		case capped && step >= config.Caps.MaxSteps:
			lead := "this turn reached its step cap of " + strconv.Itoa(config.Caps.MaxSteps) + ", and the work is not finished"
			return endAt(OutcomeStepCap, lead, messages), nil
		case capped && step == noticeStep:
			messages = append(messages, inserted(core.Origin{Source: core.SourceStepCap}, "this turn has "+strconv.Itoa(config.Caps.MaxSteps-step+1)+
				" steps left before its step cap of "+strconv.Itoa(config.Caps.MaxSteps)+", and the last of them runs no tool: "+
				"finish what you are doing, or stop and report what is done and what is left."))
		}

		stepTools := currentTools()
		definitions := sending(stepTools.Definitions())
		schemas, err := json.Marshal(definitions)
		if err != nil {
			return fail(err)
		}
		budget = budget.Sending(artifacts.preview, string(schemas))
		before := "before step " + strconv.Itoa(step)
		var trimmedBefore *Compaction
		if due := step == 1 && len(config.History) > 0 || sentTokens(messages) > budget.Ceiling(); due {
			if trimmedBefore, err = trim(step, before); err != nil {
				return fail(err)
			}
			if !config.NoFork {
				beforeFork := slices.Clone(messages)
				fork, begun, err := forkFrom("")
				if err != nil {
					return fail(err)
				}
				if why := lastForkWhy(fork, step-1); why != "" {
					return endForked(fork, before, beforeFork, begun, nil, why), nil
				}
				if fork != nil {
					forks++
					fork.Into = origin + "-f" + strconv.Itoa(forks+1)
					begun, missing := stateCarried(fork, step, beforeFork, begun, definitions, nil)
					forkInto(fork, before, beforeFork, begun, nil)
					if missing != "" {
						row.Warnings = append(row.Warnings, missing)
					}
				}
			} else {
				sayForkOff(before)
			}
			unforked, err := trimUnderTarget(step, before)
			if err != nil {
				return fail(err)
			}
			trimmedBefore = cmp.Or(unforked, trimmedBefore)
		}
		if picturesToDrop(messages) > 0 {
			tokensBefore := sentTokens(messages)
			drops := keepNewestPictures(messages)
			flush()
			recorded.listChange("pictures dropped", "step "+strconv.Itoa(step)+" sends the newest "+strconv.Itoa(konst.ImageResultsKept)+
				" pictures, so tofu dropped the "+strconv.Itoa(len(drops))+" oldest", nil, messages)
			recordPictureDrops(step, tokensBefore, messages, drops)
		}
		used := sentTokens(messages)
		crossed := underForgetShare && used > forgetShareTokens(budget)
		underForgetShare = used <= forgetShareTokens(budget)
		if crossed && slices.ContainsFunc(definitions, func(tool core.Tool) bool { return tool.Name == forget.Name }) {
			offer := forgetOffer(messages, artifacts.preview, used, budget.Ceiling())
			recorded.notice(offer)
			messages = append(messages, inserted(core.Origin{Source: core.SourceNotice, Name: forget.Name}, offer))
		}
		measureSent := func() recall.Occupancy {
			sent := historyOf(messages)
			sent.ToolSchemas = string(schemas)
			return recall.Measure(artifacts.preview, budget.Bands, sent)
		}
		asSent := measureSent()
		decision, timing, requestID, err := ask(ctx, "step "+strconv.Itoa(step), model, core.Request{Messages: withPlan(messages), Tools: definitions})
		if errors.Is(err, SentNow{}) {
			step--
			continue
		}
		if overflowed(err) {
			before, started := slices.Clone(messages), time.Now()
			shrink, shrinkErr := shrinkOverflow(artifacts, messages, asSent.Total(), budget.WindowTokens)
			recorded.wall(walls.OpCompaction, "overflow shrink", started, nil, shrinkErr)
			shownReads.Rebuilt()
			flush()
			recorded.listChange("overflow shrink", "the context window overflowed on step "+strconv.Itoa(step), before, messages)
			switch {
			case shrinkErr != nil:
				err = shrinkErr
			case shrink.results == 0:
				err = fmt.Errorf("the context window overflowed and no old tool result was left to shrink, so the turn ends: %w", err)
			default:
				told := "the context window overflowed, so tofu " + shrink.String() + ", and asked once more"
				row.Warnings = append(row.Warnings, told)
				recorded.notice(told)
				if config.Notify != nil {
					config.Notify(told)
				}
				asSent = measureSent()
				decision, timing, requestID, err = ask(ctx, "step "+strconv.Itoa(step)+", asked again after the overflow shrink", model, core.Request{Messages: withPlan(messages), Tools: definitions})
				if overflowed(err) {
					err = fmt.Errorf("the context window overflowed again after tofu %s, so the turn ends: %w", shrink, err)
				}
			}
		}
		for tried := []Account{account}; transport.KindOf(err) == transport.KindRateLimit; tried = append(tried, account) {
			next, rotated := config.Accounts.rotate(ctx, account, tried)
			var told, on string
			if rotated {
				told, on = rotatedWords(account, next), "account #"+strconv.FormatInt(next.ID, 10)
				config.tookAccount(AccountTaken{ID: next.ID, From: account.ID, Reason: AccountMoved})
			} else {
				moved, unmoved, movedOn := config.Accounts.elsewhere(ctx, left, asSent.Total())
				if !movedOn {
					limited := config.Accounts.limited(ctx, config.Wire, tried, lastAttempts, now(), err)
					limited.Continuity, err = unmoved, limited
					break
				}
				if moved.Wire != config.Wire {
					messages, artifacts.preview = WithoutThinking(messages), artifacts.preview.OnWire(moved.Wire)
				}
				told, on, next, left = moved.words(), moved.To, moved.Account, append(left, moved.From)
				config.Accounts, config.Wire, config.Spend, row.Wire, row.Spend = moved.Accounts, moved.Wire, moved.Spend, moved.Wire, moved.Spend
				if moved.Budget != (recall.Budget{}) {
					budget = moved.Budget.Sending(artifacts.preview, string(schemas))
				}
			}
			row.Warnings = append(row.Warnings, told)
			recorded.notice(told)
			if config.Notify != nil {
				config.Notify(told)
			}
			account, row.Account = next, next.ID
			if next.Model != nil {
				model = next.Model
			}
			decision, timing, requestID, err = ask(ctx, "step "+strconv.Itoa(step)+", asked again on "+on+" after a rate limit", model,
				core.Request{Messages: withPlan(messages), Tools: definitions})
		}
		if err != nil {
			var cut core.CutReply
			if errors.As(err, &cut) {
				answering = requestID
				messages = append(messages, core.Message{Role: core.RoleAssistant, Content: cut.Streamed}, inserted(core.Origin{Source: core.SourceReplyCut}, theReplyWasCut))
			}
			return fail(err)
		}
		row.Model = decision.Build
		row.TotalCostUSD += decision.Usage.Cost
		reported := decision.PromptAccounting.ContextTokens(decision.Usage.InputTokens, decision.CacheReadTokens, decision.CacheWriteTokens)
		budget = budget.Reported(reported, asSent)

		stepRow := stepFrom(requestID, step, timing, decision)
		stepRow.Compaction = trimmedBefore
		answering = stepRow.id
		measuredAgainst := budget.Bands
		stepRow.Occupancy, stepRow.Bands = &asSent, &measuredAgainst
		if decision.Outcome == core.OutcomeMessage && strings.TrimSpace(decision.Content) == "" {
			if reasoned, _, _ := strings.Cut(strings.TrimSpace(decision.Thinking.Text), "\n"); reasoned != "" {
				told := "the model ended its turn with nothing to say, so the turn ends here; its reasoning was: " + reasoned
				stepRow.Warnings = append(stepRow.Warnings, told)
				recorded.notice(told)
				if config.Notify != nil {
					config.Notify(told)
				}
				keep(stepRow)
				return finish(OutcomeStopped), nil
			}
			if askedAgainAfterBlank {
				keep(stepRow)
				blank := transport.Fail("turn.Run", transport.KindInvalidAnswer, nil, "the model answered twice in a row with no text and no tool call")
				blank.Code, blank.Said, blank.Account.Source = transport.EmptyAnswer, "twice in a row it sent no text, no tool call and no reasoning", decision.Build
				return fail(blank)
			}
			askedAgainAfterBlank = true
			stepRow.Warnings = append(stepRow.Warnings, "the model answered with no text and no tool call, so it was asked again")
			messages = append(messages, inserted(core.Origin{Source: core.SourceBlankAnswer}, yourLastReplyWasEmpty))
			keep(stepRow)
			continue
		}
		askedAgainAfterBlank = false
		if asked != nil {
			answered, err := answeredBy(decision.Content)
			switch {
			case err != nil:
				recorded.notice("the person's question from step " + strconv.Itoa(asked.step) + " counts as answered by the reply at step " + strconv.Itoa(step) + ", because jev could not judge it: " + err.Error())
				asked = nil
			case answered && asked.restated:
				recorded.notice("the person's open request from step " + strconv.Itoa(asked.step) + " is answered by the restatement at step " + strconv.Itoa(step))
				asked = nil
			case answered:
				recorded.notice("the person's question from step " + strconv.Itoa(asked.step) + " was answered at step " + strconv.Itoa(step))
				asked = nil
			}
		}

		switch decision.Outcome {
		case core.OutcomeMessage:
			messages = append(messages, core.Message{Role: core.RoleAssistant, Content: decision.Content})
			stop := hook.Stop
			if config.SpawnedFrom != "" {
				stop = hook.SubagentStop
			}
			blocked := fire(ctx, hook.Input{Event: stop, LastMessage: decision.Content, StopActive: stopContinuations > 0}).Block
			if blocked != "" && stopContinuations < konst.HookStopContinuations {
				stopContinuations++
				messages = append(messages, inserted(core.Origin{Source: core.SourceStopHook}, "a "+string(stop)+" hook kept this turn going: "+blocked))
				keep(stepRow)
				continue
			}
			if blocked != "" {
				row.Warnings = append(row.Warnings, "the turn ended with a "+string(stop)+" hook still blocking after "+strconv.Itoa(stopContinuations)+" continuations: "+blocked)
			}
			unmet := done.Unmet(ctx, messages)
			if len(unmet) > 0 && !graced {
				graced = true
				stepRow.Warnings = append(stepRow.Warnings, "done read a line as no, so the turn did not end and was given one grace turn: "+strings.Join(unmet, "; "))
				messages = append(messages, inserted(core.Origin{Source: core.SourceHandback}, unmetNote(unmet)))
				keep(stepRow)
				continue
			}
			if len(unmet) > 0 {
				row.Warnings = append(row.Warnings, "the turn ended after its one grace turn with done still reading no: "+strings.Join(unmet, "; "))
			}
			if asked != nil && !asked.endHeld {
				asked.endHeld = true
				holdFor(&stepRow, "the lead tried to end the turn before answering the person")
				keep(stepRow)
				continue
			}
			if offered := handedBack(decision.Content); offered != "" && config.SpawnedFrom == "" && concluding == "" && handbacks < konst.LeadHandbackContinuations {
				handbacks++
				stepRow.Warnings = append(stepRow.Warnings, "the lead ended by handing the person a step, so it was asked to take it: "+offered)
				asking := stepTools
				if !personAnswers {
					asking = NewRegistry()
				}
				messages = append(messages, inserted(core.Origin{Source: core.SourceHandback}, handbackNote(offered, asking)))
				keep(stepRow)
				continue
			}
			if open := config.Inbox.stillAsked(); len(open) > 0 && !polled {
				polled = true
				messages = append(messages, inserted(core.Origin{Source: core.SourceHandback}, pollNote(open)))
				keep(stepRow)
				continue
			}
			work, held := lead.work()
			if held && work.open() && concluding == "" && !nudged {
				nudged = true
				asks := work.asks(leadEndedOpening, contract.Listed(work.review))
				checked, err := DoneDecision{Verdict: DoneAccepted}, error(nil)
				if lead.Review != nil {
					stopping := row
					stopping.Task, stopping.Steps, stopping.Outcome = config.Task+"\n\n"+asks, append(slices.Clone(row.Steps), stepRow), OutcomeStopped
					checked, err = lead.decided(ctx, stopping)
				}
				if checked.ID != "" {
					row.DecisionIDs = append(row.DecisionIDs, checked.ID)
				}
				switch {
				case err != nil:
					recorded.notice("stop_check did not run on the lead's stop, so the board nudge alone decides it: " + err.Error())
				case checked.Verdict == DoneReopen:
					stepRow.Warnings = append(stepRow.Warnings, "stop_check read the lead's stop as unfinished with board work open, so it was sent back: "+checked.Reason)
					messages = append(messages, inserted(core.Origin{Source: core.SourceSlots}, asks+" "+checked.Reason))
					keep(stepRow)
					continue
				case checked.ID != "":
					recorded.notice("stop_check let the lead's stop through with board work open: " + checked.Reason)
				}
				if quiet := work.quietBecause(messages, saidByThePerson(taskOrigin), config.Task); quiet != "" {
					recorded.notice("the board nudge stayed quiet: " + quiet)
				} else {
					stepRow.Warnings = append(stepRow.Warnings, "the lead ended with board work open, so it was asked to take it")
					messages = append(messages, inserted(core.Origin{Source: core.SourceSlots}, asks))
					keep(stepRow)
					continue
				}
			}
			keep(stepRow)
			answered(decision.Content)
			if arm != nil && config.TaskOrigin.Source != core.SourceSlots {
				arm(lead.leadCheckIn(work, held))
			}
			return finish(OutcomeStopped), nil

		case core.OutcomeTruncated:
			if decision.Content != "" || len(decision.ToolCalls) > 0 {
				messages = append(messages, core.Message{Role: core.RoleAssistant, Content: decision.Content,
					ToolCalls: decision.ToolCalls, Thinking: decision.Thinking})
			}
			if len(decision.ToolCalls) == 0 {
				keep(stepRow)
				return finish(OutcomeTruncated), nil
			}
			refuse(&stepRow, decision.ToolCalls, theResponseHitTheOutputTokenLimit)
			keep(stepRow)

		case core.OutcomeToolCalls:
			messages = append(messages, core.Message{Role: core.RoleAssistant, Content: decision.Content, ToolCalls: decision.ToolCalls, Thinking: decision.Thinking})
			flush()
			if concluding != "" {
				refuse(&stepRow, decision.ToolCalls, theTurnEndsOnACleanReport)
				keep(stepRow)
				return endAt(OutcomeStopped, theTurnEndsOnACleanReport, messages), nil
			}
			if asked != nil && !asked.toolsHeld {
				asked.toolsHeld = true
				refuse(&stepRow, decision.ToolCalls, asked.refusal())
				holdFor(&stepRow, "the lead called a tool before answering the person")
				keep(stepRow)
				continue
			}
			if turnFiles != nil {
				_ = turnFiles.Begun()
			}
			pending, batches := decision.ToolCalls, 0
			forgetting, noting := forget.Hold(messages, artifacts.preview), note.Hold(messages)
			codes := map[string]Registry{}
			toolsFor := func(gated gatedCall) Registry {
				if code, asked := codes[gated.id]; asked {
					return code
				}
				return stepTools
			}
			var tripped bool
			var repeated ToolCallRow
			var repeats int
			var leadStop error
			author := row.author()
			for len(pending) > 0 && !tripped && leadStop == nil {
				width := max(stepTools.parallelPrefix(pending), 1)
				heard := make([]heardCall, width)
				ordered, released := callOrders(ctx, width)
				inParallel(crashes, "parallel gate", width, func(i int) {
					defer released[i]()
					asked := pending[i]
					call := asked
					proxied, proxyRow := config.Proxy.rewrite(ctx, asked)
					call.Arguments = proxied
					pre := hear(ctx, hook.Input{Event: hook.PreToolUse, Tool: call.Name, Args: call.Arguments, CallID: call.ID})
					if pre.Args != nil {
						call.Arguments = pre.Args
					}
					request := GateRequest{TurnID: row.ID, Task: config.Task, Tool: call.Name, Args: call.Arguments, Call: call.ID, Reason: decision.Content}
					if pre.Args == nil {
						request.Asked = asked.Arguments
					}
					code, asksCode := codeAsked(stepTools, call)
					heard[i] = heardCall{call: call, proxy: proxyRow, pre: pre, request: request, code: code, asksCode: asksCode && config.SpawnedFrom != ""}
					if !heard[i].asksCode {
						heard[i].boundary = boundaryRefusal(stepTools, call)
					}
					reachesGate := pre.Block == "" && pre.Ask == "" && !heard[i].asksCode && heard[i].boundary == ""
					if reachesGate && config.Gate != nil && !gateExempt(call.Name) && config.GateMode == GateEnforce {
						heard[i].verdict, heard[i].gateErr = decide(ordered[i], config.Gate, request)
						heard[i].judged = true
					}
				})
				wave := make([]gatedCall, 0, width)
				for i, asked := range pending[:width] {
					call, pre, request := heard[i].call, heard[i].pre, heard[i].request
					keepHooks(call.ID, pre)
					asking, waited := withPersonWait(ctx)
					asking, held := outside.Open(asking, time.Now)
					gated := gatedCall{call: call, asked: asked, proxy: heard[i].proxy, id: session.EventIDFor(origin, call.ID), parent: stepRow.id, author: author, sift: sifter, redact: redactor, task: config.Task, site: recorded.site(call.ID, messages), model: model, fire: fire, hooks: hookRunOf(hook.PreToolUse, pre), before: pre.Context, waited: waited, held: held}
					code, asksCode := heard[i].code, heard[i].asksCode
					gated.refusal = heard[i].boundary
					if gated.refusal == "" {
						gated.refusal = hookRefusal(asking, config, request, pre)
					}
					if asksCode && gated.refusal == "" {
						request.Code = &code
						if gated.refusal = codeRefusal(asking, config.Person, request); gated.refusal == "" {
							codes[gated.id] = NewRegistry(inlineCodeTool{request: request, log: config.Places.Log})
						}
					}
					if gated.refusal != "" || asksCode {
						wave = append(wave, gated)
						continue
					}
					switch {
					case config.Gate == nil || gateExempt(call.Name):
					case config.GateMode == GateShadow:
						judged := make(chan shadowVerdict, 1)
						gated.shadow = judged
						shadowed.Add(1)
						safe.Go(crashes, "shadow gate", func() {
							defer shadowed.Done()
							var verdict GateDecision
							err := safe.Call(crashes, "shadow gate", func() (err error) {
								verdict, err = config.Gate.Decide(context.WithoutCancel(ctx), request)
								return err
							})
							shadows.Lock()
							defer shadows.Unlock()
							if verdict.ID != "" {
								shadowIDs = append(shadowIDs, verdict.ID)
							}
							shadow := shadowVerdict{decision: verdict}
							if err != nil {
								shadow.err = err.Error()
								shadowErrs = append(shadowErrs, "the shadow gate could not judge "+request.Tool+": "+shadow.err)
							}
							judged <- shadow
						})
					default:
						if !heard[i].judged {
							heard[i].verdict, heard[i].gateErr = decide(ctx, config.Gate, request)
						}
						gated.verdict, gated.gateErr = heard[i].verdict, heard[i].gateErr
						if gated.verdict.ID != "" {
							row.DecisionIDs = append(row.DecisionIDs, gated.verdict.ID)
						}
					}
					if config.GateMode == GateEnforce {
						decided, hooked := gated.verdict, ""
						if gated.gateErr == "" && decided.Verdict != ledger.VerdictUnset && !decided.PersonOnly {
							judged := fire(ctx, hook.Input{Event: hook.GateVerdict, Tool: call.Name, Args: call.Arguments, CallID: call.ID,
								Gate: &hook.GateFacts{Verdict: decided.Verdict, Risk: decided.Reason, Questions: append([]ledger.Answer{}, decided.Answers...)}})
							gated.hooks = append(gated.hooks, hookRunOf(hook.GateVerdict, judged)...)
							if judged.Gate != ledger.VerdictUnset && judged.Gate != decided.Verdict {
								decided.Verdict, decided.PersonOnly = judged.Gate, true
								hooked = "this call did not run: a GateVerdict hook turned the gate's " + string(gated.verdict.Verdict) + " into " + string(judged.Gate) + ": " + judged.GateWhy + ". "
							}
						}
						if gated.refusal = gateRefusal(asking, config.gatePerson(asking), request, decided, gated.gateErr); gated.refusal != "" {
							gated.refusal = hooked + gated.refusal
						}
					}
					wave = append(wave, gated)
				}

				rows := make([]ToolCallRow, len(wave))
				answers := make([]core.Message, len(wave))
				memoHits := make([]bool, len(wave))
				waveCtx, waveEnded := config.Inbox.stoppableWave(contract.WithStep(contract.WithReadWindow(note.With(forget.With(plan.WithEarlierCalls(ctx, messages), forgetting), noting), shownReads), step))
				batch := 0
				if len(wave) > 1 {
					batches++
					batch = batches
				}
				inParallel(crashes, "parallel tool call", len(wave), func(i int) {
					gated := wave[i]
					if gated.call.Name == (askTool{}).Name() {
						defer waits.begin()()
					}
					rows[i], answers[i], memoHits[i] = gated.run(withShellCall(waveCtx, gated.id), toolsFor(gated), config.ResultBytesCap, artifacts, batch)
				})
				leadStop = waveEnded()
				shown := make([]core.Message, 0, len(answers))
				for i := range answers {
					pointer := false
					if memoHits[i] {
						answers[i].Content, pointer = pointAtCopy(answers[i], messages, shown)
						answers[i].Images, rows[i].RenderedBytes = nil, len(answers[i].Content)
					}
					sent := answers[i]
					if !pointer {
						sent.Content = withToolRef(rows[i].ID, sent.Content)
					}
					shown = append(shown, sent)
				}
				stepRow.ToolCalls = append(stepRow.ToolCalls, rows...)
				for i, gated := range wave {
					results[gated.call.ID] = rows[i]
					if label := artifacts.keep.refetched(rows[i]); label != "" {
						recorded.notice(label)
					}
				}
				messages = append(messages, shown...)
				flush()
				if config.ToolResult != nil {
					for _, answered := range answers {
						config.ToolResult(answered)
					}
				}
				pending = pending[len(wave):]
				for _, called := range rows {
					if called.Proxy != nil && called.Proxy.Note != "" && !slices.Contains(row.Warnings, called.Proxy.Note) {
						row.Warnings = append(row.Warnings, called.Proxy.Note)
					}
					if hit, seen := guard.observe(called); hit {
						tripped, repeated, repeats = true, called, seen
						break
					}
				}
			}
			if after := forgetting.Applied(messages); after != nil {
				recorded.listChange("forget", "the model dropped tool results after the last cache breakpoint on step "+strconv.Itoa(step), messages, after)
				messages = after
			}
			if notes, changed := noting.Changed(); changed {
				messages = append(messages, inserted(note.Origin(), notes))
			}
			if config.Boundary != nil {
				asked := config.Boundary.Asked()
				stepRow.Grants = slices.DeleteFunc(slices.Clone(asked), func(question subagents.Question) bool { return slices.Contains(recordedGrants, question) })
				recordedGrants = asked
			}
			if leadStop != nil {
				refuse(&stepRow, pending, thePersonStoppedTheLead)
			}
			if err := cmp.Or(ctx.Err(), leadStop); err != nil {
				keep(stepRow)
				return fail(err)
			}
			if tripped {
				cause := loopGuardCause(repeated, repeats, guard.window)
				stopped := "this turn stopped itself because " + cause
				row.Guard = &LoopGuardStop{Tool: repeated.Tool, Args: repeated.Args, Repeats: repeats}
				row.Warnings = append(row.Warnings, "the turn stopped itself: "+cause)
				refuse(&stepRow, pending, stopped)
				keep(stepRow)
				return endAt(OutcomeLoopGuard, stopped, messages), nil
			}
			if config.TaskOrigin.Source == core.SourceAsk && answeredAsksOnly(stepRow.ToolCalls) {
				keep(stepRow)
				answered(decision.Content)
				return finish(OutcomeStopped), nil
			}
			if config.SpawnedFrom == "" && onlyStartedSpawns(stepTools, stepRow.ToolCalls) {
				if asked == nil || asked.endHeld {
					keep(stepRow)
					answered(decision.Content)
					return finish(OutcomeStopped), nil
				}
				asked.endHeld = true
				holdFor(&stepRow, "the lead tried to end the turn on a spawn before answering the person")
			}
			var moved *Account
			forced := ForkKind("")
			if config.Accounts.Next != nil && !config.NoFork {
				next, spent, nextErr := config.Accounts.Next(ctx, account)
				var everySpent *EveryAccountSpent
				switch {
				case errors.As(nextErr, &everySpent):
					keep(stepRow)
					return fail(nextErr)
				case nextErr != nil:
					row.Warnings = append(row.Warnings,
						"the pinned account's windows could not be read, so this session stays on it: "+nextErr.Error())
				case spent:
					moved, forced = &next, ForkAccountSpent
				}
			}
			if forced == "" {
				trimmedAfter, err := trim(step, "after step "+strconv.Itoa(step))
				stepRow.Compaction = cmp.Or(trimmedAfter, stepRow.Compaction)
				if err != nil {
					keep(stepRow)
					return fail(err)
				}
			}
			if !config.NoFork {
				beforeFork := slices.Clone(messages)
				fork, begun, err := forkFrom(forced)
				if err == nil {
					err = ctx.Err()
				}
				if err != nil {
					keep(stepRow)
					return fail(err)
				}
				if why := lastForkWhy(fork, step); why != "" {
					fork.Step, stepRow.Fork = step, fork
					keep(stepRow)
					return endForked(fork, "after step "+strconv.Itoa(step), beforeFork, begun, moved, why), nil
				}
				if fork != nil {
					forks++
					fork.Step, fork.Into = step, origin+"-f"+strconv.Itoa(forks+1)
					begun, missing := stateCarried(fork, step, beforeFork, begun, definitions, moved)
					stepRow.Fork = fork
					keep(stepRow)
					forkInto(fork, "after step "+strconv.Itoa(step), beforeFork, begun, moved)
					if missing != "" {
						row.Warnings = append(row.Warnings, missing)
					}
					continue
				}
			} else {
				sayForkOff("after step " + strconv.Itoa(step))
			}
			unforked, err := trimUnderTarget(step, "after step "+strconv.Itoa(step))
			stepRow.Compaction = cmp.Or(unforked, stepRow.Compaction)
			keep(stepRow)
			if err != nil {
				return fail(err)
			}

		case core.OutcomeRefusal:
			keep(stepRow)
			return fail(transport.Fail("turn.Run", transport.KindProvider, nil, "the model refused: %s", decision.Refusal))

		default:
			panic("turn: unknown model outcome")
		}
	}
}

func registrySnapshot(registry *process.Registry) map[string]bool {
	if registry == nil {
		return nil
	}
	list, err := registry.List()
	if err != nil {
		return nil
	}
	names := make(map[string]bool, len(list))
	for _, entry := range list {
		names[entry.Name] = true
	}
	return names
}

func backgroundSurvivors(registry *process.Registry, before map[string]bool) string {
	if registry == nil {
		return ""
	}
	list, err := registry.List()
	if err != nil {
		return ""
	}
	var named []string
	for _, entry := range list {
		if before[entry.Name] || entry.State != process.Running {
			continue
		}
		named = append(named, entry.Name+" ("+entry.Command+")")
	}
	if len(named) == 0 {
		return ""
	}
	return "this turn started a background process still running now that it has ended: " + strings.Join(named, ", ") +
		"; see it in the shells tab, or stop it with the " + ShellToolName + " tool's stop, naming it"
}

func overflowed(err error) bool {
	var refused *recall.OverWindow
	return transport.ContextOverflow(err) || errors.As(err, &refused)
}

func (r *record) wall(op walls.Op, name string, started time.Time, clock *outside.Clock, err error) {
	wall := walls.Wall{Op: op, Name: name, MS: time.Since(started).Milliseconds(), At: started.UnixMilli(), OK: err == nil}
	if clock != nil {
		outsideMS := clock.Outside().Milliseconds()
		wall.OutsideMS = &outsideMS
	}
	r.add(session.Event{Kind: session.EventWall}, wall)
}

func (r *record) heldAgain() (*record, bool) {
	if r == nil {
		return nil, true
	}
	if !r.own || r.inbox == nil {
		return nil, false
	}
	log, err := r.inbox.open(r.store, session.Header{ID: r.session()})
	if err != nil {
		return nil, false
	}
	return &record{store: r.store, inbox: r.inbox, log: log, scope: r.scope, turn: r.turn, said: map[string]string{}}, true
}

func NewID(at time.Time) string {
	return session.IDPrefix + strconv.FormatInt(at.UnixNano(), 16)
}

func stepFrom(id string, index int, timing requestTiming, decision core.Decision) StepRow {
	return StepRow{
		id:               id,
		attempt:          timing.attempt,
		DurationMS:       timing.durationMS,
		FirstTokenMS:     decision.FirstTokenMS,
		LastByteMS:       decision.LastByteMS,
		Index:            index,
		AssistantText:    decision.Content,
		Model:            decision.Build,
		StopReason:       decision.Stop,
		PromptTokens:     decision.Usage.InputTokens,
		CompletionTokens: decision.Usage.OutputTokens,
		ReasoningTokens:  decision.Usage.ReasoningTokens,
		CacheReadTokens:  decision.CacheReadTokens,
		CacheWriteTokens: decision.CacheWriteTokens,
		CacheWrite5m:     decision.CacheWrite5m,
		CacheWrite1h:     decision.CacheWrite1h,
		CostUSD:          decision.Usage.Cost,
		Warnings:         decision.Warnings,
	}
}

type loadedReferences struct {
	mutex sync.Mutex
	held  map[string]string
}

func (*loadedReferences) Name() string { return ReferenceToolName }

func (*loadedReferences) Definition() core.Tool { return referenceTool{}.Definition() }

func (r *loadedReferences) Run(ctx context.Context, raw json.RawMessage) (contract.Result, error) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	return referenceTool{held: r.held}.Run(ctx, raw)
}

type runningTools struct {
	fetch      FetchTool
	history    historyTool
	references *loadedReferences
}

func (c Config) OfferedTools() []core.Tool {
	return NewRegistry(c.answerable(c.withLoopTools(c.Tools.tools, runningTools{references: &loadedReferences{}}))...).Definitions()
}

func (c Config) withLoopTools(tools []contract.Tool, running runningTools) []contract.Tool {
	added := slices.Clone(tools)
	holdsSkill, estimating := false, -1
	var spawner *SpawnTool
	for at, tool := range tools {
		if spawning, spawns := tool.(*SpawnTool); spawns {
			spawner = spawning
			added = append(added, messageTool{orchestrator: spawner}, subAgentsTool{orchestrator: spawner})
		}
		switch tool.Name() {
		case SkillToolName:
			added[at], holdsSkill = skillLoading{Tool: tool, references: running.references}, true
		case EstimateTimeToolName:
			estimating = at
		}
	}
	if estimating >= 0 {
		added[estimating] = EstimateTime{base: c, spawner: spawner}
	}
	if !c.TruncateResults {
		added = append(added, running.fetch)
	}
	added = withHistory(added, running.history)
	if c.referencesOffered() || holdsSkill {
		added = append(added, running.references)
	}
	return added
}

func (c Config) answerable(tools []contract.Tool) []contract.Tool {
	tools = slices.DeleteFunc(tools, func(tool contract.Tool) bool { return c.unoffered(tool.Name()) })
	for at, tool := range tools {
		if !c.personAnswers() && tool.Name() == AskPersonToolName {
			tools[at] = ListedOnly{Tool: tool, Why: nobodyAnswersThisTurn}
		}
	}
	return tools
}

func (c Config) referencesOffered() bool {
	return slices.ContainsFunc(c.Parts, func(part PromptPart) bool {
		return part.Concern == ConcernReferences && part.State == session.PartOffered
	})
}

func (c Config) personAnswers() bool {
	return c.Person != nil && c.SpawnedFrom == "" && c.TaskOrigin.Source != core.SourceCronFire
}

func (c Config) unoffered(name string) bool {
	return c.SpawnedFrom != "" && name == AskPersonToolName
}

type skillLoading struct {
	contract.Tool
	references *loadedReferences
}

func (s skillLoading) Run(ctx context.Context, raw json.RawMessage) (contract.Result, error) {
	result, err := s.Tool.Run(ctx, raw)
	if err != nil || result.Origin.Source != core.SourceSkillLoad || result.Origin.ID != "" {
		return result, err
	}
	skills, err := skill.Shipped(shipped.Files(), "")
	at := slices.IndexFunc(skills, func(one skill.Skill) bool { return one.Name == result.Origin.Name })
	if err != nil || at < 0 {
		return result, err
	}
	files, err := skill.ReferenceFiles(shipped.Files(), skills[at].Name, skills[at].References)
	if err != nil {
		return contract.Result{}, err
	}
	read, err := libraryReferences(shipped.Files(), files)
	s.references.mutex.Lock()
	defer s.references.mutex.Unlock()
	for _, reference := range read {
		s.references.held[reference.Name] = reference.Text
	}
	return result, err
}

func unendedSubAgents(tools []contract.Tool) []string {
	var unended []string
	for _, tool := range tools {
		spawner, spawns := tool.(*SpawnTool)
		if !spawns {
			continue
		}
		for _, agent := range spawner.roster.SubAgents() {
			switch agent.State {
			case subagents.Errored, subagents.Finished:
				continue
			case subagents.Working, subagents.WaitingAnswer, subagents.InReview, subagents.Reopened, subagents.Parked:
			}
			held := "holds no path"
			if agent.Holds() && len(agent.Owns) > 0 {
				held = "holds " + strings.Join(agent.Owns, " ")
			}
			mission, _, _ := strings.Cut(agent.Mission, "\n")
			unended = append(unended, agent.ID+": "+cmp.Or(agent.Agent, "no definition")+", "+agent.State.String()+", "+held+
				", ticket "+cmp.Or(agent.Ticket, "none")+": "+runeSafeHead(mission, konst.CarrySignpostBytes))
		}
	}
	return unended
}

func sessionStartMessages(source hook.SessionSource, added []hook.Context, at time.Time) []core.Message {
	messages := hookMessages(core.SourceSessionStart, added, at)
	for i := range messages {
		messages[i].Origin.ID = string(source)
	}
	return messages
}

func withoutOlderCompactContext(messages, added []core.Message) []core.Message {
	compact := func(message core.Message) bool {
		return message.Origin.Source == core.SourceSessionStart && message.Origin.ID == string(hook.SessionCompact)
	}
	if !slices.ContainsFunc(added, compact) {
		return messages
	}
	writtenBeforeTheID := func(message core.Message) bool {
		return message.Origin.Source == core.SourceSessionStart && message.Origin.ID == "" && slices.ContainsFunc(added, func(newer core.Message) bool {
			return newer.Origin.Name == message.Origin.Name && newer.Content == message.Content
		})
	}
	return slices.DeleteFunc(slices.Clone(messages), func(message core.Message) bool { return compact(message) || writtenBeforeTheID(message) })
}

type EveryAccountSpent struct {
	Source  string
	Spent   []int64
	ResetAt time.Time
}

func (e *EveryAccountSpent) Error() string {
	named := make([]string, len(e.Spent))
	for i, id := range e.Spent {
		named[i] = "#" + strconv.FormatInt(id, 10)
	}
	said := "every " + e.Source + " account is spent (" + strings.Join(named, ", ") + "), so this turn stops here rather than move onto a spent one"
	if e.ResetAt.IsZero() {
		return said + "; no reading says when one resets"
	}
	return said + "; the first resets at " + e.ResetAt.Format(time.RFC3339)
}

func stepHold(preview recall.Config, bands recall.Bands, stepTokens int) int {
	return min(forkStateTokens(preview)+stepTokens, bands.Target()*konst.ForkRoomMostPercent/100)
}

func forkStateTokens(preview recall.Config) int {
	return konst.ForkStateBytes * 1000 / preview.BytesPerThousandTokens
}

type heardCall struct {
	call     core.ToolCall
	proxy    *ProxyRow
	pre      hook.Verdict
	request  GateRequest
	code     subagents.InlineCode
	asksCode bool
	boundary string
	verdict  GateDecision
	gateErr  string
	judged   bool
}

func decide(ctx context.Context, gate Gate, request GateRequest) (GateDecision, string) {
	verdict, err := gate.Decide(ctx, request)
	if err != nil {
		return verdict, err.Error()
	}
	return verdict, ""
}

func inParallel(crashes safe.Report, unit string, n int, each func(int)) {
	if n == 1 {
		each(0)
		return
	}
	var running sync.WaitGroup
	crashed := make([]error, n)
	running.Add(n)
	for i := range n {
		safe.Go(crashes, unit, func() {
			defer running.Done()
			crashed[i] = safe.Call(nil, unit, func() error { each(i); return nil })
		})
	}
	running.Wait()
	if first := slices.IndexFunc(crashed, func(err error) bool { return err != nil }); first >= 0 {
		panic(crashed[first])
	}
}

type callOrderKey struct{}

type callOrder struct {
	after <-chan struct{}
	done  func()
}

func callOrders(ctx context.Context, n int) ([]context.Context, []func()) {
	contexts, released := make([]context.Context, n), make([]func(), n)
	after := make(chan struct{})
	close(after)
	for i := range n {
		previous, done := after, make(chan struct{})
		released[i] = sync.OnceFunc(func() { <-previous; close(done) })
		contexts[i] = context.WithValue(ctx, callOrderKey{}, callOrder{after: previous, done: released[i]})
		after = done
	}
	return contexts, released
}

func InCallOrder(ctx context.Context, write func()) {
	order, ordered := ctx.Value(callOrderKey{}).(callOrder)
	if !ordered {
		write()
		return
	}
	<-order.after
	defer order.done()
	write()
}
