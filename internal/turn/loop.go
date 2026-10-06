package turn

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
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
	"tofu/internal/konst"
	"tofu/internal/llm"
	"tofu/internal/recall"
	"tofu/internal/rule"
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

const RuleOverrideToolName = "rule_override"

func gateExempt(tool string) bool {
	return tool == "browser_tabs" || tool == "browser_read" || tool == "browser_observe" || tool == "artifact_fetch" || tool == referenceToolName || tool == RuleOverrideToolName
}

type Caps struct {
	MaxSteps         int
	MaxForks         int
	LoopGuardRepeats int
	LoopGuardWindow  int
	WallClock        time.Duration
}

const theResponseHitTheOutputTokenLimit = "the response hit the output token limit, so its arguments may be truncated. " +
	"Re-issue the tool call with complete arguments."

const theToolSucceededAndPrintedNothing = "the tool ran, succeeded and printed nothing."

const theToolFailedAndPrintedNothing = "the tool ran, failed and printed nothing."

const theToolWasAbortedAndPrintedNothing = "this call was cancelled elsewhere in the turn before it produced output. " +
	"it is not a failure of the command itself and does not need to be retried the same way."

const aRepeatRunsAgainNothing = "cached: the same call earlier in this turn, and nothing written since, so it was not run again\n"

func pointAtCopy(answer llm.Message, held ...[]llm.Message) string {
	for _, messages := range held {
		for _, message := range messages {
			if message.Role == llm.RoleTool && (message.Content == answer.Content || message.Content == aRepeatRunsAgainNothing+answer.Content) {
				return "unchanged: the same call as " + message.ToolCallID + " earlier in this turn, with nothing written since, so its result above is this result word for word"
			}
		}
	}
	return aRepeatRunsAgainNothing + answer.Content
}

const theTurnEndsOnACleanReport = "this turn ends on a clean sub-agent report, so it runs no tool"

const andThisIsItsLastStep = ", and this is its last step: answer now from what you already have, " +
	"saying what you did, what is left undone, and what to do next. no tool runs on this step."

const theLastStepRunsNoTool = "this was the turn's last step, which runs no tool"

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
	AgentType       string
	Project         string
	SessionSource   string
	System          string
	References      map[string]string
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
	Inbox           *Inbox
	Step            CalledAsTheStepIsRecordedAndBeforeTheNextOneIsAsked
	ToolResult      CalledAsEachToolCallAnswersAndBeforeTheNextRequest
	EndedSession    func(Row) error
	Notify          func(string)
	Now             func() time.Time
	NewID           func() string
}

