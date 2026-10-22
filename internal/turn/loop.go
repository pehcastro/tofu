package turn

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"runtime/debug"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"tofu/internal/classifier"
	"tofu/internal/hook"
	"tofu/internal/judge/jev"
	"tofu/internal/judge/ledger"
	"tofu/internal/judge/question"
	"tofu/internal/konst"
	"tofu/internal/llm"
	"tofu/internal/recall"
	"tofu/internal/safe"
	"tofu/internal/session"
	"tofu/internal/shell"
	"tofu/internal/subagent"
	"tofu/internal/sys"
	"tofu/internal/transport"
	"tofu/library/questions"
)

const theResponseHitTheOutputTokenLimit = "the response hit the output token limit, so its arguments may be truncated. " +
	"Re-issue the tool call with complete arguments."

const theTurnEndsOnACleanReport = "this turn ends on a clean sub-agent report, so it runs no tool"

const andThisIsItsLastStep = ", and this is its last step: answer now from what you already have, " +
	"saying what you did, what is left undone, and what to do next. no tool runs on this step."

const theLastStepRunsNoTool = "this was the turn's last step, which runs no tool"

const aMessageArrivedMidTurn = "the person wrote to you while you were working. answer them first, in the reply you write now, before any tool call, board work or spawn: " +
	"answer from what you know now, never wait on your own running work or a sub-agent, and never promise the answer for later. " +
	"research it after that answer, in this turn. take in what they told you in their own terms. " +
	"never call their message irrelevant or say it changes nothing."

const theAnswerComesFirst = "the person's message is answered before any tool call"

type openQuestion struct {
	said      string
	step      int
	toolsHeld bool
	endHeld   bool
}

func (q openQuestion) sendBack() string {
	return "the person asked \"" + q.said + "\" at step " + strconv.Itoa(q.step) + " and has no answer yet: answer it now, from what you know now, " +
		"before any tool call, board work or spawn. never wait on your own running work or a sub-agent; research goes after the answer, in this turn."
}

const (
	steerAnsweredRef     = "steer_answered@1"
	steerAnsweredName    = "answered"
	steerAnsweredBuilder = "steer_answered_state@1"
)

type steerJudge struct {
	client    *jev.Client
	set       question.Set
	asked     jev.Question
	threshold float64
	ledger    *ledger.Writer
}

func newSteerJudge(config Config) (*steerJudge, error) {
	places, err := config.ResolvedPlaces()
	if err != nil {
		return nil, err
	}
	layers, err := question.Layers(questions.Files(), config.Project)
	if err != nil {
		return nil, err
	}
	set, fields, err := question.Resolve(steerAnsweredRef, layers)
	if err != nil {
		return nil, err
	}
	asked, found := set.Question(steerAnsweredName)
	at := slices.IndexFunc(fields, func(f question.Field) bool { return f.Path == "thresholds."+steerAnsweredName+"_at" })
	if !found || at < 0 {
		return nil, fmt.Errorf("%s names no %s question or no thresholds.%s_at", steerAnsweredRef, steerAnsweredName, steerAnsweredName)
	}
	threshold, err := strconv.ParseFloat(fields[at].Value, 64)
	if err != nil {
		return nil, err
	}
	client, err := classifier.Open(config.Project, jev.ReadKeyring(), 1)
	if err != nil {
		return nil, err
	}
	return &steerJudge{client: client, set: set, asked: asked.ToJev(), threshold: threshold, ledger: ledger.NewWriter(places.Log)}, nil
}

