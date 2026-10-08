package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"tofu/internal/konst"
	"tofu/internal/llm"
	"tofu/internal/rule"
	"tofu/internal/shell"
	"tofu/internal/subagent"
	shipped "tofu/library"
)

type askTool struct {
	orchestrator *SpawnTool
	asking       subagent.SubAgent
	conversation []llm.Message
}

type askArgs struct {
	Question string `json:"question"`
	Why      string `json:"why"`
	Default  string `json:"default"`
}

func (askTool) Name() string { return "ask" }

func (askTool) Definition() llm.Tool {
	text := map[string]any{"type": "string"}
	return llm.Tool{
		Name: "ask",
		Description: "asks the orchestrator one question and waits for its answer, which comes back as this result. " +
			"ask before you start a server or a watcher, bind a port, kill a process you did not start, or change a file outside the paths you hold. " +
			"default is what you would do with no answer: when the orchestrator cannot answer in time it stands, and the result says it was assumed",
		Parameters: map[string]any{
			"type":       "object",
			"properties": map[string]any{"question": text, "why": text, "default": text},
			"required":   []string{"question", "why", "default"},
		},
	}
}

func (a askTool) Run(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args askArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, fmt.Errorf("ask: arguments are not the expected shape: %w", err)
	}
	if strings.TrimSpace(args.Question) == "" || strings.TrimSpace(args.Default) == "" {
		return Result{}, errors.New("ask: question and default are both required")
	}
	roster := a.orchestrator.roster
	roster.Reached(a.asking.ID, subagent.WaitingAnswer, "asks: "+args.Question)
	answer, err := a.orchestrator.answer(ctx, a.asking, a.conversation, args)
	roster.Reached(a.asking.ID, subagent.Working, "")
	if err != nil {
		return Result{Content: "the orchestrator did not answer, so your default stands, assumed and not confirmed: " + args.Default +
			"\n\nwhy there was no answer: " + err.Error(), Command: "ask assumed: " + args.Question}, nil
	}
	return Result{Content: "the orchestrator answers: " + answer, Command: "ask answered: " + args.Question}, nil
}

func (t *SpawnTool) answer(ctx context.Context, asking subagent.SubAgent, conversation []llm.Message, args askArgs) (string, error) {
	model := t.base.Model
	if t.base.Accounts.Pick != nil {
		account, err := t.base.Accounts.Pick(ctx)
		if err != nil {
			return "", err
		}
		model = account.Model
	}
	if model == nil {
		return "", errors.New("the orchestrator has no model to answer with")
	}
	rules, err := rule.LoadFS(shipped.Files(), "library")
	if err != nil {
		return "", err
	}
	system := t.base.System
	for _, loaded := range rules {
		if loaded.ID == "answer_asks" && !strings.Contains(system, loaded.Text) {
			system = strings.TrimSpace(loaded.Text + "\n\n" + system)
		}
	}
	var running []string
	if registry := ShellRegistryFrom(ctx); registry != nil {
		shells, err := registry.List()
		if err != nil {
			return "", err
		}
		for _, held := range shells {
			if held.State != shell.Running {
				continue
			}
			line := held.Name + " runs " + held.Command
			if port := shell.NamedPort(held.Dir, held.Command, nil); port > 0 {
				line += " on port " + strconv.Itoa(port)
			}
			running = append(running, line)
		}
	}
	for _, held := range t.roster.SubAgents() {
		running = append(running, fmt.Sprintf("sub-agent %s is %s on %q, holding %s", held.ID, held.State, held.Mission, strings.Join(held.Owns, ", ")))
	}
	question := fmt.Sprintf("sub-agent %s, whose brief is %q, asks you: %s\nwhy: %s\nwhat it does with no answer: %s\n\nwhat runs now: %s\n\nanswer it in one line.",
		asking.ID, asking.Brief, args.Question, args.Why, args.Default, strings.Join(running, "; "))
	spoken := slices.DeleteFunc(slices.Clone(conversation), func(message llm.Message) bool { return message.Role == llm.RoleSystem })
	messages := append([]llm.Message{{Role: llm.RoleSystem, Content: system}}, resumable(spoken)...)
	messages = append(messages, llm.Message{Role: llm.RoleUser, Content: question})
	within, cancel := context.WithTimeout(ctx, konst.SubAgentAskMillis*time.Millisecond)
	defer cancel()
	decision, err := model.Ask(within, llm.Request{Messages: messages})
	if err != nil {
		return "", err
	}
	if decision.Outcome != llm.OutcomeMessage || strings.TrimSpace(decision.Content) == "" {
		return "", errors.New("the orchestrator's model returned no answer text")
	}
	return strings.TrimSpace(decision.Content), nil
}