func (c Config) FirstUserMessage() string {
	if c.Environment == "" {
		return c.Task
	}
	return c.Environment + "\n\n" + c.Task
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
				added = append(added, messageTool{orchestrator: spawner}, subAgentsTool{orchestrator: spawner})
			}
		}
		if handles {
			added = append(added, artifacts.FetchTool())
		}
		if config.SpawnedFrom != "" && strings.Contains(config.System, referencesOnDemand) {
			added = append(added, referenceTool{held: config.References})
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
		return verdict
	}

	messages := make([]llm.Message, 0, len(config.History)+2)
	if config.System != "" {
		messages = append(messages, llm.Message{Role: llm.RoleSystem, Content: config.System})
	}
	afterSystem := len(messages)
	messages = append(messages, config.History...)
	first, concluding, taken := config.FirstUserMessage(), "", ""
	for _, tool := range config.Tools.tools {
		if spawner, spawning := tool.(*SpawnTool); spawning {
			concluding = spawner.cleanReport(config.Task)
			if concluding == "" && !spawner.ChecksWork {
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
	if note := strings.TrimSpace(concluding + taken + "\n\n" + hookContext); note != "" {
		first += "\n\n" + note
	}
	messages = append(messages, llm.Message{Role: llm.RoleUser, Content: first, Images: config.Images})

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
	endAt := func(outcome Outcome, lead string, history []llm.Message) Row {
		messages = history
		if config.NoLastWord {
			return finish(outcome)
		}
		messages = append(slices.Clone(history), llm.Message{Role: llm.RoleUser, Content: lead + andThisIsItsLastStep})
		request := llm.Request{Messages: messages, Tools: currentTools().Definitions()}
		if len(request.Tools) > 0 {
			request.ToolChoice = llm.ToolChoiceNone
		}
		decision, timing, err := askCountingAttempts(ctx, model, request)
		row.TotalCostUSD += decision.Usage.Cost
		if err != nil {
			row.Warnings = append(row.Warnings, lead+", and the last answer was not obtained: "+err.Error())
			return finish(outcome)
		}
		row.Model = decision.Build
		last := stepFrom(len(row.Steps)+1, timing, decision)
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
		return finish(OutcomeError), errors.New("a UserPromptSubmit hook refused this prompt: " + promptRefused)
	}
	guard := newLoopGuard(config.Caps)
	forks, recordedGrants, stopContinuations := 0, 0, 0
	askedAgainAfterBlank := false
	noticeStep := config.Caps.MaxSteps - max(1, int(math.Ceil(float64(config.Caps.MaxSteps)*konst.TurnStepCapNoticeShare)))
	for step := 1; ; step++ {
		if err := ctx.Err(); err != nil {
			return finish(OutcomeError), err
		}
		if config.Steering != nil {
			for _, steered := range config.Steering() {
				messages = append(messages, llm.Message{Role: llm.RoleUser, Content: steered})
			}
		}
		if config.Inbox != nil {
			for _, item := range config.Inbox.Take() {
				messages = append(messages, llm.Message{Role: llm.RoleUser, Content: item})
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
			messages = append(messages, llm.Message{Role: llm.RoleUser, Content: "this turn has " + strconv.Itoa(config.Caps.MaxSteps-step+1) +
				" steps left before its step cap of " + strconv.Itoa(config.Caps.MaxSteps) + ", and the last of them runs no tool: " +
				"finish what you are doing, or stop and report what is done and what is left."})
		}

		stepTools := currentTools()
		definitions := stepTools.Definitions()
		schemas, err := json.Marshal(definitions)
		if err != nil {
			return finish(OutcomeError), err
		}
		budget = budget.Sending(artifacts.preview, string(schemas))
		asSent := recall.Measure(artifacts.preview, budget.Bands, historyOf(messages))
		decision, timing, err := askCountingAttempts(ctx, model, llm.Request{Messages: messages, Tools: definitions})
		if overflowed(err) {
			shrink, shrinkErr := shrinkOverflow(artifacts, messages, asSent.Total(), budget.WindowTokens)
			switch {
			case shrinkErr != nil:
				err = shrinkErr
			case shrink.results == 0:
				err = fmt.Errorf("the context window overflowed and no old tool result was left to shrink, so the turn ends: %w", err)
			default:
				told := "the context window overflowed, so tofu " + shrink.String() + ", and asked once more"
				row.Warnings = append(row.Warnings, told)
				if config.Notify != nil {
					config.Notify(told)
				}
				asSent = recall.Measure(artifacts.preview, budget.Bands, historyOf(messages))
				decision, timing, err = askCountingAttempts(ctx, model, llm.Request{Messages: messages, Tools: definitions})
				if overflowed(err) {
					err = fmt.Errorf("the context window overflowed again after tofu %s, so the turn ends: %w", shrink, err)
				}
			}
		}
		if err != nil {
			return finish(OutcomeError), err
		}
		row.Model = decision.Build
		row.TotalCostUSD += decision.Usage.Cost
		reported := decision.PromptAccounting.BilledTokens(decision.Usage.InputTokens, decision.CacheReadTokens) + decision.CacheWriteTokens
		budget = budget.Reported(reported, asSent)

		stepRow := stepFrom(step, timing, decision)
		answering = stepRow.id
		measuredAgainst := budget.Bands
		stepRow.Occupancy, stepRow.Bands = &asSent, &measuredAgainst

		if decision.Outcome == llm.OutcomeMessage && strings.TrimSpace(decision.Content) == "" {
			if askedAgainAfterBlank {
				keep(stepRow)
				return finish(OutcomeError), transport.Fail("turn.Run", transport.KindInvalidAnswer, nil,
					"the model answered twice in a row with no text and no tool call")
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
				messages = append(messages, llm.Message{Role: llm.RoleUser, Content: "a " + string(stop) + " hook kept this turn going: " + blocked})
				keep(stepRow)
				continue
			}
			if blocked != "" {
				row.Warnings = append(row.Warnings, "the turn ended with a "+string(stop)+" hook still blocking after "+strconv.Itoa(stopContinuations)+" continuations: "+blocked)
			}
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
					request := GateRequest{TurnID: row.ID, Task: config.Task, Tool: call.Name, Args: call.Arguments}
					gated := gatedCall{call: call, asked: asked, proxy: proxyRow, id: session.EventIDFor(origin, call.ID), parent: stepRow.id, author: author, sift: sifter, thrift: thrifter, redact: redactor, task: config.Task, site: recorded.site(call.ID, messages), model: model, fire: fire}
					if gated.refusal = hookRefusal(ctx, config, request, pre); gated.refusal != "" {
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
						gated.refusal = gateRefusal(ctx, config.Person, request, gated.verdict, gated.gateErr)
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
						answers[i].Content = pointAtCopy(answers[i], messages, answers[:i])
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
				return finish(OutcomeError), err
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
			if started := startedSpawnsOnly(stepTools, stepRow.ToolCalls); config.SpawnedFrom == "" && len(started) > 0 {
				if strings.TrimSpace(decision.Content) == "" {
					messages = append(messages, llm.Message{Role: llm.RoleAssistant, Content: strings.Join(started, "\n")})
				}
				keep(stepRow)
				return finish(OutcomeStopped), nil
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
				fork, begun, err := forkHistory(artifacts, budget, config.FirstUserMessage(), messages, forced, forks+1, config.Caps.MaxForks)
				if err == nil {
					err = ctx.Err()
				}
				if err != nil {
					keep(stepRow)
					return finish(OutcomeError), err
				}
				if fork != nil && config.Caps.MaxForks > 0 && forks >= config.Caps.MaxForks {
					keep(stepRow)
					lead := "this turn reached its cap of " + strconv.Itoa(config.Caps.MaxForks) + " forks, and the work is not finished"
					return endAt(OutcomeStepCap, lead, messages), nil
				}
				if fork != nil {
					forks++
					fork.Step, fork.Into = step, origin+"-f"+strconv.Itoa(forks+1)
					stepRow.Fork = fork
					keep(stepRow)
					ended := row
					ended.Outcome, ended.ForkedInto, ended.Conversation = OutcomeForked, fork.Into, messages[afterSystem:]
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

func (t *SpawnTool) reportTaken(task string) string {
	agents := t.roster.SubAgents()
	at := slices.IndexFunc(agents, func(agent subagent.SubAgent) bool { return agent.State == subagent.Finished && agent.Report == task })
	if at < 0 {
		return ""
	}
	return "the person has not asked tofu to check a sub-agent's work, so take " + agents[at].ID + "'s report as the result: " +
		"do not read its files again or rerun its build, tests or type check, unless the report says something failed."
}

func (t *SpawnTool) cleanReport(task string) string {
	spawned := t.Spawned()
	if len(spawned) != 1 {
		return ""
	}
	id := spawned[0].ID
	agents := t.roster.SubAgents()
	at := slices.IndexFunc(agents, func(agent subagent.SubAgent) bool { return agent.ID == id })
	definition, err := t.SubAgents.Named(spawned[0].Agent)
	if at < 0 || agents[at].State != subagent.Finished || agents[at].Report != task || err != nil || definition.Language == "" || len(definition.Gate) == 0 {
		return ""
	}
	var rows []Row
	unseen := false
	t.tree.mu.Lock()
	for _, row := range t.tree.rows {
		if row.ID == id {
			rows = append(rows, row)
		}
		unseen = unseen || strings.HasPrefix(row.ID, id+"-f") || (row.ID == id && len(row.Steps) == 0)
	}
	t.tree.mu.Unlock()
	if unseen || len(rows) == 0 || completionOf(subagent.Finished, findings(wholeRun(rows), subagent.Finished)) != subagent.Done ||
		len(gateMissed(t.Project, projectRecipes(t.Project), definition, agents[at].Owns, rows)) > 0 {
		return ""
	}
	edited := false
	for _, row := range rows {
		for _, step := range row.Steps {
			for _, call := range step.ToolCalls {
				path := writtenPath(call)
				if slices.Contains([]string{".vue", ".svelte", ".tsx", ".jsx", ".html", ".css", ".scss"}, strings.ToLower(filepath.Ext(path))) {
					return ""
				}
				edited = edited || (path != "" && rule.LanguageOf(path) == definition.Language)
			}
		}
	}
	if !edited {
		return ""
	}
	return id + "'s report is clean, as tofu read it from the run: its completion is done, " + strings.Join(definition.Gate, " and ") +
		" ran after its last edit and passed, it is the only sub-agent this request delegated to, and it changed no file a browser shows. " +
		"so this turn checks nothing again: answer the person from the report in a few lines, and call no tool."
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
		ReasoningTokens:  decision.Usage.ReasoningTokens,
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
	shadow  <-chan shadowVerdict
	refusal string
	fire    func(hook.Input) hook.Verdict
}

type shadowVerdict struct {
	decision GateDecision
	err      string
}

func (g gatedCall) run(ctx context.Context, tools Registry, resultBytesCap int, artifacts Artifacts, batch int) (ToolCallRow, llm.Message, bool) {
	row, answer := rejectedCall(g.call, time.Now(), g.refusal, g.id, g.parent, g.author)
	repeat := false
	if g.refusal == "" {
		row, answer, repeat = g.execute(ctx, tools, resultBytesCap, artifacts)
	}
	verdict, gateErr := g.verdict, g.gateErr
	select {
	case judged := <-g.shadow:
		verdict, gateErr = judged.decision, judged.err
	default:
	}
	row.GateDecisionID, row.GateVerdict, row.GateError = verdict.ID, string(verdict.Verdict), gateErr
	row.ParallelBatch = batch
	row.Proxy = g.proxy
	row.Command = g.redact.Redact(row.Command)
	if len(row.Args) > 0 {
		row.Args = json.RawMessage(g.redact.Redact(string(row.Args)))
	}
	return row, answer, repeat
}

func (g gatedCall) execute(ctx context.Context, tools Registry, resultBytesCap int, artifacts Artifacts) (ToolCallRow, llm.Message, bool) {
	call := g.call
	started := time.Now()
	ctx = context.WithValue(context.WithValue(context.WithValue(ctx, shellOwnerKey{}, g.author), spawnSiteKey{}, g.site), runningModelKey{}, g.model)
	tool, ok := tools.byName[call.Name]
	if !ok {
		row, answer := rejectedCall(call, started, "unknown tool "+strconv.Quote(call.Name), g.id, g.parent, g.author)
		return row, answer, false
	}

	result, err := tool.Run(ctx, call.Arguments)
	if err == nil && g.proxy != nil && g.proxy.Ran != "" && proxyPanicked(result.Content) {
		g.proxy.ProxyBytes = len(result.Content)
		g.proxy.Note = g.proxy.Proxy + " panicked instead of filtering the output, so the command ran as it was asked for"
		call = g.asked
		result, err = tool.Run(ctx, call.Arguments)
	}
	if err != nil {
		row, answer := rejectedCall(call, started, g.redact.Redact(err.Error()), g.id, g.parent, g.author)
		return row, answer, false
	}
	result.Content, result.FailureText = g.redact.Redact(result.Content), g.redact.Redact(result.FailureText)
	var post hook.Verdict
	if !result.Repeat {
		post = g.fire(hook.Input{Event: hook.PostToolUse, Tool: call.Name, Args: call.Arguments, CallID: call.ID, Response: result.Content, Failed: result.FailureText != ""})
	}

	cut := g.cutShellResult(ctx, call.Name, result)
	thriftCut := g.cutThriftResult(ctx, call.Name, result)
	saved := cut.Saved
	text := cut.Text
	if thriftCut.Saved > 0 {
		text = thriftCut.Text
		saved += thriftCut.Saved
	}
	rendered, handle, storeErr := artifacts.Render(call.Name, call.Arguments, text, resultBytesCap)
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
	if post.Block != "" {
		body += "\n\na PostToolUse hook says: " + post.Block
	}
	if post.Context != "" {
		body += "\n\n" + post.Context
	}
	return row, llm.Message{
		Role:            llm.RoleTool,
		ToolCallID:      call.ID,
		Content:         body,
		ToolOutcome:     outcome,
		ToolResultBytes: row.ResultBytes,
	}, result.Repeat
}

type hookEngineKey struct{}

func EndSession(ctx context.Context, project, session, reason string) []string {
	verdict := hook.Load(project, shell.Choice{}).Fire(ctx, hook.Input{Event: hook.SessionEnd, Session: session, Reason: reason})
	return verdict.Warnings
}

func hookEngine(ctx context.Context, config Config, turnID string) (*hook.Engine, []string) {
	if held, inherited := ctx.Value(hookEngineKey{}).(*hook.Engine); inherited {
		return held, nil
	}
	root, choice := config.Project, shell.Choice{}
	if bash, found := config.Tools.byName[bashToolName].(*BashTool); found {
		root, choice = cmp.Or(root, string(bash.root)), bash.choice
	}
	root, _ = filepath.Abs(cmp.Or(root, "."))
	engine := hook.Load(root, choice)
	warnings, untrusted := engine.Problems(), engine.Untrusted()
	if len(untrusted) == 0 {
		return engine, warnings
	}
	listed := make([]string, len(untrusted))
	for i, unknown := range untrusted {
		file, _ := filepath.Rel(root, unknown.File)
		listed[i] = string(unknown.Event) + " " + cmp.Or(unknown.Matcher, "*") + ": " + unknown.Command + " (" + string(unknown.Trust) + ", " + file + ")"
	}
	told := config.Notify
	if told == nil {
		told = func(said string) { warnings = append(warnings, said) }
	}
	if config.Person == nil {
		told(strconv.Itoa(len(untrusted)) + " project hooks are not trusted, so they did not run: " + strings.Join(listed, "; ") +
			". run tofu hooks trust in the project to trust them")
		return engine, warnings
	}
	told("this project has hooks tofu has not run, and each runs a command on this machine:\n" + strings.Join(listed, "\n") +
		"\n1 runs them in this turn only, 2 refuses them until they change, 3 trusts them until they change")
	asked, _ := json.Marshal(map[string]string{"command": strings.Join(listed, "\n")})
	answer, err := config.Person(ctx, GateRequest{TurnID: turnID, Task: config.Task, Tool: "hooks", Args: asked}, GateDecision{})
	switch {
	case err != nil:
		return engine, append(warnings, "the person could not be asked about this project's hooks, so they did not run: "+err.Error())
	case answer == PersonAllowedOnce:
		err = engine.Answer(untrusted, hook.AnswerOnce)
	case answer == PersonAlwaysHere:
		err = engine.Answer(untrusted, hook.AnswerAlways)
	default:
		err = engine.Answer(untrusted, hook.AnswerRefuse)
	}
	if err != nil {
		warnings = append(warnings, "the answer about this project's hooks was not saved, so it is asked again next turn: "+err.Error())
	}
	return engine, warnings
}

func hookRefusal(ctx context.Context, config Config, request GateRequest, pre hook.Verdict) string {
	switch {
	case pre.Block != "":
		return "this call did not run: a PreToolUse hook refused it: " + pre.Block
	case pre.Ask == "":
		return ""
	case config.Person == nil:
		return "this call did not run: a PreToolUse hook asks the person first, and no person was available to answer: " + pre.Ask
	}
	if config.Notify != nil {
		config.Notify("a PreToolUse hook asks you before " + request.Tool + " runs: " + pre.Ask)
	}
	answer, err := config.Person(ctx, request, GateDecision{})
	switch {
	case err != nil:
		return "this call did not run: a PreToolUse hook asks the person first, and the person could not be asked: " + err.Error()
	case answer.allows():
		return ""
	}
	return "this call did not run: a PreToolUse hook asked the person, and the person did not allow it: " + pre.Ask
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
