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

	"tofu/internal/crew"
	"tofu/internal/konst"
	"tofu/internal/llm"
	"tofu/internal/recall"
	"tofu/internal/session"
	"tofu/internal/sys"
	"tofu/internal/transport"
)

type Model interface {
	Ask(ctx context.Context, request llm.Request) (llm.Decision, error)
}

type Caps struct {
	MaxSteps     int
	MaxDecisions int
}

func (c Caps) exceeded(step int) (Outcome, bool) {
	if c.MaxSteps > 0 && step > c.MaxSteps {
		return OutcomeStepCap, true
	}
	return OutcomeUnset, false
}

const theResponseHitTheOutputTokenLimit = "the response hit the output token limit, so its arguments may be truncated. " +
	"Re-issue the tool call with complete arguments."

const andThisIsItsLastStep = " and this is its last step: answer now from what you already have, " +
	"saying what you did, what is left undone, and what to do next."

type Config struct {
	Model           Model
	Spend           Spend
	Tools           Registry
	Gate            Gate
	GateMode        GateMode
	Boundary        *crew.Boundary
	Person          Person
	Task            string
	History         []llm.Message
	Wire            string
	SpawnedFrom     string
	System          string
	Environment     string
	Caps            Caps
	ResultBytesCap  int
	ArtifactDir     string
	TruncateResults bool
	NoCompaction    bool
	NoFork          bool
	NoLastWord      bool
	Budget          recall.Budget
	Sessions        *session.Store
	Steering        func() []string
	Step            func(StepRow)
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
	if config.Model == nil {
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
	tools := config.Tools
	if handles {
		tools = NewRegistry(append(slices.Clone(tools.tools), artifacts.FetchTool())...)
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
	start := now()
	origin := newID()
	row := Row{ID: origin, Schema: SchemaVersion, At: start, Task: config.Task, Wire: config.Wire, Spend: config.Spend, Root: origin, SpawnedFrom: config.SpawnedFrom, Budget: budget}

	var recorder *session.Recorder
	if config.Sessions != nil {
		if recorder, err = config.Sessions.Begin(row.Header()); err != nil {
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
	messages = append(messages, llm.Message{Role: llm.RoleUser, Content: config.FirstUserMessage()})

	var written sync.WaitGroup
	var forkWrites sync.Mutex
	var forkWriteErrs []string
	latest := make(chan StepRow, konst.TurnMaxSteps)
	defer close(latest)
	if config.Step != nil {
		go func() {
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
		note(recorder.Append(session.EventStep, step))
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
		for _, tool := range tools.tools {
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
	endAt := func(outcome Outcome, step int, history []llm.Message) Row {
		messages = history
		if config.NoLastWord || step == 1 {
			return finish(outcome)
		}
		messages = append(slices.Clone(history),
			llm.Message{Role: llm.RoleUser, Content: "this turn reached its " + outcome.String() + andThisIsItsLastStep})
		decision, err := config.Model.Ask(ctx, llm.Request{Messages: messages})
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
			row.Warnings = append(row.Warnings, "the turn reached "+outcome.String()+" and the last answer was not obtained: "+reason)
			return finish(outcome)
		}
		row.Model = decision.Build
		messages = append(messages, llm.Message{Role: llm.RoleAssistant, Content: decision.Content})
		keep(stepFrom(len(row.Steps)+1, decision))
		return finish(outcome)
	}

	decisions, forks, recordedGrants := 0, 0, 0
	for step := 1; ; step++ {
		if config.Steering != nil {
			for _, steered := range config.Steering() {
				messages = append(messages, llm.Message{Role: llm.RoleUser, Content: steered})
			}
		}
		settled := messages
		if outcome, capped := config.Caps.exceeded(step); capped {
			return endAt(outcome, step, settled), nil
		}

		decision, err := config.Model.Ask(ctx, llm.Request{Messages: messages, Tools: tools.Definitions()})
		if err != nil {
			return finish(OutcomeError), err
		}
		row.Model = decision.Build
		row.TotalCostUSD += decision.Usage.Cost

		stepRow := stepFrom(step, decision)

		switch decision.Outcome {
		case llm.OutcomeMessage:
			messages = append(messages, llm.Message{Role: llm.RoleAssistant, Content: decision.Content})
			keep(stepRow)
			return finish(OutcomeStopped), nil

		case llm.OutcomeTruncated:
			if decision.Content != "" || len(decision.ToolCalls) > 0 {
				messages = append(messages, llm.Message{Role: llm.RoleAssistant, Content: decision.Content, ToolCalls: decision.ToolCalls})
			}
			if len(decision.ToolCalls) == 0 {
				keep(stepRow)
				return finish(OutcomeTruncated), nil
			}
			for _, call := range decision.ToolCalls {
				callRow, resultMessage := rejectedCall(call, time.Now(),
					"tool call "+strconv.Quote(call.Name)+" was not executed: "+theResponseHitTheOutputTokenLimit)
				stepRow.ToolCalls = append(stepRow.ToolCalls, callRow)
				messages = append(messages, resultMessage)
			}
			keep(stepRow)

		case llm.OutcomeToolCalls:
			messages = append(messages, llm.Message{Role: llm.RoleAssistant, ToolCalls: decision.ToolCalls})
			pending, batches, capped := decision.ToolCalls, 0, false
			for len(pending) > 0 && !capped {
				width := min(max(tools.parallelPrefix(pending), 1), konst.TurnParallelToolCalls)
				wave := make([]gatedCall, 0, width)
				for _, call := range pending[:width] {
					if config.Gate != nil && config.Caps.MaxDecisions > 0 && decisions >= config.Caps.MaxDecisions {
						capped = true
						break
					}
					request := GateRequest{TurnID: row.ID, Task: config.Task, Tool: call.Name, Args: call.Arguments}
					gated := gatedCall{call: call}
					if config.Gate != nil {
						decisions++
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
					rows[0], answers[0] = wave[0].run(ctx, tools, config.ResultBytesCap, artifacts, 0)
				} else {
					batches++
					var running sync.WaitGroup
					running.Add(len(wave))
					for i, gated := range wave {
						go func() {
							defer running.Done()
							rows[i], answers[i] = gated.run(ctx, tools, config.ResultBytesCap, artifacts, batches)
						}()
					}
					running.Wait()
				}
				stepRow.ToolCalls = append(stepRow.ToolCalls, rows...)
				messages = append(messages, answers...)
				flush()
				pending = pending[len(wave):]
			}
			if config.Boundary != nil {
				asked := config.Boundary.Asked()
				stepRow.Grants = slices.Clone(asked[recordedGrants:])
				recordedGrants = len(asked)
			}
			if capped {
				keep(stepRow)
				return endAt(OutcomeDecisionCap, step, settled), nil
			}
			if !config.NoFork {
				fork, begun, occupancy, err := forkHistory(artifacts, budget, config.FirstUserMessage(), messages)
				measuredAgainst := budget.Bands
				stepRow.Occupancy, stepRow.Bands = &occupancy, &measuredAgainst
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
						ForkedFrom:  ended.ID,
						ForkKind:    fork.Kind,
						SpawnedFrom: config.SpawnedFrom,
						Budget:      budget,
					}
					messages, sent = begun, 0
					if recorder != nil {
						next, beginErr := config.Sessions.Begin(row.Header())
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

func stepFrom(index int, decision llm.Decision) StepRow {
	return StepRow{
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
	verdict GateDecision
	gateErr string
	refusal string
}

func (g gatedCall) run(ctx context.Context, tools Registry, resultBytesCap int, artifacts Artifacts, batch int) (ToolCallRow, llm.Message) {
	row, answer := rejectedCall(g.call, time.Now(), g.refusal)
	if g.refusal == "" {
		row, answer = runToolCall(ctx, tools, g.call, resultBytesCap, artifacts)
	}
	row.GateDecisionID, row.GateVerdict, row.GateError = g.verdict.ID, string(g.verdict.Verdict), g.gateErr
	row.ParallelBatch = batch
	return row, answer
}

func runToolCall(ctx context.Context, tools Registry, call llm.ToolCall, resultBytesCap int, artifacts Artifacts) (ToolCallRow, llm.Message) {
	started := time.Now()
	tool, ok := tools.lookup(call.Name)
	if !ok {
		return rejectedCall(call, started, "unknown tool "+strconv.Quote(call.Name))
	}

	spawner, spawning := tool.(*SpawnTool)
	spawnedBefore := 0
	if spawning {
		spawnedBefore = len(spawner.children)
	}

	result, err := tool.Run(ctx, call.Arguments)
	if err != nil {
		return rejectedCall(call, started, err.Error())
	}

	rendered, handle, storeErr := artifacts.Render(result.Content, resultBytesCap)
	sum := sha256.Sum256([]byte(result.Content))
	row := ToolCallRow{
		Tool:          call.Name,
		Args:          call.Arguments,
		Command:       result.Command,
		ExitCode:      result.ExitCode,
		ResultBytes:   len(result.Content),
		RenderedBytes: len(rendered),
		ResultHash:    hex.EncodeToString(sum[:]),
		ResultHandle:  handle,
		DurationMS:    time.Since(started).Milliseconds(),
	}
	if storeErr != nil {
		row.ResultHandleError = storeErr.Error()
	}
	if spawning && len(spawner.children) > spawnedBefore {
		row.ChildID = spawner.children[spawnedBefore].ID
	}
	return row, llm.Message{
		Role:            llm.RoleTool,
		ToolCallID:      call.ID,
		Content:         rendered,
		ToolOutcome:     row.Outcome(),
		ToolResultBytes: row.ResultBytes,
	}
}

func rejectedCall(call llm.ToolCall, started time.Time, reason string) (ToolCallRow, llm.Message) {
	content := "error: " + reason
	row := ToolCallRow{Tool: call.Name, Args: call.Arguments, Error: reason, DurationMS: time.Since(started).Milliseconds()}
	return row, llm.Message{
		Role:            llm.RoleTool,
		ToolCallID:      call.ID,
		Content:         content,
		ToolOutcome:     row.Outcome(),
		ToolResultBytes: len(content),
	}
}
