package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"

	"tofu/internal/konst"
	"tofu/internal/llm"
	"tofu/internal/recall"
	"tofu/internal/session"
	"tofu/internal/turn"
)

const (
	readDollarsPerMillion  = 0.20
	writeDollarsPerMillion = 8.00
)

type rule struct {
	name string
	gate turn.ShrinkGate
}

type tally struct {
	requests, cold, rewrites, overTarget int
	sent, read, rewritten                int
}

type agent struct {
	messages, sent []llm.Message
	calls          []llm.ToolCall
	results        []llm.Message
	target         int
	askedAt        time.Time
}

type replay struct {
	store   *recall.Store
	preview recall.Config
	ttl     time.Duration
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() (err error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	projects := flag.String("projects", filepath.Join(home, ".tofu", "projects"), "the recorded sessions, read only")
	flag.Parse()
	paths, err := filepath.Glob(filepath.Join(*projects, "*", "sessions", "*", "events.jsonl"))
	if err != nil {
		return err
	}
	held, err := os.MkdirTemp("", "tofu-shrink-replay-")
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, os.RemoveAll(held)) }()
	preview, err := recall.LoadConfig()
	if err != nil {
		return err
	}
	ttl, err := time.ParseDuration(konst.SubscriptionCacheTTL)
	if err != nil {
		return err
	}
	r := replay{store: recall.NewStore(held), preview: preview, ttl: ttl}
	rules := []rule{{"before", turn.ShrinkWhenFull}, {"after", turn.ShrinkWhenPaid}}
	tallies := make([]tally, len(rules))
	browsed := 0
	for _, path := range paths {
		events, err := browserEvents(path)
		if err != nil {
			return err
		}
		if events == nil {
			continue
		}
		browsed++
		for i, rule := range rules {
			if err := r.run(rule, events, &tallies[i]); err != nil {
				return fmt.Errorf("%s, rule %s: %w", path, rule.name, err)
			}
		}
	}
	fmt.Printf("page shrink replay, %s, offline over %s\n", time.Now().Format("2006-01-02 15:04"), *projects)
	fmt.Printf("session files %d, with a browser page %d, requests %d, requests after a gap over the %s cache life %d\n",
		len(paths), browsed, tallies[0].requests, ttl, tallies[0].cold)
	fmt.Printf("tokens estimated at %d bytes a thousand plus %d a message; tool schemas left out, the same under every rule\n",
		preview.BytesPerThousandTokens, konst.MessageFramingTokens)
	fmt.Printf("list price, Opus 5.5: cache read $%.2f, 1-hour cache write $%.2f a million tokens; every request priced as Anthropic\n\n",
		readDollarsPerMillion, writeDollarsPerMillion)
	fmt.Printf("%-18s %14s %14s %14s %14s %9s %12s %10s\n", "rule", "sent", "cache read", "cache write", "rewritten", "rewrites", "over target", "list USD")
	for i, rule := range rules {
		t := tallies[i]
		fmt.Printf("%-18s %14d %14d %14d %14d %9d %12d %10.2f\n", rule.name+" "+string(rule.gate), t.sent, t.read, t.sent-t.read, t.rewritten, t.rewrites, t.overTarget,
			(float64(t.read)*readDollarsPerMillion+float64(t.sent-t.read)*writeDollarsPerMillion)/1e6)
	}
	return nil
}

func browserEvents(path string) ([]session.Event, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if !bytes.Contains(raw, []byte(`"tool":"browser_observe"`)) && !bytes.Contains(raw, []byte(`"tool":"browser_act"`)) {
		return nil, nil
	}
	var events []session.Event
	for line := range bytes.Lines(raw) {
		var event session.Event
		if err := json.Unmarshal(line, &event); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		events = append(events, event)
	}
	return events, nil
}

func (r *replay) run(rule rule, events []session.Event, t *tally) error {
	agents := map[string]*agent{}
	for _, event := range events {
		a := agents[event.Agent]
		if a == nil {
			a = &agent{}
			agents[event.Agent] = a
		}
		switch event.Kind {
		case session.EventTurnStart:
			var start session.TurnStart
			if err := json.Unmarshal(event.Body, &start); err != nil {
				return err
			}
			a.target = start.ContextTarget
		case session.EventPrompt:
			var prompt session.PromptBody
			if err := json.Unmarshal(event.Body, &prompt); err != nil {
				return err
			}
			a.messages = []llm.Message{{Role: llm.RoleSystem, Content: prompt.System}}
			a.sent = slices.Clone(a.messages)
		case session.EventCompaction:
			var compacted struct {
				Fork json.RawMessage `json:"fork"`
			}
			if err := json.Unmarshal(event.Body, &compacted); err != nil {
				return err
			}
			if compacted.Fork != nil {
				a.messages = slices.DeleteFunc(a.messages, func(m llm.Message) bool { return m.Role != llm.RoleSystem })
				a.sent = slices.Clone(a.messages)
			}
		case session.EventMessage:
			var message session.MessageBody
			if err := json.Unmarshal(event.Body, &message); err != nil {
				return err
			}
			if message.Role == session.RoleUser {
				a.messages = append(a.messages, llm.Message{Role: llm.RoleUser, Content: message.Content})
			}
		case session.EventToolCall:
			var call session.CallBody
			if err := json.Unmarshal(event.Body, &call); err != nil {
				return err
			}
			a.calls = append(a.calls, llm.ToolCall{ID: event.Call, Name: call.Tool, Arguments: call.Args})
		case session.EventToolResult:
			var result session.ResultBody
			if err := json.Unmarshal(event.Body, &result); err != nil {
				return err
			}
			a.results = append(a.results, llm.Message{Role: llm.RoleTool, ToolCallID: event.Call, Content: result.Content})
		case session.EventRequest:
			var step session.StepBody
			if err := json.Unmarshal(event.Body, &step); err != nil {
				return err
			}
			if err := turn.ShrinkPages(r.store, r.preview, a.messages, rule.gate); err != nil {
				return err
			}
			r.count(a, t, event.At)
			a.messages = append(a.messages, llm.Message{Role: llm.RoleAssistant, Content: step.AssistantText, ToolCalls: a.calls})
			a.messages = append(a.messages, a.results...)
			a.calls, a.results = nil, nil
		}
	}
	return nil
}

func (r *replay) count(a *agent, t *tally, at time.Time) {
	shared, kept := min(len(a.sent), len(a.messages)), 0
	for kept < shared && a.sent[kept].Content == a.messages[kept].Content {
		kept++
	}
	if !a.askedAt.IsZero() && at.Sub(a.askedAt) > r.ttl {
		t.cold++
		kept = 0
	} else if kept < shared {
		t.rewrites++
		t.rewritten += turn.HistoryTokens(r.preview, a.messages[kept:shared])
	}
	sent := turn.HistoryTokens(r.preview, a.messages)
	t.requests++
	t.sent += sent
	t.read += turn.HistoryTokens(r.preview, a.messages[:kept])
	if a.target > 0 && sent > a.target {
		t.overTarget++
	}
	a.sent, a.askedAt = slices.Clone(a.messages), at
}
