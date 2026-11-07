package forget

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"

	"tofu/internal/agent/context/recall"
	"tofu/internal/konst"
	"tofu/internal/models/llm/core"
	"tofu/internal/tools/contract"
	"tofu/internal/tools/files/read"
)

const Name = "forget"

type Window struct {
	held       []core.Message
	breakpoint int
	estimate   recall.Config
	mutex      sync.Mutex
	lines      map[string]string
}

type windowKey struct{}

func Hold(messages []core.Message, estimate recall.Config) *Window {
	breakpoint := len(messages) - 1
	for at := len(messages) - 2; at >= 0; at-- {
		if messages[at].Role == core.RoleAssistant {
			breakpoint = at
			break
		}
	}
	return &Window{held: messages, breakpoint: max(breakpoint, 0), estimate: estimate, lines: map[string]string{}}
}

func With(ctx context.Context, window *Window) context.Context {
	return context.WithValue(ctx, windowKey{}, window)
}

func (w *Window) Applied(messages []core.Message) []core.Message {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	if len(w.lines) == 0 {
		return nil
	}
	after := slices.Clone(messages)
	for at := w.breakpoint; at < len(after); at++ {
		if line, dropped := w.lines[after[at].ToolCallID]; dropped {
			after[at].Content, after[at].Images, after[at].ToolResultBytes = line, nil, len(line)
		}
	}
	return after
}

type Forget struct{}

func (Forget) Name() string { return Name }

func (Forget) Definition() core.Tool {
	return core.Tool{
		Name: Name,
		Description: "drops tool results you no longer need from your own context window, each left as one line naming the call, from your next request on. " +
			"calls are the call ids the context tool lists. only results after the last cache breakpoint can go, which are the results your last request showed you for the first time; " +
			"an older one is refused, since dropping it would rewrite the cached prefix. it says the tokens freed and what the cache pays for it.",
		Parameters: map[string]any{
			"type":       "object",
			"properties": map[string]any{"calls": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}},
			"required":   []string{"calls"},
		},
	}
}

type calledWith struct {
	Path    string `json:"path"`
	Command string `json:"command"`
	Pattern string `json:"pattern"`
}

func (w *Window) called(call string) (string, calledWith) {
	var args calledWith
	for _, message := range w.held {
		if at := slices.IndexFunc(message.ToolCalls, func(asked core.ToolCall) bool { return asked.ID == call }); at >= 0 {
			_ = json.Unmarshal(message.ToolCalls[at].Arguments, &args)
			return message.ToolCalls[at].Name, args
		}
	}
	return "", args
}

func (w *Window) tokens(messages []core.Message) int {
	total := 0
	for _, message := range messages {
		total += w.estimate.MessageTokens(message.Content)
	}
	return total
}

func (Forget) Run(ctx context.Context, raw json.RawMessage) (contract.Result, error) {
	var args struct {
		Calls []string `json:"calls"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return contract.Result{}, fmt.Errorf("forget: arguments are not the expected shape: %w", err)
	}
	if len(args.Calls) == 0 {
		return contract.Result{}, errors.New("forget: calls is empty; name the call ids to drop, as the context tool lists them")
	}
	w, inTurn := ctx.Value(windowKey{}).(*Window)
	if !inTurn {
		return contract.Result{}, errors.New("forget: no turn is running, so there is no window to drop from; the model calls forget inside a turn")
	}
	breakpoint := fmt.Sprintf("the last cache breakpoint, at message %d, the end of what the request before your last one sent", w.breakpoint)
	w.mutex.Lock()
	defer w.mutex.Unlock()
	lines := map[string]string{}
	var said strings.Builder
	var reads []string
	freed := 0
	for _, call := range args.Calls {
		at := slices.IndexFunc(w.held, func(message core.Message) bool { return message.Role == core.RoleTool && message.ToolCallID == call })
		switch {
		case at < 0:
			return contract.Result{}, fmt.Errorf("forget: %s is no tool result in your window; the context tool lists the call ids", call)
		case at < w.breakpoint:
			return contract.Result{}, fmt.Errorf("forget: %s is message %d, before %s; dropping it would rewrite the cached prefix, so nothing was dropped", call, at+1, breakpoint)
		}
		if w.lines[call] != "" || lines[call] != "" {
			continue
		}
		name, with := w.called(call)
		shown, _, _ := strings.Cut(strings.TrimSpace(cmp.Or(with.Path, with.Command, with.Pattern)), "\n")
		label := cmp.Or(strings.TrimSpace(name+" "+shown[:min(len(shown), konst.ContextLabelChars)]), call)
		if name == read.Name {
			reads = append(reads, with.Path)
		}
		size := w.estimate.Tokens(w.held[at].Content)
		lines[call] = fmt.Sprintf("forgotten at your request: %s, %d bytes, about %d tokens; run it again if you need it", label, len(w.held[at].Content), size)
		freed += size - w.estimate.Tokens(lines[call])
		fmt.Fprintf(&said, "  %s %s: %d tokens\n", call, label, size)
	}
	maps.Copy(w.lines, lines)
	for _, path := range reads {
		contract.ReadWindowOf(ctx).Dropped(path)
	}
	summary := fmt.Sprintf("dropped: %d results after %s\n%stokens_freed: %d\n"+
		"cache_cost: about %d tokens from message %d on are written to the cache again on your next request rather than read\n"+
		"kept_summary: each dropped result is one line naming its call; the next request reads about %d tokens of messages from the cache, the prefix up to the breakpoint, plus the tool definitions\n",
		len(lines), breakpoint, said.String(), freed, w.tokens(w.held[w.breakpoint:])-freed, w.breakpoint+1, w.tokens(w.held[:w.breakpoint]))
	return contract.Result{Content: summary, Command: strings.Join(args.Calls, " ")}, nil
}
