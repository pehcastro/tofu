package turn

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"tofu/internal/llm"
	"tofu/internal/recall"
	"tofu/internal/session"
	"tofu/internal/shell"
	"tofu/internal/subagent"
	"tofu/internal/sys"
	"tofu/internal/transport"
)

type Model interface {
	Ask(ctx context.Context, request llm.Request) (llm.Decision, error)
}

type runningModelKey struct{}

type RunningModel struct{}

func (RunningModel) Ask(ctx context.Context, request llm.Request) (llm.Decision, error) {
	model, running := ctx.Value(runningModelKey{}).(Model)
	if !running {
		return llm.Decision{}, errors.New("no turn is running, so there is no turn model to ask")
	}
	return model.Ask(ctx, request)
}

func readsTheBrowser(tool string) bool {
	return tool == "browser_tabs" || tool == "browser_read"
}

type Caps struct {
	MaxSteps         int
	LoopGuardRepeats int
	LoopGuardWindow  int
}

const theResponseHitTheOutputTokenLimit = "the response hit the output token limit, so its arguments may be truncated. " +
	"Re-issue the tool call with complete arguments."

const theToolSucceededAndPrintedNothing = "the tool ran, succeeded and printed nothing."

const theToolFailedAndPrintedNothing = "the tool ran, failed and printed nothing."

const theToolWasAbortedAndPrintedNothing = "this call was cancelled elsewhere in the turn before it produced output. " +
	"it is not a failure of the command itself and does not need to be retried the same way."

const andThisIsItsLastStep = " and this is its last step: answer now from what you already have, " +
	"saying what you did, what is left undone, and what to do next."

type CalledAsTheStepIsRecordedAndBeforeTheNextOneIsAsked func(StepRow)

type CalledAsEachToolCallAnswersAndBeforeTheNextRequest func(llm.Message)

type Config struct {
	Model           Model
	Accounts        Accounts
	Spend           Spend
	Tools           Registry
	ToolSource      func() Registry
	Gate            Gate
	GateMode        GateMode
	Proxy           *CommandProxy
	Boundary        *subagent.Boundary
	Person          Person
	Task            string
	Images          []llm.Image
	History         []llm.Message
	Wire            string
	SpawnedFrom     string
	System          string
	Environment     string
	Caps            Caps
	Sift            *ShellSift
	Thrift          *ThriftSift
	ResultBytesCap  int
	ArtifactDir     string
	TruncateResults bool
	NoCompaction    bool
	NoFork          bool
	NoLastWord      bool
	Budget          recall.Budget
	Sessions        *session.Store
	Session         string
	Log             *session.Log
	Turn            string
	SpawnedBy       string
	Steering        func() []string
	Step            CalledAsTheStepIsRecordedAndBeforeTheNextOneIsAsked
	ToolResult      CalledAsEachToolCallAnswersAndBeforeTheNextRequest
	EndedSession    func(Row) error
	Now             func() time.Time
	NewID           func() string
}

func (c Config) FirstUserMessage() string {
	if c.Environment == "" {
		return c.Task
	}
	return c.Environment + "\n\n" + c.Task
}

