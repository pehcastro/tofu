package wiring

import (
	"cmp"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"tofu/internal/agent/context/recall"
	"tofu/internal/agent/loop"
	"tofu/internal/host"
	"tofu/internal/konst"
	"tofu/internal/models/classifiers/core"
	"tofu/internal/models/llm/catalog"
	llm "tofu/internal/models/llm/core"
	"tofu/internal/models/llm/wire/anthropic"
	"tofu/internal/models/llm/wire/codex"
	"tofu/internal/models/llm/wire/openai"
	"tofu/internal/models/transport"
	"tofu/internal/platform/credential"
	settingspkg "tofu/internal/store/settings"
)

const (
	VerifySubAgentsRule        = "verify_sub_agents"
	OpenRouterEndpointVariable = "TOFU_OPENROUTER_ENDPOINT"
)

func WireEfforts(wire string) []llm.Effort {
	switch wire {
	case WireSubscription:
		return anthropic.ReasoningEfforts()
	case WireCodex:
		return llm.Efforts()
	}
	return nil
}

func WireSpend(wire string) loop.Spend {
	if wire == WireKey || wire == WireMeta {
		return loop.SpendAPIKey
	}
	return loop.SpendSubscription
}

func ChooseModel(opts RunOpts) (catalog.Model, error) {
	switch opts.Wire {
	case "":
		return catalog.Model{}, catalog.RoleOrchestrator.Unset()
	case WireKey:
		if opts.Model == "" {
			return catalog.Model{}, errors.New("--wire openrouter names no model: pass --model, or bind the orchestrator to an openrouter model")
		}
		library, err := ModelLibrary(opts.Dir)
		if err != nil {
			return catalog.Model{}, err
		}
		if listed, known := library.Resolve(opts.Model); known && listed.Provider == catalog.OpenRouter {
			return library.Select(listed.Slug())
		}
		return catalog.Model{ID: opts.Model}, nil
	case WireMeta:
		library, err := ModelLibrary(opts.Dir)
		if err != nil {
			return catalog.Model{}, err
		}
		if opts.Model == "" {
			return library.Default(catalog.Subscription(catalog.Meta))
		}
		return library.Select(opts.Model)
	}
	if opts.Model != "" {
		return SelectModel(opts.Wire, opts.Model)
	}
	bound, err := BoundRoles(opts.Wire, opts.Dir)
	if err != nil {
		return catalog.Model{}, err
	}
	orchestrator := bound[catalog.RoleOrchestrator]
	if orchestrator.Wire != opts.Wire {
		return catalog.Model{}, fmt.Errorf(
			"%s, and --wire %s reaches another subscription: run --wire %s instead, or bind the orchestrator role to a model --wire %s serves",
			orchestrator.Says(), opts.Wire, orchestrator.Wire, opts.Wire)
	}
	return orchestrator.Model, nil
}

func OnTheBoundWire(opts RunOpts) RunOpts {
	if opts.Wire != "" && opts.Model != "" {
		return opts
	}
	library, err := ModelLibrary(opts.Dir)
	if err != nil {
		return opts
	}
	if opts.Model != "" {
		if named, err := library.Select(opts.Model); err == nil {
			opts.Wire = library.WireOf(named)
		}
		return opts
	}
	if orchestrator, bound := library.Bound(catalog.RoleOrchestrator); bound && (opts.Wire == "" || opts.Wire == library.WireOf(orchestrator)) {
		opts.Wire, opts.Model = library.WireOf(orchestrator), orchestrator.Slug()
	}
	return opts
}

func ContextBudget(opts RunOpts, model catalog.Model) (recall.Budget, error) {
	registry, err := ModelRegistry()
	if err != nil {
		return recall.Budget{}, err
	}
	windowTokens, windowSource := catalog.WindowFor(model, registry, catalog.Served{})
	budget, err := recall.BudgetUnder(model.ID, windowTokens, settingspkg.IntSaying(opts.Dir, settingspkg.ContextCeiling, nil))
	if err != nil {
		return recall.Budget{}, err
	}
	budget.WindowSource = windowSource
	if opts.ContextCeiling <= 0 {
		return budget, nil
	}
	return budget.At(opts.ContextCeiling, fmt.Sprintf("--context-ceiling %d, which is read before %s and wins when both are set",
		opts.ContextCeiling, recall.CeilingVariable)), nil
}

