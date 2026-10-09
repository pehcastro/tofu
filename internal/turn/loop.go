package turn

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"tofu/internal/hook"
	"tofu/internal/judge/ledger"
	"tofu/internal/konst"
	"tofu/internal/llm"
	"tofu/internal/recall"
	"tofu/internal/session"
	"tofu/internal/shell"
	"tofu/internal/sys"
	"tofu/internal/transport"
)

const theResponseHitTheOutputTokenLimit = "the response hit the output token limit, so its arguments may be truncated. " +
	"Re-issue the tool call with complete arguments."

const theTurnEndsOnACleanReport = "this turn ends on a clean sub-agent report, so it runs no tool"

const andThisIsItsLastStep = ", and this is its last step: answer now from what you already have, " +
	"saying what you did, what is left undone, and what to do next. no tool runs on this step."

const theLastStepRunsNoTool = "this was the turn's last step, which runs no tool"

const aMessageArrivedMidTurn = "the person's next message arrived while you were working. before your next tool call, " +
	"write one line saying what it changes in what you are doing, or that it changes nothing."

const theLeadSkippedTheLine = "the lead read a message from the person mid-turn and called a tool without a line saying what it changes"

const cronTaskSource = "cron "

type SentNow struct{}

func (SentNow) Error() string {
	return "the person sent a queued message now, so this request was dropped and the step asked again with it"
}