func WriteSession(store *session.Store, row Row) error {
	header, events, err := row.Record()
	if err != nil {
		return err
	}
	return store.Write(header, events)
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
	withLoopTools := func(tools []Tool) []Tool {
		added := slices.Clone(tools)
		for _, tool := range tools {
			if spawner, spawning := tool.(*SpawnTool); spawning {
				added = append(added, messageTool{orchestrator: spawner})
			}
		}
		if handles {
			added = append(added, artifacts.FetchTool())
		}
		return added
	}
	currentTools := func() Registry {
		return NewRegistry(withLoopTools(source().tools)...)
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
	sentTools := withLoopTools(config.Tools.tools)
	usedTools := make([]string, len(sentTools))
	for i, tool := range sentTools {
		usedTools[i] = tool.Name()
	}
	row := Row{ID: origin, Schema: SchemaVersion, At: start, Task: config.Task, Wire: config.Wire, Spend: config.Spend, Root: origin, Account: account.ID, SpawnedFrom: config.SpawnedFrom, SpawnedBy: config.SpawnedBy, Budget: budget, System: config.System, Tools: usedTools}

	recorded, err := openRecord(config, row)
	if err != nil {
		return Row{}, err
	}
	row.Session = recorded.session()
	recorded.begin(row)

	messages := make([]llm.Message, 0, len(config.History)+2)
	if config.System != "" {
		messages = append(messages, llm.Message{Role: llm.RoleSystem, Content: config.System})
	}
	afterSystem := len(messages)
	messages = append(messages, config.History...)
	messages = append(messages, llm.Message{Role: llm.RoleUser, Content: config.FirstUserMessage(), Images: config.Images})

	var written sync.WaitGroup
	var forkWrites sync.Mutex
	var forkWriteErrs []string
	sent, answering := afterSystem+len(config.History), ""
	results := map[string]ToolCallRow{}
	flush := func() {
		for _, message := range messages[min(sent, len(messages)):] {
			recorded.message(message, answering, results)
		}
		sent = max(sent, len(messages))
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
		if survivors := backgroundSurvivors(registry, beforeShells); survivors != "" {
			row.Warnings = append(row.Warnings, survivors)
		}
		for _, failed := range forkWriteErrs {
			row.Warnings = append(row.Warnings, "the session this one forked from was not written: "+failed)
		}
		for _, tool := range currentTools().tools {
			if spawner, spawning := tool.(*SpawnTool); spawning {
				for _, subAgent := range spawner.SubAgentRows() {
					row.SubAgentIDs = append(row.SubAgentIDs, subAgent.ID)
				}
				spawner.mu.Lock()
				row.TotalCostUSD += spawner.spend
				spawner.mu.Unlock()
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
	endAt := func(outcome Outcome, lead string, step int, history []llm.Message) Row {
		messages = history
		if config.NoLastWord || step == 1 {
			return finish(outcome)
		}
		messages = append(slices.Clone(history), llm.Message{Role: llm.RoleUser, Content: lead + andThisIsItsLastStep})
		decision, timing, err := askCountingAttempts(ctx, model, llm.Request{Messages: messages, Tools: currentTools().Definitions()})
		row.TotalCostUSD += decision.Usage.Cost
		reason := ""
		switch {
		case err != nil:
			reason = err.Error()
		case decision.Outcome == llm.OutcomeTruncated:
			reason = "it hit the output token limit partway through"
		case decision.Outcome != llm.OutcomeMessage:
			reason = "the model answered with no text"
		}
		if reason != "" {
			row.Warnings = append(row.Warnings, lead+", and the last answer was not obtained: "+reason)
			return finish(outcome)
		}
		row.Model = decision.Build
		last := stepFrom(len(row.Steps)+1, timing, decision)
		answering = last.id
		messages = append(messages, llm.Message{Role: llm.RoleAssistant, Content: decision.Content})
		keep(last)
		return finish(outcome)
	}
	flush()
	guard := newLoopGuard(config.Caps)
	forks, recordedGrants := 0, 0
	for step := 1; ; step++ {
		if err := ctx.Err(); err != nil {
			return finish(OutcomeError), err
		}
		if config.Steering != nil {
			for _, steered := range config.Steering() {
				messages = append(messages, llm.Message{Role: llm.RoleUser, Content: steered})
			}
		}
		if config.Caps.MaxSteps > 0 && step > config.Caps.MaxSteps {
			lead := "this turn reached its " + OutcomeStepCap.String() + " of " + strconv.Itoa(config.Caps.MaxSteps) + ", and the work is not finished"
			return endAt(OutcomeStepCap, lead, step, messages), nil
		}

		stepTools := currentTools()
		asSent := recall.Measure(artifacts.preview, budget.Bands, historyOf(messages))
		decision, timing, err := askCountingAttempts(ctx, model, llm.Request{Messages: messages, Tools: stepTools.Definitions()})
		if err != nil {
			return finish(OutcomeError), err
		}
		row.Model = decision.Build
		row.TotalCostUSD += decision.Usage.Cost

		stepRow := stepFrom(step, timing, decision)
		answering = stepRow.id
		measuredAgainst := budget.Bands
		stepRow.Occupancy, stepRow.Bands = &asSent, &measuredAgainst

		switch decision.Outcome {
		case llm.OutcomeMessage:
			messages = append(messages, llm.Message{Role: llm.RoleAssistant, Content: decision.Content})
			keep(stepRow)
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
			for _, call := range decision.ToolCalls {
				callRow, resultMessage := rejectedCall(call, time.Now(),
					"tool call "+strconv.Quote(call.Name)+" was not executed: "+theResponseHitTheOutputTokenLimit,
					session.EventIDFor(origin, call.ID), stepRow.id, row.author())
				stepRow.ToolCalls, results[call.ID] = append(stepRow.ToolCalls, callRow), callRow
				messages = append(messages, resultMessage)
			}
			keep(stepRow)

		case llm.OutcomeToolCalls:
			messages = append(messages, llm.Message{Role: llm.RoleAssistant, ToolCalls: decision.ToolCalls, Thinking: decision.Thinking})
			flush()
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
					request := GateRequest{TurnID: row.ID, Task: config.Task, Tool: call.Name, Args: call.Arguments}
					gated := gatedCall{call: call, asked: asked, proxy: proxyRow, id: session.EventIDFor(origin, call.ID), parent: stepRow.id, author: author, sift: sifter, thrift: thrifter, redact: redactor, task: config.Task, site: recorded.site(call.ID, messages), model: model}
					if config.Gate != nil && !readsTheBrowser(call.Name) {
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
						gated.refusal = gateRefusal(ctx, config.Person, request, gated.verdict, gated.gateErr)
					}
					wave = append(wave, gated)
				}

				rows := make([]ToolCallRow, len(wave))
				answers := make([]llm.Message, len(wave))
				if len(wave) == 1 {
					rows[0], answers[0] = wave[0].run(ctx, stepTools, config.ResultBytesCap, artifacts, 0)
				} else {
					batches++
					var running sync.WaitGroup
					running.Add(len(wave))
					for i, gated := range wave {
						go func() {
							defer running.Done()
							rows[i], answers[i] = gated.run(ctx, stepTools, config.ResultBytesCap, artifacts, batches)
						}()
					}
					running.Wait()
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
				return finish(OutcomeError), err
			}
			if tripped {
				cause := loopGuardCause(repeated, repeats, guard.window)
				stopped := "this turn stopped itself because " + cause
				row.Guard = &LoopGuardStop{Tool: repeated.Tool, Args: repeated.Args, Repeats: repeats}
				row.Warnings = append(row.Warnings, "the turn stopped itself: "+cause)
				for _, unanswered := range pending {
					callRow, resultMessage := rejectedCall(unanswered, time.Now(),
						"tool call "+strconv.Quote(unanswered.Name)+" was not executed: "+stopped,
						session.EventIDFor(origin, unanswered.ID), stepRow.id, author)
					stepRow.ToolCalls, results[unanswered.ID] = append(stepRow.ToolCalls, callRow), callRow
					messages = append(messages, resultMessage)
				}
				keep(stepRow)
				return endAt(OutcomeLoopGuard, stopped, step, messages), nil
			}
			if !config.NoFork {
				moved, moving, forced := Account{}, false, ForkKind("")
				if config.Accounts.Next != nil {
					next, spent, nextErr := config.Accounts.Next(ctx, account)
					switch {
					case nextErr != nil:
						row.Warnings = append(row.Warnings,
							"the pinned account's windows could not be read, so this session stays on it: "+nextErr.Error())
					case spent:
						moved, moving, forced = next, true, ForkAccountSpent
					}
				}
				fork, begun, err := forkHistory(artifacts, budget, config.FirstUserMessage(), messages, forced)
				if err == nil {
					err = ctx.Err()
				}
				if err != nil {
					keep(stepRow)
					return finish(OutcomeError), err
				}
				if fork != nil {
					forks++
					fork.Step, fork.Into = step, origin+"-f"+strconv.Itoa(forks+1)
					stepRow.Fork = fork
					keep(stepRow)
					ended := row
					ended.Outcome, ended.ForkedInto = OutcomeForked, fork.Into
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
					if moving {
						row.Account = moved.ID
						row.Warnings = append(row.Warnings, movedAccountWords(account, moved, fork.TokensAfter))
						account = moved
						if moved.Model != nil {
							model = moved.Model
						}
					}
					recorded.fork(ended, row, fork, row.At)
					row.Session = recorded.session()
					messages, sent = begun, 0
					continue
				}
			}
			if !config.NoCompaction && budget.Automatic {
				compaction, err := compactHistory(artifacts, budget, step, messages)
				if err != nil {
					keep(stepRow)
					return finish(OutcomeError), err
				}
				stepRow.Compaction = compaction
			}
			keep(stepRow)

		case llm.OutcomeRefusal:
			keep(stepRow)
			return finish(OutcomeError), transport.Fail("turn.Run", transport.KindProvider, nil, "the model refused: %s", decision.Refusal)

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

func NewID(at time.Time) string {
	return session.IDPrefix + strconv.FormatInt(at.UnixNano(), 16)
}

func stepFrom(index int, timing requestTiming, decision llm.Decision) StepRow {
	return StepRow{
		id:               session.NewEventID(),
		attempt:          timing.attempt,
		DurationMS:       timing.durationMS,
		FirstTokenMS:     decision.FirstTokenMS,
		Index:            index,
		AssistantText:    decision.Content,
		Model:            decision.Build,
		StopReason:       decision.Stop,
		PromptTokens:     decision.Usage.InputTokens,
		CompletionTokens: decision.Usage.OutputTokens,
		CacheReadTokens:  decision.CacheReadTokens,
		CacheWriteTokens: decision.CacheWriteTokens,
		CostUSD:          decision.Usage.Cost,
		Warnings:         decision.Warnings,
	}
}

type gatedCall struct {
	call    llm.ToolCall
	asked   llm.ToolCall
	proxy   *ProxyRow
	id      string
	parent  string
	author  string
	task    string
	sift    *ShellSift
	thrift  *ThriftSift
	redact  sys.KeyRedactor
	site    spawnSite
	model   Model
	verdict GateDecision
	gateErr string
	refusal string
}

func (g gatedCall) run(ctx context.Context, tools Registry, resultBytesCap int, artifacts Artifacts, batch int) (ToolCallRow, llm.Message) {
	row, answer := rejectedCall(g.call, time.Now(), g.refusal, g.id, g.parent, g.author)
	if g.refusal == "" {
		row, answer = g.execute(ctx, tools, resultBytesCap, artifacts)
	}
	row.GateDecisionID, row.GateVerdict, row.GateError = g.verdict.ID, string(g.verdict.Verdict), g.gateErr
	row.ParallelBatch = batch
	row.Proxy = g.proxy
	row.Command = g.redact.Redact(row.Command)
	if len(row.Args) > 0 {
		row.Args = json.RawMessage(g.redact.Redact(string(row.Args)))
	}
	return row, answer
}

func (g gatedCall) execute(ctx context.Context, tools Registry, resultBytesCap int, artifacts Artifacts) (ToolCallRow, llm.Message) {
	call := g.call
	started := time.Now()
	ctx = context.WithValue(context.WithValue(context.WithValue(ctx, shellOwnerKey{}, g.author), spawnSiteKey{}, g.site), runningModelKey{}, g.model)
	tool, ok := tools.byName[call.Name]
	if !ok {
		return rejectedCall(call, started, "unknown tool "+strconv.Quote(call.Name), g.id, g.parent, g.author)
	}

	result, err := tool.Run(ctx, call.Arguments)
	if err == nil && g.proxy != nil && g.proxy.Ran != "" && proxyPanicked(result.Content) {
		g.proxy.ProxyBytes = len(result.Content)
		g.proxy.Note = g.proxy.Proxy + " panicked instead of filtering the output, so the command ran as it was asked for"
		call = g.asked
		result, err = tool.Run(ctx, call.Arguments)
	}
	if err != nil {
		return rejectedCall(call, started, g.redact.Redact(err.Error()), g.id, g.parent, g.author)
	}
	result.Content, result.FailureText = g.redact.Redact(result.Content), g.redact.Redact(result.FailureText)

	cut := g.cutShellResult(ctx, call.Name, result)
	thriftCut := g.cutThriftResult(ctx, call.Name, result)
	saved := cut.Saved
	text := cut.Text
	if thriftCut.Saved > 0 {
		text = thriftCut.Text
		saved += thriftCut.Saved
	}
	rendered, handle, storeErr := artifacts.Render(text, resultBytesCap)
	sum := sha256.Sum256([]byte(result.Content))
	row := ToolCallRow{
		ID:             g.id,
		Parent:         g.parent,
		Author:         g.author,
		Call:           call.ID,
		Tool:           call.Name,
		Args:           call.Arguments,
		Command:        result.Command,
		ExitCode:       result.ExitCode,
		ResultBytes:    len(result.Content),
		RenderedBytes:  len(rendered),
		ResultHash:     hex.EncodeToString(sum[:]),
		ResultHandle:   handle,
		SiftSavedBytes: saved,
		DurationMS:     time.Since(started).Milliseconds(),
		Error:          result.FailureText,
		SubAgentID:     result.SubAgent,
	}
	if storeErr != nil {
		row.ResultHandleError = storeErr.Error()
	}
	outcome := row.Outcome()
	if result.Outcome == ResultAborted {
		outcome = llm.ToolOutcomeAborted
	}
	body := rendered
	if body == "" {
		switch outcome {
		case llm.ToolOutcomeAborted:
			body = theToolWasAbortedAndPrintedNothing
		case llm.ToolOutcomeFailed:
			body = theToolFailedAndPrintedNothing
		default:
			body = theToolSucceededAndPrintedNothing
		}
	}
	return row, llm.Message{
		Role:            llm.RoleTool,
		ToolCallID:      call.ID,
		Content:         body,
		ToolOutcome:     outcome,
		ToolResultBytes: row.ResultBytes,
	}
}

func rejectedCall(call llm.ToolCall, started time.Time, reason, id, parent, author string) (ToolCallRow, llm.Message) {
	content := "error: " + reason
	row := ToolCallRow{ID: id, Parent: parent, Author: author, Call: call.ID, Tool: call.Name, Args: call.Arguments, Error: reason, DurationMS: time.Since(started).Milliseconds()}
	return row, llm.Message{
		Role:            llm.RoleTool,
		ToolCallID:      call.ID,
		Content:         content,
		ToolOutcome:     row.Outcome(),
		ToolResultBytes: len(content),
	}
}
