package digest

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"tofu/internal/agent/context/recall"
	"tofu/internal/models/llm/core"
	"tofu/internal/platform/text"
	"tofu/internal/store/session"
	"tofu/internal/tools/agents/context/window"
	"tofu/internal/tools/contract"
)

const (
	Name     = "digest"
	answerer = "you answer one question about one tool result, numbered line by line as L1, L2 and on. " +
		"answer in a few plain sentences from the result alone, and cite every line your answer rests on as L12, or L12-L15 for a run of lines. " +
		"when the result does not hold the answer, say so and cite nothing."
)

var (
	artifactLead = regexp.MustCompile(`^artifact (\S+) holds `)
	lineRef      = regexp.MustCompile(`\bL(\d+)(?:\s*-\s*L?(\d+))?\b`)
)

type Model interface {
	Ask(ctx context.Context, request core.Request) (core.Decision, error)
}

type Bound struct {
	Model        Model
	Slug         string
	Subscription bool
	Budget       recall.Budget
	Release      func()
}

type Digest struct {
	Sessions  *session.Store
	Artifacts *recall.Store
	Bind      func(context.Context) (Bound, error)
}

func (Digest) Name() string { return Name }

func (Digest) Definition() core.Tool {
	return core.Tool{
		Name: Name,
		Description: "answers one question about one earlier tool result, such as which tests failed in a long test log, without reading the result into your context. " +
			"call is the call id of that result; question is what you want to know. a cheap model reads the result whole and answers with the line numbers it rests on, " +
			"each cited line is quoted from the real result, and the cost of the call is the last line. session names another recorded session by name, id or head; left out it is the one you run in.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"call":     map[string]any{"type": "string"},
				"question": map[string]any{"type": "string"},
				"session":  map[string]any{"type": "string"},
			},
			"required": []string{"call", "question"},
		},
	}
}

