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
	"time"

	"boji/internal/llm"
	"boji/internal/sys"
	"boji/internal/transport"
)

type Model interface {
	Ask(ctx context.Context, request llm.Request) (llm.Decision, error)
}

type Caps struct {
	MaxSteps     int
	MaxWallClock time.Duration
	MaxDecisions int
}

func (c Caps) exceeded(step int, elapsed time.Duration) (Outcome, bool) {
	if c.MaxSteps > 0 && step > c.MaxSteps {
		return OutcomeStepCap, true
	}
	if c.MaxWallClock > 0 && elapsed >= c.MaxWallClock {
		return OutcomeWallClockCap, true
	}
	return OutcomeUnset, false
}

type Config struct {
	Model           Model
	Spend           Spend
	Tools           Registry
	Gate            Gate
	Task            string
	System          string
	Caps            Caps
	ResultBytesCap  int
	ArtifactDir     string
	TruncateResults bool
	Now             func() time.Time
	NewID           func() string
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
		newID = func() string { return "turn-" + strconv.FormatInt(now().UnixNano(), 16) }
	}

	start := now()
	row := Row{ID: newID(), Schema: SchemaVersion, At: start, Task: config.Task, Spend: config.Spend}
	finish := func(outcome Outcome) Row {
		row.Outcome = outcome
		row.WallClockMS = now().Sub(start).Milliseconds()
		return row
	}
	messages := []llm.Message{{Role: llm.RoleUser, Content: config.Task}}
	if config.System != "" {
		messages = append([]llm.Message{{Role: llm.RoleSystem, Content: config.System}}, messages...)
	}

	decisions := 0
	for step := 1; ; step++ {
		if outcome, capped := config.Caps.exceeded(step, now().Sub(start)); capped {
			return finish(outcome), nil
		}

		decision, err := config.Model.Ask(ctx, llm.Request{Messages: messages, Tools: tools.Definitions()})
		if err != nil {
			return finish(OutcomeError), err
		}
		row.Model = decision.Build
		row.TotalCostUSD += decision.Usage.Cost

		stepRow := StepRow{
			Index:            step,
			AssistantText:    decision.Content,
			StopReason:       decision.Stop,
			PromptTokens:     decision.Usage.InputTokens,
			CompletionTokens: decision.Usage.OutputTokens,
			CacheReadTokens:  decision.CacheReadTokens,
			CacheWriteTokens: decision.CacheWriteTokens,
			CostUSD:          decision.Usage.Cost,
			Warnings:         decision.Warnings,
		}

		switch decision.Outcome {
		case llm.OutcomeMessage:
			row.Steps = append(row.Steps, stepRow)
			return finish(OutcomeStopped), nil

		case llm.OutcomeToolCalls:
			messages = append(messages, llm.Message{Role: llm.RoleAssistant, ToolCalls: decision.ToolCalls})
			for _, call := range decision.ToolCalls {
				var gated GateDecision
				var gateErr string
				if config.Gate != nil {
					if config.Caps.MaxDecisions > 0 && decisions >= config.Caps.MaxDecisions {
						row.Steps = append(row.Steps, stepRow)
						return finish(OutcomeDecisionCap), nil
					}
					decisions++
					var err error
					gated, err = config.Gate.Decide(ctx, GateRequest{TurnID: row.ID, Task: config.Task, Tool: call.Name, Args: call.Arguments})
					if err != nil {
						gateErr = err.Error()
					}
					if gated.ID != "" {
						row.DecisionIDs = append(row.DecisionIDs, gated.ID)
					}
				}
				callRow, resultMessage := runToolCall(ctx, tools, call, config.ResultBytesCap, artifacts)
				callRow.GateDecisionID, callRow.GateVerdict, callRow.GateError = gated.ID, gated.Verdict, gateErr
				stepRow.ToolCalls = append(stepRow.ToolCalls, callRow)
				messages = append(messages, resultMessage)
			}
			row.Steps = append(row.Steps, stepRow)

		case llm.OutcomeRefusal:
			row.Steps = append(row.Steps, stepRow)
			return finish(OutcomeError), transport.Fail("turn.Run", transport.KindProvider, nil, "the model refused: %s", decision.Refusal)

		default:
			panic("turn: unknown model outcome")
		}
	}
}

func runToolCall(ctx context.Context, tools Registry, call llm.ToolCall, resultBytesCap int, artifacts Artifacts) (ToolCallRow, llm.Message) {
	started := time.Now()
	tool, ok := tools.lookup(call.Name)
	if !ok {
		return rejectedCall(call, started, "unknown tool "+strconv.Quote(call.Name))
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
	return row, llm.Message{Role: llm.RoleTool, ToolCallID: call.ID, Content: rendered}
}

func rejectedCall(call llm.ToolCall, started time.Time, reason string) (ToolCallRow, llm.Message) {
	row := ToolCallRow{Tool: call.Name, Args: call.Arguments, Error: reason, DurationMS: time.Since(started).Milliseconds()}
	return row, llm.Message{Role: llm.RoleTool, ToolCallID: call.ID, Content: "error: " + reason}
}