func TurnTransport() transport.Config {
	return transport.Config{
		AttemptTimeout: time.Duration(konst.TurnAttemptTimeoutMillis) * time.Millisecond,
		Retries:        konst.TurnRetries,
		Backoff:        time.Duration(konst.TurnBackoffMillis) * time.Millisecond,
		MaxBackoff:     time.Duration(konst.TurnMaxBackoffMillis) * time.Millisecond,
		Growth:         konst.TurnBackoffGrowth,
		JitterFraction: konst.TurnBackoffJitterFraction,
		TotalWait:      time.Duration(konst.TurnTotalBackoffCeilingMillis) * time.Millisecond,
		Concurrency:    1,
	}
}

func KeyModel(model string) (loop.Model, error) {
	facts := KeyFactsOf(host.RoleLLM, OpenRouterName)
	key, err := core.KeyFor(credential.CredentialFileName, facts.Variable)
	if err != nil {
		return nil, fmt.Errorf("no %s %s key, run %s: %w", facts.Name, facts.Role, facts.Hint(), err)
	}
	wire, err := openai.New(openai.Config{
		Endpoint:  os.Getenv(OpenRouterEndpointVariable),
		Model:     model,
		Key:       key,
		KeyName:   facts.Variable,
		Transport: TurnTransport(),
	})
	if err != nil {
		return nil, err
	}
	return llm.NewClient(wire)
}

type CodexTurn struct {
	Wire             *codex.Wire
	Effort           llm.Effort
	Verbosity        catalog.Verbosity
	ReasoningContext catalog.ReasoningContext
}

func (c CodexTurn) Ask(ctx context.Context, request llm.Request) (llm.Decision, error) {
	var instructions []string
	messages := make([]llm.Message, 0, len(request.Messages))
	for _, message := range request.Messages {
		if message.Role == llm.RoleSystem {
			instructions = append(instructions, message.Content)
			continue
		}
		messages = append(messages, message)
	}

	result, _, err := c.Wire.Ask(ctx, codex.Request{
		Turn:             request.Turn,
		Instructions:     strings.Join(instructions, "\n\n"),
		Messages:         messages,
		Tools:            request.Tools,
		ToolChoice:       request.ToolChoice.OpenAIValue(),
		Effort:           c.Effort,
		SummaryOff:       request.OmitThinkingSummary,
		Verbosity:        string(c.Verbosity),
		ReasoningContext: string(c.ReasoningContext),
		OnThinking:       request.OnThinking,
		OnRetry:          request.OnRetry,
	})
	if err != nil {
		return llm.Decision{}, err
	}

	decision := llm.Decision{
		Build:            result.Model,
		RequestID:        result.ID,
		Outcome:          llm.OutcomeAfter(result.Stop, len(result.ToolCalls)),
		Stop:             result.StopReason,
		Content:          result.Content,
		Thinking:         llm.Thinking{Text: result.Thinking, Signature: codex.EncodeReasoning(result.ReasoningID, result.ReasoningEncrypted)},
		ToolCalls:        result.ToolCalls,
		Usage:            llm.Usage{InputTokens: result.Usage.Input, OutputTokens: result.Usage.Output, ReasoningTokens: result.Usage.Reasoning},
		PromptAccounting: llm.PromptAccountingFor(codex.Name),
		CacheReadTokens:  result.Usage.CacheRead,
		FirstTokenMS:     result.FirstTokenMS,
		Warnings:         result.Warnings,
	}
	if decision.Outcome == llm.OutcomeRefusal {
		decision.Refusal = cmp.Or(result.Refusal, result.StopReason)
	}
	return decision, nil
}

func SessionID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	raw[6] = raw[6]&0x0f | 0x40
	raw[8] = raw[8]&0x3f | 0x80
	text := hex.EncodeToString(raw[:])
	return text[:8] + "-" + text[8:12] + "-" + text[12:16] + "-" + text[16:20] + "-" + text[20:], nil
}

type SubscriptionModel struct{ Opts RunOpts }

func (s SubscriptionModel) Ask(ctx context.Context, request llm.Request) (llm.Decision, error) {
	opened, err := OpenWire(s.Opts)
	if opened.Held != nil {
		defer opened.Held.Close()
	}
	if err != nil {
		return llm.Decision{}, err
	}
	account, err := opened.Held.ForTurn().Pick(ctx)
	if err != nil {
		return llm.Decision{}, err
	}
	return account.Model.Ask(ctx, request)
}
