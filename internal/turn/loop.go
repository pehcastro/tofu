package turn

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"tofu/internal/konst"
	"tofu/internal/llm"
	"tofu/internal/recall"
	"tofu/internal/session"
	"tofu/internal/subagent"
	"tofu/internal/sys"
	"tofu/internal/transport"
)

type Model interface {
	Ask(ctx context.Context, request llm.Request) (llm.Decision, error)
}

type Caps struct {
	MaxSteps         int
	LoopGuardRepeats int
	LoopGuardWindow  int
}

func (c Caps) exceeded(step int) (Outcome, bool) {
	if c.MaxSteps > 0 && step > c.MaxSteps {
		return OutcomeStepCap, true
	}
	return OutcomeUnset, false
}

const theResponseHitTheOutputTokenLimit = "the response hit the output token limit, so its arguments may be truncated. " +
	"Re-issue the tool call with complete arguments."

const theToolSucceededAndPrintedNothing = "the tool ran, succeeded and printed nothing."

const theToolFailedAndPrintedNothing = "the tool ran, failed and printed nothing."

const andThisIsItsLastStep = " and this is its last step: answer now from what you already have, " +
	"saying what you did, what is left undone, and what to do next."

type CalledOnItsOwnGoroutineAndAlwaysBeforeRunReturns func(StepRow)

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
	Steering        func() []string
	Step            CalledOnItsOwnGoroutineAndAlwaysBeforeRunReturns
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
	source := config.ToolSource
	if source == nil {
		source = func() Registry { return config.Tools }
	}
	currentTools := func() Registry {
		live := source()
		if !handles {
			return live
		}
		return NewRegistry(append(slices.Clone(live.tools), artifacts.FetchTool())...)
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
	row := Row{ID: origin, Schema: SchemaVersion, At: start, Task: config.Task, Wire: config.Wire, Spend: config.Spend, Root: origin, Account: account.ID, SpawnedFrom: config.SpawnedFrom, Budget: budget}

	var recorder *session.Recorder
	if config.Sessions != nil {
		if recorder, err = config.Sessions.Begin(row.Header(), row.author()); err != nil {
			return Row{}, err
		}
	}
	var recordErrs []string
	note := func(err error) {
		if err != nil {
			recordErrs = append(recordErrs, err.Error())
		}
	}

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
	latest := make(chan StepRow, konst.TurnMaxSteps)
	var stepping sync.WaitGroup
	defer func() {
		close(latest)
		stepping.Wait()
	}()
	if config.Step != nil {
		stepping.Add(1)
		go func() {
			defer stepping.Done()
			for step := range latest {
				config.Step(step)
			}
		}()
	}
	sent := 0
	flush := func() {
		if recorder == nil {
			return
		}
		for _, message := range messages[min(sent, len(messages)):] {
			if message.Role == llm.RoleSystem {
				continue
			}
			note(recorder.Append(session.EventMessage, messageRowOf(message)))
		}
		sent = max(sent, len(messages))
	}
	keep := func(step StepRow) {
		flush()
		note(recorder.AppendAttempt(session.EventStep, step.id, step.attempt, step))
		row.Steps = append(row.Steps, step)
		select {
		case latest <- step:
			return
		default:
		}
		select {
		case <-latest:
		default:
		}
		latest <- step
	}
	finish := func(outcome Outcome) Row {
		row.Outcome = outcome
		row.Conversation = messages[afterSystem:]
		flush()
		written.Wait()
		for _, failed := range forkWriteErrs {
			row.Warnings = append(row.Warnings, "the session this one forked from was not written: "+failed)
		}
		for _, tool := range currentTools().tools {
			if spawner, spawning := tool.(*SpawnTool); spawning {
				for _, child := range spawner.children {
					row.ChildIDs = append(row.ChildIDs, child.ID)
				}
				row.TotalCostUSD += spawner.spend
			}
		}
		row.WallClockMS = now().Sub(start).Milliseconds()
		for _, failed := range recordErrs {
			row.Warnings = append(row.Warnings, "part of this turn was not recorded as it ran: "+failed)
		}
		if err := recorder.End(row.Header(), row.Summary()); err != nil {
			row.Warnings = append(row.Warnings, "the turn's own record was not closed: "+err.Error())
		}
		return row
	}
	endAt := func(outcome Outcome, lead string, step int, history []llm.Message) Row {
		messages = history
		if config.NoLastWord || step == 1 {
			return finish(outcome)
		}
		messages = append(slices.Clone(history), llm.Message{Role: llm.RoleUser, Content: lead + andThisIsItsLastStep})
		decision, attempt, err := askCountingAttempts(ctx, model, llm.Request{Messages: messages, Tools: currentTools().Definitions()})
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
		messages = append(messages, llm.Message{Role: llm.RoleAssistant, Content: decision.Content})
		keep(stepFrom(len(row.Steps)+1, attempt, decision))
		return finish(outcome)
	}
	guard := newLoopGuard(config.Caps)
	forks, recordedGrants := 0, 0
	for step := 1; ; step++ {
		if config.Steering != nil {
			for _, steered := range config.Steering() {
				messages = append(messages, llm.Message{Role: llm.RoleUser, Content: steered})
			}
		}
		if outcome, capped := config.Caps.exceeded(step); capped {
			lead := "this turn reached its " + outcome.String() + " of " + strconv.Itoa(config.Caps.MaxSteps) + ", and the work is not finished"
			return endAt(outcome, lead, step, messages), nil
		}

		stepTools := currentTools()
		asSent := recall.Measure(artifacts.preview, budget.Bands, historyOf(messages))
		decision, attempt, err := askCountingAttempts(ctx, model, llm.Request{Messages: messages, Tools: stepTools.Definitions()})
		if err != nil {
			return finish(OutcomeError), err
		}
		row.Model = decision.Build
		row.TotalCostUSD += decision.Usage.Cost

		stepRow := stepFrom(step, attempt, decision)
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
				stepRow.ToolCalls = append(stepRow.ToolCalls, callRow)
				messages = append(messages, resultMessage)
			}
			keep(stepRow)

		case llm.OutcomeToolCalls:
			messages = append(messages, llm.Message{Role: llm.RoleAssistant, ToolCalls: decision.ToolCalls, Thinking: decision.Thinking})
			pending, batches := decision.ToolCalls, 0
			var tripped bool
			var repeated ToolCallRow
			var repeats int
			author := row.author()
			for len(pending) > 0 && !tripped {
				width := min(max(stepTools.parallelPrefix(pending), 1), konst.TurnParallelToolCalls)
				wave := make([]gatedCall, 0, width)
				for _, asked := range pending[:width] {
					call := asked
					proxied, proxyRow := config.Proxy.rewrite(ctx, asked)
					call.Arguments = proxied
					request := GateRequest{TurnID: row.ID, Task: config.Task, Tool: call.Name, Args: call.Arguments}
					gated := gatedCall{call: call, asked: asked, proxy: proxyRow, id: session.EventIDFor(origin, call.ID), parent: stepRow.id, author: author, sift: sifter, thrift: thrifter, task: config.Task}
					if config.Gate != nil {
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
				messages = append(messages, answers...)
				flush()
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
			if tripped {
				cause := loopGuardCause(repeated, repeats, guard.window)
				stopped := "this turn stopped itself because " + cause
				row.Guard = &LoopGuardStop{Tool: repeated.Tool, Args: repeated.Args, Repeats: repeats}
				row.Warnings = append(row.Warnings, "the turn stopped itself: "+cause)
				for _, unanswered := range pending {
					callRow, resultMessage := rejectedCall(unanswered, time.Now(),
						"tool call "+strconv.Quote(unanswered.Name)+" was not executed: "+stopped,
						session.EventIDFor(origin, unanswered.ID), stepRow.id, author)
					stepRow.ToolCalls = append(stepRow.ToolCalls, callRow)
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
					note(recorder.End(ended.Header(), ended.Summary()))
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
						Budget:      budget,
					}
					if moving {
						row.Account = moved.ID
						row.Warnings = append(row.Warnings, movedAccountWords(account, moved, fork.TokensAfter))
						account = moved
						if moved.Model != nil {
							model = moved.Model
						}
					}
					messages, sent = begun, 0
					if recorder != nil {
						next, beginErr := config.Sessions.Begin(row.Header(), row.author())
						note(beginErr)
						recorder = next
					}
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

func NewID(at time.Time) string {
	return session.IDPrefix + strconv.FormatInt(at.UnixNano(), 16)
}

func stepFrom(index, attempt int, decision llm.Decision) StepRow {
	return StepRow{
		id:               session.NewEventID(),
		attempt:          attempt,
		Index:            index,
		AssistantText:    decision.Content,
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
	return row, answer
}

func (g gatedCall) execute(ctx context.Context, tools Registry, resultBytesCap int, artifacts Artifacts) (ToolCallRow, llm.Message) {
	call := g.call
	started := time.Now()
	tool, ok := tools.byName[call.Name]
	if !ok {
		return rejectedCall(call, started, "unknown tool "+strconv.Quote(call.Name), g.id, g.parent, g.author)
	}

	spawner, spawning := tool.(*SpawnTool)
	spawnedBefore := 0
	if spawning {
		spawnedBefore = len(spawner.children)
	}

	result, err := tool.Run(ctx, call.Arguments)
	if err == nil && g.proxy != nil && g.proxy.Ran != "" && proxyPanicked(result.Content) {
		g.proxy.ProxyBytes = len(result.Content)
		g.proxy.Note = g.proxy.Proxy + " panicked instead of filtering the output, so the command ran as it was asked for"
		call = g.asked
		result, err = tool.Run(ctx, call.Arguments)
	}
	if err != nil {
		return rejectedCall(call, started, err.Error(), g.id, g.parent, g.author)
	}

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
	}
	if storeErr != nil {
		row.ResultHandleError = storeErr.Error()
	}
	if spawning && len(spawner.children) > spawnedBefore {
		row.ChildID = spawner.children[spawnedBefore].ID
	}
	body := rendered
	if body == "" {
		body = theToolSucceededAndPrintedNothing
		if row.Outcome() == llm.ToolOutcomeFailed {
			body = theToolFailedAndPrintedNothing
		}
	}
	return row, llm.Message{
		Role:            llm.RoleTool,
		ToolCallID:      call.ID,
		Content:         body,
		ToolOutcome:     row.Outcome(),
		ToolResultBytes: row.ResultBytes,
	}
}

func rejectedCall(call llm.ToolCall, started time.Time, reason, id, parent, author string) (ToolCallRow, llm.Message) {
	content := "error: " + reason
	row := ToolCallRow{ID: id, Parent: parent, Author: author, Tool: call.Name, Args: call.Arguments, Error: reason, DurationMS: time.Since(started).Milliseconds()}
	return row, llm.Message{
		Role:            llm.RoleTool,
		ToolCallID:      call.ID,
		Content:         content,
		ToolOutcome:     row.Outcome(),
		ToolResultBytes: len(content),
	}
}