func (j *steerJudge) answers(ctx context.Context, said, reply string) (bool, error) {
	body, err := json.Marshal(struct {
		Said  string `json:"said"`
		Reply string `json:"reply"`
	}{said, reply})
	if err != nil {
		return false, err
	}
	asked := []jev.Question{j.asked}
	row := ledger.Row{Point: j.set.Name, Questions: j.set.Name, Version: j.set.QuestionsVersion, StateHash: ledger.HashOf(body), StateBuilder: steerAnsweredBuilder, State: body,
		Verdict: ledger.VerdictAsk, Reason: &ledger.Reason{Question: steerAnsweredName, Comparison: ">= " + strconv.FormatFloat(j.threshold, 'f', -1, 64), Threshold: j.threshold, Mode: ledger.ModeEnforced}}
	decision, err := j.client.Ask(ctx, jev.Request{State: json.RawMessage(body), Questions: asked})
	if err != nil {
		failed := "jev could not answer, so the person's question stays open: " + err.Error()
		row.Reason.ModeReason = &failed
		_, unwritten := j.ledger.Append(row)
		return false, errors.Join(err, unwritten)
	}
	decision.Record(&row, asked, j.set.QuestionsVersion)
	row.Reason.Value = row.Answers[0].Noul
	if row.Reason.Value >= j.threshold {
		row.Verdict = ledger.VerdictAllow
	}
	_, err = j.ledger.Append(row)
	return row.Verdict == ledger.VerdictAllow, err
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
	handles := !config.TruncateResults
	artifacts, err := NewArtifacts(dir, handles)
	if err != nil {
		return Row{}, err
	}
	artifacts.preview = artifacts.preview.OnWire(config.Wire)
	sifter := siftOrNothing(config.Sift)
	thrifter := thriftOrNothing(config.Thrift)
	if config.Redactor == nil {
		loaded := sys.LoadKeyRedactor()
		config.Redactor = &loaded
	}
	redactor := *config.Redactor
	source := config.ToolSource
	if source == nil {
		source = func() Registry { return config.Tools }
	}
	var recorded *record
	personAnswers := config.Person != nil && config.SpawnedFrom == "" && config.TaskOrigin.Source != llm.SourceCronFire
	unoffered := func(name string) bool { return !personAnswers && name == AskPersonToolName }
	withLoopTools := func(tools []Tool) []Tool {
		added := slices.Clone(tools)
		for _, tool := range tools {
			if spawner, spawning := tool.(*SpawnTool); spawning {
				added = append(added, messageTool{orchestrator: spawner}, subAgentsTool{orchestrator: spawner})
			}
		}
		if handles {
			added = append(added, artifacts.FetchTool())
		}
		if config.Sessions != nil {
			added = append(added, lookupTool{sessions: config.Sessions, current: func() string { return recorded.session() }})
		}
		if config.SpawnedFrom != "" && strings.Contains(config.System, referencesOnDemand) {
			added = append(added, referenceTool{held: config.References})
		}
		return added
	}
	answerable := func(tools []Tool) []Tool {
		return slices.DeleteFunc(tools, func(tool Tool) bool { return unoffered(tool.Name()) })
	}
	currentTools := func() Registry {
		return NewRegistry(answerable(withLoopTools(source().tools))...)
	}
	sending := func(definitions []llm.Tool) []llm.Tool {
		return slices.DeleteFunc(slices.Clone(config.Prefix.toolsOr(definitions)), func(tool llm.Tool) bool { return unoffered(tool.Name) })
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
	sentTools := answerable(withLoopTools(config.Tools.tools))
	usedTools := make([]string, len(sentTools))
	for i, tool := range sentTools {
		usedTools[i] = tool.Name()
	}
	sentSchemas, err := json.Marshal(NewRegistry(sentTools...).Definitions())
	if err != nil {
		return Row{}, err
	}
	row := Row{ID: origin, Schema: SchemaVersion, At: start, Task: config.Task, Wire: config.Wire, Spend: config.Spend, Root: origin, Account: account.ID, SpawnedFrom: config.SpawnedFrom, SpawnedBy: config.SpawnedBy, Budget: budget, System: config.System, Tools: usedTools, ToolSchemas: string(sentSchemas)}

	upgraded := config.upgradeNote()
	recorded, err = openRecord(config, row)
	if err != nil {
		return Row{}, err
	}
	row.Session = recorded.session()
	taskOrigin := config.taskOrigin(start)
	recorded.begin(row, taskOrigin)
	if len(config.History) > 0 {
		recorded.listChange("carried", "the turn starts on the conversation the last turn or the resume left", nil, config.History)
	}

	hooks, untrustedHooks := hookEngine(ctx, config, origin)
	ctx = context.WithValue(ctx, hookEngineKey{}, hooks)
	row.Warnings = append(row.Warnings, untrustedHooks...)
	var hookWarnings sync.Mutex
	fire := func(ctx context.Context, in hook.Input) hook.Verdict {
		in.Session, in.Turn = row.Session, row.ID
		if config.SpawnedFrom != "" {
			in.Agent, in.AgentType = origin, config.AgentType
		}
		verdict := hooks.Fire(ctx, in)
		hookWarnings.Lock()
		defer hookWarnings.Unlock()
		for _, warning := range verdict.Warnings {
			if !slices.Contains(row.Warnings, warning) {
				row.Warnings = append(row.Warnings, warning)
			}
		}
		for _, ran := range verdict.Runs {
			ran.Decision, ran.Problem, ran.Stderr = redactor.Redact(ran.Decision), redactor.Redact(ran.Problem), redactor.Redact(ran.Stderr)
			recorded.add(session.Event{Kind: session.EventHook, Call: in.CallID}, ran)
		}
		return verdict
	}
	ctx = context.WithValue(ctx, hookFireKey{}, fire)

	messages := make([]llm.Message, 0, len(config.History)+2)
	headTools := NewRegistry(withLoopTools(source().tools)...).Definitions()
	if system := config.Prefix.hold(config.SystemMessage(), headTools); system != "" {
		messages = append(messages, llm.Message{Role: llm.RoleSystem, Content: system})
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
		return func(ctx context.Context, why, wire string, model Model, request llm.Request) (llm.Decision, error) {
			if err := compacting.Err(); err != nil {
				return llm.Decision{}, err
			}
			id, started := session.NewEventID(), time.Now()
			tapped, tap := llm.Tapped(ctx)
			decision, timing, err := askCountingAttempts(tapped, model, request)
			recording.Lock()
			defer recording.Unlock()
			into.exchange(id, why, wire, request, started, tap.Attempts(), decision, err)
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
	episodesCarried := konst.MemtreeViewBytes
	episodesFor := func(sized recall.Budget, head []llm.Message, saidElsewhere []string) string {
		config.Inbox.compacted()
		room := (sized.Bands.Target() - sized.Tokens(artifacts.preview, historyOf(head))) * artifacts.preview.BytesPerThousandTokens / 1000
		allowed := min(room*konst.EpisodeViewPercentOfRoom/100, episodesCarried)
		if allowed < konst.MemtreeLineBytes {
			episodesCarried = 0
			return ""
		}
		view, err := keeper.Episodes(allowed, saidElsewhere)
		if err != nil {
			row.Warnings = append(row.Warnings, "the episode view is not carried: "+err.Error())
		}
		episodesCarried = len(view)
		return view
	}
	carried, view := slices.DeleteFunc(slices.Clone(config.History), func(message llm.Message) bool { return message.Origin.Source == llm.SourceSpawnLine }), []llm.Message(nil)
	if config.opensGeneration() {
		carried, view = withoutMemoryViews(carried), config.MemoryMessage()
	}
	if schemas, err := json.Marshal(headTools); err == nil && keeper != nil && len(config.History) == 0 {
		view = withEpisodes(view, episodesFor(budget.Sending(artifacts.preview, string(schemas)), slices.Concat(messages, view), nil))
	}
	messages = slices.Concat(messages, carried, view)
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
	var hooked []llm.Message
	if config.SpawnedFrom == "" {
		if config.SessionSource != "" {
			hooked = hookMessages(llm.SourceSessionStart, fire(ctx, hook.Input{Event: hook.SessionStart, Source: config.SessionSource}).Context, start)
		}
		prompted := fire(ctx, hook.Input{Event: hook.UserPromptSubmit, Prompt: config.Task})
		promptRefused, hooked = prompted.Block, append(hooked, hookMessages(llm.SourcePromptSubmit, prompted.Context, start)...)
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
	refuse := func(step *StepRow, calls []llm.ToolCall, why string) {
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
	sentTokens := func(messages []llm.Message) int {
		return budget.Tokens(artifacts.preview, historyOf(messages))
	}
	var lastAnswered time.Time
	var lastAttempts []llm.Attempt
	ask := func(ctx context.Context, why string, request llm.Request) (llm.Decision, requestTiming, string, error) {
		id, started := session.NewEventID(), time.Now()
		request.Turn = row.ID
		if tokens := sentTokens(request.Messages); tokens > budget.Ceiling() {
			err := fmt.Errorf("this request is about %d tokens, past the %d token ceiling after the trim and the fork, so it was not sent", tokens, budget.Ceiling())
			recorded.exchange(id, why, config.Wire, request, started, nil, llm.Decision{}, err)
			return llm.Decision{}, requestTiming{}, id, err
		}
		tapped, tap := llm.Tapped(ctx)
		decision, timing, err := askCountingAttempts(tapped, model, request)
		lastAttempts = tap.Attempts()
		recorded.exchange(id, why, config.Wire, request, started, lastAttempts, decision, err)
		if err == nil {
			lastAnswered = now()
		}
		return decision, timing, id, err
	}
	inserted := func(origin llm.Origin, text string) llm.Message {
		origin.TakenAt = now()
		return llm.Message{Role: llm.RoleUser, Content: text, Origin: origin}
	}
	withPlan := func(messages []llm.Message) []llm.Message {
		if config.Plan == nil {
			return messages
		}
		shown := config.Plan()
		if shown == "" {
			return messages
		}
		return append(slices.Clip(messages), inserted(llm.Origin{Source: llm.SourcePlan}, thePlanAsItStands+shown))
	}
	compacted := func() {
		if config.SpawnedFrom != "" {
			return
		}
		messages = append(messages, hookMessages(llm.SourceSessionStart, fire(ctx, hook.Input{Event: hook.SessionStart, Source: sessionSourceCompact}).Context, now())...)
	}
	trims := !config.NoCompaction && budget.Automatic
	trimFrom := func(step int, when string, from int) (*Compaction, error) {
		before := slices.Clone(messages)
		compaction, shrink, err := trimRead(artifacts, budget, step, messages, from)
		if compaction != nil {
			recorded.listChange("compaction", when+": the conversation crossed its budget, so tofu "+shrink.String(), before, messages)
			compacted()
		}
		return compaction, err
	}
	landsFrom := func(step, from, tokens int) bool {
		holdingNothing := Artifacts{preview: artifacts.preview, handles: artifacts.handles}
		reached, _, err := trimRead(holdingNothing, budget, step, slices.Clone(messages), from)
		return err == nil && reached != nil && reached.TokensAfter <= tokens
	}
	trimUnderTarget := func(step int, when string) (*Compaction, error) {
		if !trims || sentTokens(messages) <= budget.Bands.Target() {
			return nil, nil
		}
		firstMissing := sort.Search(len(messages)+1, func(from int) bool { return !landsFrom(step, from, budget.Bands.Target()) })
		return trimFrom(step, when, max(firstMissing-1, 0))
	}
	trim := func(step int, when string) (*Compaction, error) {
		if !trims {
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
		return trimFrom(step, when, from)
	}
	stateCarried := func(fork *Fork, step int, beforeFork, begun []llm.Message, tools []llm.Tool) ([]llm.Message, string) {
		begun = config.withFreshMemoryView(begun)
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
			asked = begun
		}
		request := llm.Request{Messages: append(slices.Clone(asked), inserted(llm.Origin{Source: llm.SourceForkState}, forkStateAsk())), Tools: tools}
		if len(tools) > 0 {
			request.ToolChoice = llm.ToolChoiceNone
		}
		decision, timing, id, err := ask(context.WithValue(ctx, quietAskKey{}, true), "the working state the fork carries", request)
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
		case decision.Outcome != llm.OutcomeMessage || strings.TrimSpace(decision.Content) == "":
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
		row.WallClockMS = now().Sub(start).Milliseconds()
		if recorded != nil {
			for _, failed := range recorded.failed {
				row.Warnings = append(row.Warnings, "part of this turn was not recorded as it ran: "+failed)
			}
		}
		recorded.end(row)
		return row
	}
	fail := func(err error) (Row, error) {
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
	endAt := func(outcome Outcome, lead string, history []llm.Message) Row {
		messages = history
		if config.NoLastWord {
			return finish(outcome)
		}
		messages = append(slices.Clone(history), inserted(llm.Origin{Source: llm.SourceLastWord}, lead+andThisIsItsLastStep))
		request := llm.Request{Messages: messages, Tools: sending(currentTools().Definitions())}
		if len(request.Tools) > 0 {
			request.ToolChoice = llm.ToolChoiceNone
		}
		decision, timing, id, err := ask(ctx, "the last word: "+lead, request)
		row.TotalCostUSD += decision.Usage.Cost
		if err != nil {
			row.Warnings = append(row.Warnings, lead+", and the last answer was not obtained: "+err.Error())
			return finish(outcome)
		}
		row.Model = decision.Build
		last := stepFrom(id, len(row.Steps)+1, timing, decision)
		answering = last.id
		if decision.Content != "" || len(decision.ToolCalls) > 0 {
			messages = append(messages, llm.Message{Role: llm.RoleAssistant, Content: decision.Content, ToolCalls: decision.ToolCalls, Thinking: decision.Thinking})
		}
		refuse(&last, decision.ToolCalls, theLastStepRunsNoTool)
		switch {
		case decision.Outcome == llm.OutcomeTruncated:
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
	var recordedGrants []subagent.Question
	if config.Boundary != nil {
		recordedGrants = config.Boundary.Asked()
	}
	crashes := sys.Crashes(config.Project)
	forkInto := func(fork *Fork, when string, beforeFork, begun []llm.Message, moved *Account) {
		ended := row
		ended.Outcome, ended.ForkedInto, ended.EndedInFork, ended.Conversation = OutcomeForked, fork.Into, fork, messages[afterSystem:]
		ended.WallClockMS = now().Sub(start).Milliseconds()
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
		row.Session = recorded.session()
		recorded.listChange("fork", string(fork.Kind)+" fork "+strconv.Itoa(forks)+" "+when+" from "+ended.Session+", "+
			strconv.Itoa(fork.TokensBefore)+" to "+strconv.Itoa(fork.TokensAfter)+" tokens, "+strconv.Itoa(fork.TailMessages)+" tail messages", beforeFork, begun)
		messages, sent, answering = begun, 0, ""
		if fork.Kind == ForkCompact {
			compacted()
		}
		flush()
	}
	askedAgainAfterBlank, polled, nudged := false, false, false
	steeredAt := 0
	var left []string
	var asked *openQuestion
	holdFor := func(stepRow *StepRow, why string) {
		told := why + ", so it was sent back to answer the person's message from step " + strconv.Itoa(asked.step)
		stepRow.Warnings = append(stepRow.Warnings, told)
		recorded.notice(told)
		messages = append(messages, inserted(llm.Origin{Source: llm.SourceAnswerFirst}, asked.sendBack()))
	}
	var steer *steerJudge
	answeredBy := func(reply string) (bool, error) {
		if strings.TrimSpace(reply) == "" {
			return false, nil
		}
		if steer == nil {
			built, err := newSteerJudge(config)
			if err != nil {
				return false, err
			}
			steer = built
		}
		return steer.answers(ctx, asked.said, reply)
	}
	noticeStep := config.Caps.MaxSteps - max(1, int(math.Ceil(float64(config.Caps.MaxSteps)*konst.TurnStepCapNoticeShare)))
	for step := 1; ; step++ {
		if err := ctx.Err(); err != nil {
			return fail(err)
		}
		if config.Steering != nil {
			for _, steered := range config.Steering() {
				if steeredAt != step {
					messages = append(messages, inserted(llm.Origin{Source: llm.SourceAnswerFirst}, aMessageArrivedMidTurn))
					first, _, _ := strings.Cut(strings.TrimSpace(steered), "\n")
					asked = &openQuestion{said: runeSafeHead(first, konst.LeadHandbackQuoteBytes), step: step}
					recorded.notice("open question from the person at step " + strconv.Itoa(step) + ": " + asked.said)
				}
				steeredAt = step
				said := inserted(llm.Origin{Source: llm.SourceSteer}, steered)
				if config.Inbox != nil {
					said.Content += config.Inbox.repeatNote(config.Sessions, cmp.Or(row.Session, config.Session), steered)
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
				if item.origin.Source == llm.SourceMemorySaved {
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
			messages = append(messages, inserted(llm.Origin{Source: llm.SourceStepCap}, "this turn has "+strconv.Itoa(config.Caps.MaxSteps-step+1)+
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
				fork, begun, err := forkHistory(artifacts, budget, config.forkOpening(taskOrigin), messages, "", forks+1, config.Caps.MaxForks)
				if err != nil {
					return fail(err)
				}
				if fork != nil {
					forks++
					fork.Into = origin + "-f" + strconv.Itoa(forks+1)
					begun, missing := stateCarried(fork, step, beforeFork, begun, definitions)
					forkInto(fork, before, beforeFork, begun, nil)
					if missing != "" {
						row.Warnings = append(row.Warnings, missing)
					}
				}
			}
			unforked, err := trimUnderTarget(step, before)
			if err != nil {
				return fail(err)
			}
			trimmedBefore = cmp.Or(unforked, trimmedBefore)
		}
		keepNewestPictures(messages)
		asSent := recall.Measure(artifacts.preview, budget.Bands, historyOf(messages))
		decision, timing, requestID, err := ask(ctx, "step "+strconv.Itoa(step), llm.Request{Messages: withPlan(messages), Tools: definitions})
		if errors.Is(err, SentNow{}) {
			step--
			continue
		}
		if overflowed(err) {
			before := slices.Clone(messages)
			shrink, shrinkErr := shrinkOverflow(artifacts, messages, asSent.Total(), budget.WindowTokens)
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
				asSent = recall.Measure(artifacts.preview, budget.Bands, historyOf(messages))
				decision, timing, requestID, err = ask(ctx, "step "+strconv.Itoa(step)+", asked again after the overflow shrink", llm.Request{Messages: withPlan(messages), Tools: definitions})
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
					budget = moved.Budget
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
			decision, timing, requestID, err = ask(ctx, "step "+strconv.Itoa(step)+", asked again on "+on+" after a rate limit",
				llm.Request{Messages: withPlan(messages), Tools: definitions})
		}
		if err != nil {
			var cut llm.CutReply
			if errors.As(err, &cut) {
				answering = requestID
				messages = append(messages, llm.Message{Role: llm.RoleAssistant, Content: cut.Streamed}, inserted(llm.Origin{Source: llm.SourceReplyCut}, theReplyWasCut))
			}
			return fail(err)
		}
		row.Model = decision.Build
		row.TotalCostUSD += decision.Usage.Cost
		reported := decision.PromptAccounting.BilledTokens(decision.Usage.InputTokens, decision.CacheReadTokens) + decision.CacheWriteTokens
		budget = budget.Reported(reported, asSent)

		stepRow := stepFrom(requestID, step, timing, decision)
		stepRow.Compaction = trimmedBefore
		answering = stepRow.id
		measuredAgainst := budget.Bands
		stepRow.Occupancy, stepRow.Bands = &asSent, &measuredAgainst
		if decision.Outcome == llm.OutcomeMessage && strings.TrimSpace(decision.Content) == "" {
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
			messages = append(messages, inserted(llm.Origin{Source: llm.SourceBlankAnswer}, yourLastReplyWasEmpty))
			keep(stepRow)
			continue
		}
		askedAgainAfterBlank = false
		if asked != nil {
			answered, err := answeredBy(decision.Content)
			switch {
			case err != nil:
				recorded.notice("the person's question from step " + strconv.Itoa(asked.step) + " stays open, since jev could not judge the reply at step " + strconv.Itoa(step) + ": " + err.Error())
			case answered:
				recorded.notice("the person's question from step " + strconv.Itoa(asked.step) + " was answered at step " + strconv.Itoa(step))
				asked = nil
			}
		}

		switch decision.Outcome {
		case llm.OutcomeMessage:
			messages = append(messages, llm.Message{Role: llm.RoleAssistant, Content: decision.Content})
			stop := hook.Stop
			if config.SpawnedFrom != "" {
				stop = hook.SubagentStop
			}
			blocked := fire(ctx, hook.Input{Event: stop, LastMessage: decision.Content, StopActive: stopContinuations > 0}).Block
			if blocked != "" && stopContinuations < konst.HookStopContinuations {
				stopContinuations++
				messages = append(messages, inserted(llm.Origin{Source: llm.SourceStopHook}, "a "+string(stop)+" hook kept this turn going: "+blocked))
				keep(stepRow)
				continue
			}
			if blocked != "" {
				row.Warnings = append(row.Warnings, "the turn ended with a "+string(stop)+" hook still blocking after "+strconv.Itoa(stopContinuations)+" continuations: "+blocked)
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
				messages = append(messages, inserted(llm.Origin{Source: llm.SourceHandback}, handbackNote(offered, stepTools)))
				keep(stepRow)
				continue
			}
			if open := config.Inbox.stillAsked(); len(open) > 0 && !polled {
				polled = true
				messages = append(messages, inserted(llm.Origin{Source: llm.SourceHandback}, pollNote(open)))
				keep(stepRow)
				continue
			}
			work, held := lead.work()
			if held && work.open() && concluding == "" && !nudged {
				nudged = true
				if quiet := work.quietBecause(messages, saidByThePerson(taskOrigin), config.Task); quiet != "" {
					recorded.notice("the board nudge stayed quiet: " + quiet)
				} else {
					stepRow.Warnings = append(stepRow.Warnings, "the lead ended with board work open, so it was asked to take it")
					messages = append(messages, inserted(llm.Origin{Source: llm.SourceSlots}, work.asks(leadEndedOpening, listed(work.review))))
					keep(stepRow)
					continue
				}
			}
			keep(stepRow)
			answered(decision.Content)
			if arm != nil && config.TaskOrigin.Source != llm.SourceSlots {
				arm(lead.leadCheckIn(work, held))
			}
			return finish(OutcomeStopped), nil

		case llm.OutcomeTruncated:
			if decision.Content != "" || len(decision.ToolCalls) > 0 {
				messages = append(messages, llm.Message{Role: llm.RoleAssistant, Content: decision.Content,
					ToolCalls: decision.ToolCalls, Thinking: decision.Thinking})
			}
			if len(decision.ToolCalls) == 0 {
				keep(stepRow)
				return finish(OutcomeTruncated), nil
			}
			refuse(&stepRow, decision.ToolCalls, theResponseHitTheOutputTokenLimit)
			keep(stepRow)

		case llm.OutcomeToolCalls:
			messages = append(messages, llm.Message{Role: llm.RoleAssistant, Content: decision.Content, ToolCalls: decision.ToolCalls, Thinking: decision.Thinking})
			flush()
			if concluding != "" {
				refuse(&stepRow, decision.ToolCalls, theTurnEndsOnACleanReport)
				keep(stepRow)
				return endAt(OutcomeStopped, theTurnEndsOnACleanReport, messages), nil
			}
			if asked != nil && !asked.toolsHeld {
				asked.toolsHeld = true
				refuse(&stepRow, decision.ToolCalls, theAnswerComesFirst)
				holdFor(&stepRow, "the lead called a tool before answering the person")
				keep(stepRow)
				continue
			}
			pending, batches := decision.ToolCalls, 0
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
				wave := make([]gatedCall, 0, width)
				for _, asked := range pending[:width] {
					call := asked
					proxied, proxyRow := config.Proxy.rewrite(ctx, asked)
					call.Arguments = proxied
					pre := fire(ctx, hook.Input{Event: hook.PreToolUse, Tool: call.Name, Args: call.Arguments, CallID: call.ID})
					if pre.Args != nil {
						call.Arguments = pre.Args
					}
					request := GateRequest{TurnID: row.ID, Task: config.Task, Tool: call.Name, Args: call.Arguments, Call: call.ID, Reason: decision.Content}
					if pre.Args == nil {
						request.Asked = asked.Arguments
					}
					gated := gatedCall{call: call, asked: asked, proxy: proxyRow, id: session.EventIDFor(origin, call.ID), parent: stepRow.id, author: author, sift: sifter, thrift: thrifter, redact: redactor, task: config.Task, site: recorded.site(call.ID, messages), model: model, fire: fire, hooks: hookRunOf(hook.PreToolUse, pre), before: pre.Context}
					code, asksCode := codeAsked(stepTools, call)
					if asksCode = asksCode && config.SpawnedFrom != ""; !asksCode {
						gated.refusal = boundaryRefusal(stepTools, call)
					}
					if gated.refusal == "" {
						gated.refusal = hookRefusal(ctx, config, request, pre)
					}
					if asksCode && gated.refusal == "" {
						request.Code = &code
						if gated.refusal = codeRefusal(ctx, config.Person, request); gated.refusal == "" {
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
						verdict, err := config.Gate.Decide(ctx, request)
						gated.verdict = verdict
						if err != nil {
							gated.gateErr = err.Error()
						}
						if verdict.ID != "" {
							row.DecisionIDs = append(row.DecisionIDs, verdict.ID)
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
						if gated.refusal = gateRefusal(ctx, config.gatePerson(ctx), request, decided, gated.gateErr); gated.refusal != "" {
							gated.refusal = hooked + gated.refusal
						}
					}
					wave = append(wave, gated)
				}

				rows := make([]ToolCallRow, len(wave))
				answers := make([]llm.Message, len(wave))
				memoHits := make([]bool, len(wave))
				waveCtx, waveEnded := config.Inbox.stoppableWave(ctx)
				runAt := func(i, batch int) {
					gated := wave[i]
					if gated.call.Name == (askTool{}).Name() {
						defer waits.begin()()
					}
					rows[i], answers[i], memoHits[i] = gated.run(withShellCall(waveCtx, gated.id), toolsFor(gated), config.ResultBytesCap, artifacts, batch)
				}
				if len(wave) == 1 {
					runAt(0, 0)
				} else {
					batches++
					var running sync.WaitGroup
					crashed := make([]error, len(wave))
					running.Add(len(wave))
					for i := range wave {
						safe.Go(crashes, "parallel tool call", func() {
							defer running.Done()
							crashed[i] = safe.Call(nil, "parallel tool call", func() error { runAt(i, batches); return nil })
						})
					}
					running.Wait()
					if first := slices.IndexFunc(crashed, func(err error) bool { return err != nil }); first >= 0 {
						panic(crashed[first])
					}
				}
				leadStop = waveEnded()
				shown := make([]llm.Message, 0, len(answers))
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
			if config.Boundary != nil {
				asked := config.Boundary.Asked()
				stepRow.Grants = slices.DeleteFunc(slices.Clone(asked), func(question subagent.Question) bool { return slices.Contains(recordedGrants, question) })
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
			if config.TaskOrigin.Source == llm.SourceAsk && answeredAsksOnly(stepRow.ToolCalls) {
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
				switch {
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
				fork, begun, err := forkHistory(artifacts, budget, config.forkOpening(taskOrigin), messages, forced, forks+1, config.Caps.MaxForks)
				if err == nil {
					err = ctx.Err()
				}
				if err != nil {
					keep(stepRow)
					return fail(err)
				}
				if fork != nil && config.Caps.MaxForks > 0 && forks >= config.Caps.MaxForks {
					keep(stepRow)
					lead := "this turn reached its cap of " + strconv.Itoa(config.Caps.MaxForks) + " forks, and the work is not finished"
					return endAt(OutcomeStepCap, lead, messages), nil
				}
				if fork != nil {
					forks++
					fork.Step, fork.Into = step, origin+"-f"+strconv.Itoa(forks+1)
					begun, missing := stateCarried(fork, step, beforeFork, begun, definitions)
					stepRow.Fork = fork
					keep(stepRow)
					forkInto(fork, "after step "+strconv.Itoa(step), beforeFork, begun, moved)
					if missing != "" {
						row.Warnings = append(row.Warnings, missing)
					}
					continue
				}
			}
			unforked, err := trimUnderTarget(step, "after step "+strconv.Itoa(step))
			stepRow.Compaction = cmp.Or(unforked, stepRow.Compaction)
			keep(stepRow)
			if err != nil {
				return fail(err)
			}

		case llm.OutcomeRefusal:
			keep(stepRow)
			return fail(transport.Fail("turn.Run", transport.KindProvider, nil, "the model refused: %s", decision.Refusal))

		default:
			panic("turn: unknown model outcome")
		}
	}
}

func registrySnapshot(registry *shell.Registry) map[string]bool {
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

func backgroundSurvivors(registry *shell.Registry, before map[string]bool) string {
	if registry == nil {
		return ""
	}
	list, err := registry.List()
	if err != nil {
		return ""
	}
	var named []string
	for _, entry := range list {
		if before[entry.Name] || entry.State != shell.Running {
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

func stepFrom(id string, index int, timing requestTiming, decision llm.Decision) StepRow {
	return StepRow{
		id:               id,
		attempt:          timing.attempt,
		DurationMS:       timing.durationMS,
		FirstTokenMS:     decision.FirstTokenMS,
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
