package turn

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"time"

	"boji/internal/llm"
	"boji/internal/transport"
)

type Model interface {
	Ask(ctx context.Context, request llm.Request) (llm.Decision, error)
}

type Caps struct {
	MaxSteps     int
	MaxCostUSD   float64
	MaxWallClock time.Duration
}

func (c Caps) exceeded(step int, spent float64, elapsed time.Duration) (Outcome, bool) {
	if c.MaxSteps > 0 && step > c.MaxSteps {
		return OutcomeStepCap, true
	}
	if c.MaxCostUSD > 0 && spent >= c.MaxCostUSD {
		return OutcomeCostCap, true
	}
	if c.MaxWallClock > 0 && elapsed >= c.MaxWallClock {
		return OutcomeWallClockCap, true
	}
	return OutcomeUnset, false
}

type Config struct {
	Model          Model
	Tools          Registry
	Task           string
	System         string
	Caps           Caps
	ResultBytesCap int
	Now            func() time.Time
	NewID          func() string
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
	now := config.Now
	if now == nil {
		now = time.Now
	}
	newID := config.NewID
	if newID == nil {
		newID = func() string { return "turn-" + strconv.FormatInt(now().UnixNano(), 16) }
	}

	start := now()
	row := Row{ID: newID(), At: start, Task: config.Task}
	messages := []llm.Message{{Role: llm.RoleUser, Content: config.Task}}
	if config.System != "" {
		messages = append([]llm.Message{{Role: llm.RoleSystem, Content: config.System}}, messages...)
	}

	for step := 1; ; step++ {
		elapsed := now().Sub(start)
		if outcome, capped := config.Caps.exceeded(step, row.TotalCostUSD, elapsed); capped {
			row.Outcome = outcome
			row.WallClockMS = elapsed.Milliseconds()
			return row, nil
		}

		decision, err := config.Model.Ask(ctx, llm.Request{Messages: messages, Tools: config.Tools.Definitions()})
		if err != nil {
			row.Outcome = OutcomeError
			row.WallClockMS = now().Sub(start).Milliseconds()
			return row, err
		}
		row.Model = decision.Build
		row.TotalCostUSD += decision.Usage.Cost

		stepRow := StepRow{
			Index:            step,
			AssistantText:    decision.Content,
			PromptTokens:     decision.Usage.InputTokens,
			CompletionTokens: decision.Usage.OutputTokens,
			CostUSD:          decision.Usage.Cost,
		}

		switch decision.Outcome {
		case llm.OutcomeMessage:
			row.Steps = append(row.Steps, stepRow)
			row.Outcome = OutcomeStopped
			row.WallClockMS = now().Sub(start).Milliseconds()
			return row, nil

		case llm.OutcomeToolCalls:
			messages = append(messages, llm.Message{Role: llm.RoleAssistant, ToolCalls: decision.ToolCalls})
			for _, call := range decision.ToolCalls {
				callRow, resultMessage := runToolCall(ctx, config.Tools, call, config.ResultBytesCap)
				stepRow.ToolCalls = append(stepRow.ToolCalls, callRow)
				messages = append(messages, resultMessage)
			}
			row.Steps = append(row.Steps, stepRow)

		case llm.OutcomeRefusal:
			row.Steps = append(row.Steps, stepRow)
			row.Outcome = OutcomeError
			row.WallClockMS = now().Sub(start).Milliseconds()
			return row, transport.Fail("turn.Run", transport.KindProvider, nil, "the model refused: %s", decision.Refusal)

		default:
			panic("turn: unknown model outcome")
		}
	}
}

func runToolCall(ctx context.Context, tools Registry, call llm.ToolCall, resultBytesCap int) (ToolCallRow, llm.Message) {
	started := time.Now()
	tool, ok := tools.lookup(call.Name)
	if !ok {
		return rejectedCall(call, started, "unknown tool "+strconv.Quote(call.Name))
	}

	result, err := tool.Run(ctx, call.Arguments)
	if err != nil {
		return rejectedCall(call, started, err.Error())
	}

	rendered := result.Content
	if len(rendered) > resultBytesCap {
		head := resultBytesCap / 2
		rendered = rendered[:head] + "\n...(truncated)...\n" + rendered[len(rendered)-(resultBytesCap-head):]
	}
	sum := sha256.Sum256([]byte(result.Content))
	row := ToolCallRow{
		Tool:          call.Name,
		Args:          call.Arguments,
		Command:       result.Command,
		ExitCode:      result.ExitCode,
		ResultBytes:   len(result.Content),
		RenderedBytes: len(rendered),
		ResultHash:    hex.EncodeToString(sum[:]),
		DurationMS:    time.Since(started).Milliseconds(),
	}
	return row, llm.Message{Role: llm.RoleTool, ToolCallID: call.ID, Content: rendered}
}

func rejectedCall(call llm.ToolCall, started time.Time, reason string) (ToolCallRow, llm.Message) {
	row := ToolCallRow{Tool: call.Name, Args: call.Arguments, Error: reason, DurationMS: time.Since(started).Milliseconds()}
	return row, llm.Message{Role: llm.RoleTool, ToolCallID: call.ID, Content: "error: " + reason}
}