func (d Digest) Run(ctx context.Context, raw json.RawMessage) (contract.Result, error) {
	var args struct {
		Call     string `json:"call"`
		Question string `json:"question"`
		Session  string `json:"session"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return contract.Result{}, fmt.Errorf("digest: arguments are not the expected shape: %w", err)
	}
	args.Call, args.Question = strings.TrimSpace(args.Call), strings.TrimSpace(args.Question)
	if args.Call == "" || args.Question == "" {
		return contract.Result{}, errors.New("digest: call and question are both required: call is the id of an earlier tool call, question is what to ask about its result")
	}
	result, err := d.resultOf(ctx, args.Session, args.Call)
	if err != nil {
		return contract.Result{}, fmt.Errorf("digest: %w", err)
	}
	command := Name + " " + args.Call
	if strings.TrimSpace(result) == "" {
		return contract.Result{Content: "call " + args.Call + " returned nothing, so there is nothing to ask about\n", Command: command}, nil
	}
	bound, err := d.Bind(ctx)
	if err != nil {
		return contract.Result{}, fmt.Errorf("digest: %w", err)
	}
	defer bound.Release()
	cfg, err := recall.LoadConfig()
	if err != nil {
		return contract.Result{}, fmt.Errorf("digest: %w", err)
	}
	lines := strings.Split(strings.ReplaceAll(strings.TrimRight(result, "\n"), "\r\n", "\n"), "\n")
	target := bound.Budget.Bands.Target()
	room := target - cfg.MessageTokens(answerer+args.Question)
	var numbered strings.Builder
	read := 0
	for read < len(lines) {
		line := "L" + strconv.Itoa(read+1) + " " + lines[read] + "\n"
		if room -= cfg.MessageTokens(line); room < 0 {
			break
		}
		numbered.WriteString(line)
		read++
	}
	if read == 0 {
		return contract.Result{}, fmt.Errorf("digest: the first line of call %s does not fit the %d token target of %s", args.Call, target, bound.Slug)
	}
	decision, err := bound.Model.Ask(ctx, core.Request{Messages: []core.Message{
		{Role: core.RoleSystem, Content: answerer},
		{Role: core.RoleUser, Content: "question: " + args.Question + "\n\nthe result of call " + args.Call + ":\n" + numbered.String()},
	}})
	if err != nil {
		return contract.Result{}, fmt.Errorf("digest: %s did not answer: %w", bound.Slug, err)
	}
	answer, err := answerOf(decision)
	if err != nil {
		return contract.Result{}, fmt.Errorf("digest: %s %w", bound.Slug, err)
	}
	var out strings.Builder
	out.WriteString(answer + "\n")
	if read < len(lines) {
		fmt.Fprintf(&out, "\nthe model read lines 1 to %d of %d, as many as fit the %d token target of %s; the rest were not read\n", read, len(lines), target, bound.Slug)
	}
	out.WriteString(quoted(answer, lines[:read], args.Call))
	spent := strconv.FormatFloat(decision.Usage.Cost, 'f', 4, 64) + " USD"
	if bound.Subscription {
		spent = "a subscription, no money"
	}
	fmt.Fprintf(&out, "\ncost %s, 1 request, %d tokens in and %d out on %s\n", spent,
		decision.Usage.InputTokens+decision.CacheReadTokens+decision.CacheWriteTokens, decision.Usage.OutputTokens, cmp.Or(decision.Build, bound.Slug))
	return contract.Result{Content: out.String(), Command: command}, nil
}

func answerOf(decision core.Decision) (string, error) {
	answer := strings.TrimSpace(decision.Content)
	switch decision.Outcome {
	case core.OutcomeMessage:
		if answer == "" {
			return "", errors.New("answered with no words, which is how a content refusal arrives")
		}
		return answer, nil
	case core.OutcomeRefusal:
		return "", errors.New("refused: " + cmp.Or(decision.Refusal, answer))
	case core.OutcomeTruncated, core.OutcomeToolCalls, core.OutcomeUnknown:
		return "", errors.New("stopped with " + decision.Outcome.String() + " rather than an answer")
	}
	panic("digest: an outcome this build does not know: " + decision.Outcome.String())
}

func quoted(answer string, read []string, call string) string {
	var cited []int
	var past []string
	for _, ref := range lineRef.FindAllStringSubmatch(answer, -1) {
		first, _ := strconv.Atoi(ref[1])
		last := first
		if ref[2] != "" {
			last, _ = strconv.Atoi(ref[2])
		}
		if first < 1 || last < first || last > len(read) {
			past = append(past, ref[0])
			continue
		}
		for at := first; at <= last; at++ {
			cited = append(cited, at)
		}
	}
	slices.Sort(cited)
	if cited = slices.Compact(cited); len(cited) == 0 && len(past) == 0 {
		return "\nthe answer cites no line of the result\n"
	}
	out := "\nthe lines it cites, from the result of call " + call + ":\n"
	for _, at := range cited {
		out += "L" + strconv.Itoa(at) + " " + read[at-1] + "\n"
	}
	if len(past) > 0 {
		out += "cited but not among the lines read, so not quoted: " + strings.Join(past, ", ") + "\n"
	}
	return out
}

func (d Digest) resultOf(ctx context.Context, handle, call string) (string, error) {
	asked, err := window.Ask(ctx, d.Sessions, handle)
	if err != nil {
		return "", err
	}
	if asked.Header.ID == "" {
		return "", errors.New("no session is recorded yet, so there is no call " + call + " to read")
	}
	var calls []string
	seen := map[string]bool{}
	for id, events := asked.Header.ID, asked.Events; id != "" && !seen[id]; {
		seen[id] = true
		called, answered := false, -1
		for at, event := range events {
			switch {
			case event.Kind == session.EventToolCall && event.Call != "":
				calls = append(calls, event.Call)
				called = called || event.Call == call
			case event.Kind == session.EventToolResult && event.Call == call:
				answered = at
			}
		}
		if answered >= 0 {
			var body session.ResultBody
			if err := json.Unmarshal(events[answered].Body, &body); err != nil {
				return "", fmt.Errorf("the result of call %s in session %s does not parse: %w", call, id, err)
			}
			return d.whole(cmp.Or(body.Content, body.Error))
		}
		if called {
			return "", fmt.Errorf("call %s in session %s has no result recorded yet", call, id)
		}
		header, err := d.Sessions.Header(id)
		if err != nil {
			return "", fmt.Errorf("session %s: %w", id, err)
		}
		id, events = "", nil
		if header.CarriedFrom != nil {
			id = header.CarriedFrom.Session
			if events, err = d.Sessions.Events(id); err != nil {
				return "", fmt.Errorf("session %s: %w", id, err)
			}
		}
	}
	if len(calls) == 0 {
		return "", fmt.Errorf("session %s records no tool call, so there is no call %s", asked.Handle, call)
	}
	closest := slices.MinFunc(calls, func(a, b string) int {
		return cmp.Compare(text.EditDistance(call, a), text.EditDistance(call, b))
	})
	return "", fmt.Errorf("no call %s in session %s or any session it was carried from; the closest is %s", call, asked.Handle, closest)
}

func (d Digest) whole(recorded string) (string, error) {
	held := artifactLead.FindStringSubmatch(recorded)
	if held == nil {
		return recorded, nil
	}
	body, err := d.Artifacts.Fetch(held[1])
	if err != nil {
		return "", fmt.Errorf("the result was stored whole as artifact %s, and it does not read: %w", held[1], err)
	}
	return string(body), nil
}