func Run(ctx context.Context, config Config) (Row, error) {
	if config.Model == nil && config.Accounts.Pick == nil {
		return Row{}, errors.New("turn: no model")
	}
	if strings.TrimSpace(config.Task) == "" {
		return Row{}, errors.New("turn: no task")
	}
	if config.ResultBytesCap <= 0 {
		return Row{}, errors.New("turn: no tool result byte cap")
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
	dir := config.ArtifactDir
	if dir == "" {
		state, err := sys.ProjectStateDir()
		if err != nil {
			return Row{}, err
		}
		dir = filepath.Join(state, "artifacts")
	}
	handles := !config.TruncateResults
	artifacts, err := NewArtifacts(dir, handles)
	if err != nil {
		return Row{}, err
	}
	artifacts.preview = artifacts.preview.OnWire(config.Wire)
	sifter := siftOrNothing(config.Sift)
	thrifter := thriftOrNothing(config.Thrift)
	redactor := sys.LoadKeyRedactor()
	source := config.ToolSource
	if source == nil {
		source = func() Registry { return config.Tools }
	}
	var recorded *record
	personAnswers := config.Person != nil && config.SpawnedFrom == "" && !strings.HasPrefix(config.TaskOrigin.Source, cronTaskSource)
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
		if account.Model != nil {
			model = account.Model
		}
	}
	if model == nil {
		return Row{}, errors.New("turn: the account this session was pinned to came with no model")
	}
	start := now()
	origin := newID()
	sentTools := answerable(withLoopTools(config.Tools.tools))
	usedTools := make([]string, len(sentTools))
	for i, tool := range sentTools {
		usedTools[i] = tool.Name()
	}
	row := Row{ID: origin, Schema: SchemaVersion, At: start, Task: config.Task, Wire: config.Wire, Spend: config.Spend, Root: origin, Account: account.ID, SpawnedFrom: config.SpawnedFrom, SpawnedBy: config.SpawnedBy, Budget: budget, System: config.System, Tools: usedTools}

	recorded, err = openRecord(config, row)
	if err != nil {
		return Row{}, err
	}
	row.Session = recorded.session()
	recorded.begin(row)
	if len(config.History) > 0 {
		recorded.listChange("carried", "the turn starts on the conversation the last turn or the resume left", nil, config.History)
	}

	hooks, untrustedHooks := hookEngine(ctx, config, origin)
	ctx = context.WithValue(ctx, hookEngineKey{}, hooks)
	row.Warnings = append(row.Warnings, untrustedHooks...)
	var hookWarnings sync.Mutex
	fire := func(in hook.Input) hook.Verdict {
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
	if system := config.Prefix.hold(config.SystemMessage(), NewRegistry(withLoopTools(source().tools)...).Definitions()); system != "" {
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
	answered := func(reply string) {
		episode(episodeOfLead, reply)
		if keeper == nil {
			return
		}
		if err := keeper.Compact(); err != nil {
			row.Warnings = append(row.Warnings, "the episode view was not summarized after this turn: "+err.Error())
		}
	}
	messages = slices.Concat(messages, config.MemoryMessage(), config.History)
	if keeper != nil && len(config.History) == 0 {
		view, err := keeper.Episodes()
		if err != nil {
			row.Warnings = append(row.Warnings, "the episode view is not sent this session: "+err.Error())
		}
		if view != "" {
			messages = withEpisodes(messages, view)
		}
	}
	concluding, taken := "", ""
	for _, tool := range config.Tools.tools {
		if spawner, spawning := tool.(*SpawnTool); spawning && !spawner.ChecksWork {
			concluding = spawner.cleanReport(config.Task)
			if concluding == "" {
				taken = spawner.reportTaken(config.Task)
			}
		}
	}
	var promptRefused, hookContext string
	if config.SpawnedFrom == "" {
		if config.SessionSource != "" {
			hookContext = fire(hook.Input{Event: hook.SessionStart, Source: config.SessionSource}).Context
		}
		prompted := fire(hook.Input{Event: hook.UserPromptSubmit, Prompt: config.Task})
		promptRefused, hookContext = prompted.Block, strings.TrimSpace(hookContext+"\n\n"+prompted.Context)
	}
	first := config.FirstUserMessage(concluding+taken, hookContext)
	taskOrigin, taskSource := config.TaskOrigin, sourceTask
	if config.SpawnedFrom != "" {
		taskSource = sourceBrief
	}
	taskOrigin.Source, taskOrigin.TakenAt = cmp.Or(taskOrigin.Source, taskSource), start
	messages = append(messages, llm.Message{Role: llm.RoleUser, Content: first, Images: config.Images, Origin: taskOrigin})
	episode(episodeKind(taskOrigin.Source), config.Task)

	var written, shadowed sync.WaitGroup
	var shadows sync.Mutex
	var shadowIDs, shadowErrs []string
	var forkWrites sync.Mutex
	var forkWriteErrs []string
	sent, answering := afterSystem+len(config.History), ""
	results := map[string]ToolCallRow{}
	refuse := func(step *StepRow, calls []llm.ToolCall, why string) {
		for _, call := range calls {
			callRow, resultMessage := rejectedCall(call, time.Now(), "tool call "+strconv.Quote(call.Name)+" was not executed: "+why,
				session.EventIDFor(origin, call.ID), step.id, row.author())
			callRow.Refused = true
			step.ToolCalls, results[call.ID] = append(step.ToolCalls, callRow), callRow
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
	ask := func(ctx context.Context, why string, request llm.Request) (llm.Decision, requestTiming, string, error) {
		id, started := session.NewEventID(), time.Now()
		if tokens := sentTokens(request.Messages); tokens > budget.Ceiling() {
			err := fmt.Errorf("this request is about %d tokens, past the %d token ceiling after the trim and the fork, so it was not sent", tokens, budget.Ceiling())
			recorded.exchange(id, why, config.Wire, request, started, nil, llm.Decision{}, err)
			return llm.Decision{}, requestTiming{}, id, err
		}
		tapped, tap := llm.Tapped(ctx)
		decision, timing, err := askCountingAttempts(tapped, model, request)
		recorded.exchange(id, why, config.Wire, request, started, tap.Attempts(), decision, err)
		return decision, timing, id, err
	}
	inserted := func(source, text string, posted time.Time) llm.Message {
		return llm.Message{Role: llm.RoleUser, Content: text, Origin: llm.Origin{Source: source, PostedAt: posted, TakenAt: now()}}
	}
	compacted := func() {
		if config.SpawnedFrom != "" {
			return
		}
		if said := fire(hook.Input{Event: hook.SessionStart, Source: sessionSourceCompact}).Context; said != "" {
			messages = append(messages, inserted(sourceSessionStartHook, said, time.Time{}))
		}
	}
	trim := func(step int, when string) (*Compaction, error) {
		if config.NoCompaction || !budget.Automatic {
			return nil, nil
		}
		before := slices.Clone(messages)
		compaction, shrink, err := trimRead(artifacts, budget, step, messages)
		if compaction != nil {
			recorded.listChange("compaction", when+": the conversation crossed its budget, so tofu "+shrink.String(), before, messages)
			compacted()
		}
		return compaction, err
	}
	stateCarried := func(fork *Fork, step int, beforeFork, begun []llm.Message, tools []llm.Tool) ([]llm.Message, string) {
		if keeper != nil {
			view, err := keeper.Episodes()
			missing := ""
			if err != nil {
				missing = "the fork carries no episode view, because reading it failed: " + err.Error()
			}
			if view != "" {
				begun = withEpisodes(begun, view)
			}
			carryState(fork, begun, "", row.Session)
			fork.TokensAfter = budget.Tokens(artifacts.preview, historyOf(begun))
			return begun, missing
		}
		asked := beforeFork
		if sentTokens(asked) > budget.Ceiling() {
			asked = begun
		}
		request := llm.Request{Messages: append(slices.Clone(asked), inserted(sourceForkState, forkStateAsk(), time.Time{})), Tools: tools}
		if len(tools) > 0 {
			request.ToolChoice = llm.ToolChoiceNone
		}
		decision, timing, id, err := ask(context.WithValue(ctx, quietAskKey{}, true), "the working state the fork carries", request)
		row.TotalCostUSD += decision.Usage.Cost
		if err == nil {
			spent := stepFrom(id, step, timing, decision)
			spent.AssistantText = ""
			recorded.step(spent)
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
		fork.TokensAfter = budget.Tokens(artifacts.preview, historyOf(begun))
		return begun, missing
	}
	keep := func(step StepRow) {
		flush()
		recorded.step(step)
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
	endAt := func(outcome Outcome, lead string, history []llm.Message) Row {
		messages = history
		if config.NoLastWord {
			return finish(outcome)
		}
		messages = append(slices.Clone(history), inserted(sourceLastWord, lead+andThisIsItsLastStep, time.Time{}))
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
	forks, recordedGrants, stopContinuations, handbacks := 0, 0, 0, 0
	forkInto := func(fork *Fork, when string, beforeFork, begun []llm.Message, moved *Account) {
		ended := row
		ended.Outcome, ended.ForkedInto, ended.EndedInFork, ended.Conversation = OutcomeForked, fork.Into, fork, messages[afterSystem:]
		ended.WallClockMS = now().Sub(start).Milliseconds()
		if config.EndedSession != nil {
			written.Add(1)
			go func() {
				defer written.Done()
				if err := config.EndedSession(ended); err != nil {
					forkWrites.Lock()
					forkWriteErrs = append(forkWriteErrs, err.Error())
					forkWrites.Unlock()
				}
			}()
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
		}
		if moved != nil {
			row.Account = moved.ID
			row.Warnings = append(row.Warnings, movedAccountWords(account, *moved, fork.TokensAfter))
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
	askedAgainAfterBlank, polled := false, false
	steeredAt := 0
	noticeStep := config.Caps.MaxSteps - max(1, int(math.Ceil(float64(config.Caps.MaxSteps)*konst.TurnStepCapNoticeShare)))
	for step := 1; ; step++ {
		if err := ctx.Err(); err != nil {
			return fail(err)
		}
		if config.Steering != nil {
			for _, steered := range config.Steering() {
				if steeredAt != step {
					messages = append(messages, inserted(sourceMidTurnNote, aMessageArrivedMidTurn, time.Time{}))
				}
				steeredAt = step
				said := inserted(sourceSteer, steered, time.Time{})
				if config.ImagesOf != nil {
					said.Images = config.ImagesOf(steered)
				}
				messages = append(messages, said)
				episode(episodeOfPerson, steered)
			}
		}
		if config.Inbox != nil {
			for _, item := range config.Inbox.takeItems() {
				messages = append(messages, inserted(item.source, item.text, item.posted))
				episode(episodeKind(item.source), item.text)
				if item.source == sourceMemory {
					recorded.notice(item.text)
				}
			}
		}
		capped := config.Caps.MaxSteps > 0
		switch {
		case config.Caps.WallClock > 0 && now().Sub(start) >= config.Caps.WallClock:
			lead := "this sub-agent retired at its wall clock cap of " + config.Caps.WallClock.String() + ", and the work is not finished"
			row.Warnings = append(row.Warnings, lead)
			return endAt(OutcomeRetiredWallClockCap, lead, messages), nil
		case capped && step >= config.Caps.MaxSteps:
			lead := "this turn reached its step cap of " + strconv.Itoa(config.Caps.MaxSteps) + ", and the work is not finished"
			return endAt(OutcomeStepCap, lead, messages), nil
		case capped && step == noticeStep:
			messages = append(messages, inserted(sourceStepCapNotice, "this turn has "+strconv.Itoa(config.Caps.MaxSteps-step+1)+
				" steps left before its step cap of "+strconv.Itoa(config.Caps.MaxSteps)+", and the last of them runs no tool: "+
				"finish what you are doing, or stop and report what is done and what is left.", time.Time{}))
		}

		stepTools := currentTools()
		definitions := sending(stepTools.Definitions())
		schemas, err := json.Marshal(definitions)
		if err != nil {
			return fail(err)
		}
		budget = budget.Sending(artifacts.preview, string(schemas))
		before := "before step " + strconv.Itoa(step)
		due := step == 1 && len(config.History) > 0 || sentTokens(messages) > budget.Ceiling()
		if due {
			if _, err := trim(step, before); err != nil {
				return fail(err)
			}
		}
		if due && !config.NoFork {
			beforeFork := slices.Clone(messages)
			fork, begun, err := forkHistory(artifacts, budget, config.FirstUserMessage(), messages, "", forks+1, config.Caps.MaxForks)
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
		keepNewestPictures(messages)
		asSent := recall.Measure(artifacts.preview, budget.Bands, historyOf(messages))
		decision, timing, requestID, err := ask(ctx, "step "+strconv.Itoa(step), llm.Request{Messages: messages, Tools: definitions})
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
				decision, timing, requestID, err = ask(ctx, "step "+strconv.Itoa(step)+", asked again after the overflow shrink", llm.Request{Messages: messages, Tools: definitions})
				if overflowed(err) {
					err = fmt.Errorf("the context window overflowed again after tofu %s, so the turn ends: %w", shrink, err)
				}
			}
		}
		if err != nil {
			return fail(err)
		}
		row.Model = decision.Build
		row.TotalCostUSD += decision.Usage.Cost
		reported := decision.PromptAccounting.BilledTokens(decision.Usage.InputTokens, decision.CacheReadTokens) + decision.CacheWriteTokens
		budget = budget.Reported(reported, asSent)

		stepRow := stepFrom(requestID, step, timing, decision)
		answering = stepRow.id
		measuredAgainst := budget.Bands
		stepRow.Occupancy, stepRow.Bands = &asSent, &measuredAgainst
		if steeredAt == step && decision.Outcome == llm.OutcomeToolCalls && strings.TrimSpace(decision.Content) == "" {
			stepRow.Warnings = append(stepRow.Warnings, theLeadSkippedTheLine)
		}

		if decision.Outcome == llm.OutcomeMessage && strings.TrimSpace(decision.Content) == "" {
			if askedAgainAfterBlank {
				keep(stepRow)
				return fail(transport.Fail("turn.Run", transport.KindInvalidAnswer, nil,
					"the model answered twice in a row with no text and no tool call"))
			}
			askedAgainAfterBlank = true
			stepRow.Warnings = append(stepRow.Warnings, "the model answered with no text and no tool call, so it was asked again")
			keep(stepRow)
			continue
		}
		askedAgainAfterBlank = false

		switch decision.Outcome {
		case llm.OutcomeMessage:
			messages = append(messages, llm.Message{Role: llm.RoleAssistant, Content: decision.Content})
			stop := hook.Stop
			if config.SpawnedFrom != "" {
				stop = hook.SubagentStop
			}
			blocked := fire(hook.Input{Event: stop, LastMessage: decision.Content, StopActive: stopContinuations > 0}).Block
			if blocked != "" && stopContinuations < konst.HookStopContinuations {
				stopContinuations++
				messages = append(messages, inserted(sourceStopHook, "a "+string(stop)+" hook kept this turn going: "+blocked, time.Time{}))
				keep(stepRow)
				continue
			}
			if blocked != "" {
				row.Warnings = append(row.Warnings, "the turn ended with a "+string(stop)+" hook still blocking after "+strconv.Itoa(stopContinuations)+" continuations: "+blocked)
			}
			if offered := handedBack(decision.Content); offered != "" && config.SpawnedFrom == "" && concluding == "" && handbacks < konst.LeadHandbackContinuations {
				handbacks++
				stepRow.Warnings = append(stepRow.Warnings, "the lead ended by handing the person a step, so it was asked to take it: "+offered)
				messages = append(messages, inserted(sourceHandback, handbackNote(offered, stepTools), time.Time{}))
				keep(stepRow)
				continue
			}
			if open := config.Inbox.stillAsked(); len(open) > 0 && !polled {
				polled = true
				messages = append(messages, inserted(sourceHandback, pollNote(open), time.Time{}))
				keep(stepRow)
				continue
			}
			keep(stepRow)
			answered(decision.Content)
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
			pending, batches := decision.ToolCalls, 0
			var tripped bool
			var repeated ToolCallRow
			var repeats int
			author := row.author()
			for len(pending) > 0 && !tripped {
				width := max(stepTools.parallelPrefix(pending), 1)
				wave := make([]gatedCall, 0, width)
				for _, asked := range pending[:width] {
					call := asked
					proxied, proxyRow := config.Proxy.rewrite(ctx, asked)
					call.Arguments = proxied
					pre := fire(hook.Input{Event: hook.PreToolUse, Tool: call.Name, Args: call.Arguments, CallID: call.ID})
					if pre.Args != nil {
						call.Arguments = pre.Args
					}
					request := GateRequest{TurnID: row.ID, Task: config.Task, Tool: call.Name, Args: call.Arguments, Call: call.ID}
					gated := gatedCall{call: call, asked: asked, proxy: proxyRow, id: session.EventIDFor(origin, call.ID), parent: stepRow.id, author: author, sift: sifter, thrift: thrifter, redact: redactor, task: config.Task, site: recorded.site(call.ID, messages), model: model, fire: fire, hooks: hookRunOf(hook.PreToolUse, pre)}
					if gated.refusal = boundaryRefusal(stepTools, call); gated.refusal == "" {
						gated.refusal = hookRefusal(ctx, config, request, pre)
					}
					if gated.refusal != "" {
						wave = append(wave, gated)
						continue
					}
					switch {
					case config.Gate == nil || gateExempt(call.Name):
					case config.GateMode == GateShadow:
						judged := make(chan shadowVerdict, 1)
						gated.shadow = judged
						shadowed.Add(1)
						go func() {
							defer shadowed.Done()
							verdict, err := config.Gate.Decide(context.WithoutCancel(ctx), request)
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
						}()
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
							judged := fire(hook.Input{Event: hook.GateVerdict, Tool: call.Name, Args: call.Arguments, CallID: call.ID,
								Gate: &hook.GateFacts{Verdict: decided.Verdict, Risk: decided.Reason, Questions: append([]ledger.Answer{}, decided.Answers...)}})
							gated.hooks = append(gated.hooks, hookRunOf(hook.GateVerdict, judged)...)
							if judged.Gate != ledger.VerdictUnset && judged.Gate != decided.Verdict {
								decided.Verdict, decided.PersonOnly = judged.Gate, true
								hooked = "this call did not run: a GateVerdict hook turned the gate's " + string(gated.verdict.Verdict) + " into " + string(judged.Gate) + ": " + judged.GateWhy + ". "
							}
						}
						if gated.refusal = gateRefusal(ctx, config.Person, request, decided, gated.gateErr); gated.refusal != "" {
							gated.refusal = hooked + gated.refusal
						}
					}
					wave = append(wave, gated)
				}

				rows := make([]ToolCallRow, len(wave))
				answers := make([]llm.Message, len(wave))
				memoHits := make([]bool, len(wave))
				if len(wave) == 1 {
					rows[0], answers[0], memoHits[0] = wave[0].run(ctx, stepTools, config.ResultBytesCap, artifacts, 0)
				} else {
					batches++
					var running sync.WaitGroup
					running.Add(len(wave))
					for i, gated := range wave {
						go func() {
							defer running.Done()
							rows[i], answers[i], memoHits[i] = gated.run(ctx, stepTools, config.ResultBytesCap, artifacts, batches)
						}()
					}
					running.Wait()
				}
				for i := range answers {
					if memoHits[i] {
						answers[i].Content, answers[i].Images = pointAtCopy(answers[i], messages, answers[:i]), nil
						rows[i].RenderedBytes = len(answers[i].Content)
					}
				}
				stepRow.ToolCalls = append(stepRow.ToolCalls, rows...)
				for i, gated := range wave {
					results[gated.call.ID] = rows[i]
				}
				messages = append(messages, answers...)
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
				stepRow.Grants = slices.Clone(asked[recordedGrants:])
				recordedGrants = len(asked)
			}
			if err := ctx.Err(); err != nil {
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
			if config.TaskOrigin.Source == sourceGateAsk && answeredAsksOnly(stepRow.ToolCalls) {
				keep(stepRow)
				answered(decision.Content)
				return finish(OutcomeStopped), nil
			}
			if started := startedSpawnsOnly(stepTools, stepRow.ToolCalls); config.SpawnedFrom == "" && len(started) > 0 {
				if strings.TrimSpace(decision.Content) == "" {
					messages = append(messages, llm.Message{Role: llm.RoleAssistant, Content: strings.Join(started, "\n"), Origin: llm.Origin{Source: sourceSpawnLine, TakenAt: now()}})
				}
				keep(stepRow)
				answered(decision.Content)
				return finish(OutcomeStopped), nil
			}
			if stepRow.Compaction, err = trim(step, "after step "+strconv.Itoa(step)); err != nil {
				keep(stepRow)
				return fail(err)
			}
			if !config.NoFork {
				var moved *Account
				forced := ForkKind("")
				if config.Accounts.Next != nil {
					next, spent, nextErr := config.Accounts.Next(ctx, account)
					switch {
					case nextErr != nil:
						row.Warnings = append(row.Warnings,
							"the pinned account's windows could not be read, so this session stays on it: "+nextErr.Error())
					case spent:
						moved, forced = &next, ForkAccountSpent
					}
				}
				beforeFork := slices.Clone(messages)
				fork, begun, err := forkHistory(artifacts, budget, config.FirstUserMessage(), messages, forced, forks+1, config.Caps.MaxForks)
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
			keep(stepRow)

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
