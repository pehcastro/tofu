package connector

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"boji/internal/konst"
	"boji/internal/llm"
	"boji/internal/transport"
)

const replySchema = `{"type":"object","properties":{"kind":{"type":"string","enum":["message","tool_call"]},` +
	`"text":{"type":"string"},"tool":{"type":"string"},"arguments":{"type":"object"}},` +
	`"required":["kind"],"additionalProperties":false}`

type Claude struct {
	Bin           string
	Model         string
	Dir           string
	WallClock     time.Duration
	ListBudgetUSD float64
}

func NewClaude(dir string) (Claude, error) {
	if dir == "" {
		return Claude{}, transport.Fail("connector.NewClaude", transport.KindBadRequest, nil,
			"the claude connector needs a working directory")
	}
	return Claude{
		Bin:           konst.ClaudeBin,
		Model:         konst.ClaudeModelAlias,
		Dir:           dir,
		WallClock:     time.Duration(konst.ClaudeWallClockMillis) * time.Millisecond,
		ListBudgetUSD: konst.ClaudeListBudgetUSD,
	}, nil
}

func (c Claude) Ask(ctx context.Context, request llm.Request) (llm.Decision, error) {
	system, prompt, err := render(request)
	if err != nil {
		return llm.Decision{}, err
	}

	ctx, stop := context.WithTimeout(ctx, c.WallClock)
	defer stop()

	command := exec.CommandContext(ctx, c.Bin,
		"-p",
		"--output-format", "stream-json",
		"--verbose",
		"--tools", "",
		"--permission-prompts", "none",
		"--setting-sources", "",
		"--strict-mcp-config",
		"--no-session-persistence",
		"--model", c.Model,
		"--max-budget-usd", strconv.FormatFloat(c.ListBudgetUSD, 'f', -1, 64),
		"--append-system-prompt", system,
		"--json-schema", replySchema,
	)
	command.Dir = c.Dir
	command.Stdin = strings.NewReader(prompt)
	var out, fail bytes.Buffer
	command.Stdout, command.Stderr = &out, &fail
	command.WaitDelay = time.Second

	started := time.Now()
	runErr := command.Run()
	latency := time.Since(started)

	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return llm.Decision{}, transport.Fail("connector.Claude.Ask", transport.KindTimeout, nil,
			"the claude subprocess produced no result within the %s wall clock cap", c.WallClock)
	}

	transcript, err := ReadTranscript(bytes.NewReader(out.Bytes()))
	if err != nil {
		return llm.Decision{}, err
	}
	if transcript.Stop == StopProcessFailed && runErr != nil && transcript.Detail == "" {
		transcript.Detail = runErr.Error() + ": " + strings.TrimSpace(fail.String())
	}
	return decide(transcript, latency, len(prompt))
}

func decide(transcript Transcript, latency time.Duration, bytesSent int) (llm.Decision, error) {
	switch transcript.Stop {
	case StopCapReached:
		return llm.Decision{}, transport.Fail("connector.Claude.Ask", transport.KindBudget, nil,
			"the claude run stopped at a cap: %s", transcript.Detail)
	case StopQuotaClosed:
		return llm.Decision{}, transport.Fail("connector.Claude.Ask", transport.KindRateLimit, nil,
			"the claude quota window is closed: %s", transcript.Detail)
	case StopProcessFailed:
		return llm.Decision{}, transport.Fail("connector.Claude.Ask", transport.KindProvider, nil,
			"the claude subprocess failed: %s", transcript.Detail)
	case StopPermissionDenied, StopModelStopped:
	}

	decision := llm.Decision{
		Build:       transcript.Model,
		RequestID:   transcript.SessionID,
		TransportID: transcript.CallID,
		Stop:        transcript.Stop.String(),
		Usage:       llm.Usage{InputTokens: transcript.InputTokens, OutputTokens: transcript.OutputTokens},
		ListCostUSD: transcript.ListCostUSD,
		Latency:     latency,
		Bytes:       bytesSent,
		Raw:         transcript.Structured,
	}

	if transcript.Stop == StopPermissionDenied {
		decision.Outcome = llm.OutcomeRefusal
		decision.Refusal = "claude denied its own tool use: " + strings.Join(transcript.Denied, " ")
		return decision, nil
	}

	var reply struct {
		Kind      string          `json:"kind"`
		Text      string          `json:"text"`
		Tool      string          `json:"tool"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(transcript.Structured, &reply); err != nil {
		return llm.Decision{}, transport.Fail("connector.Claude.Ask", transport.KindInvalidAnswer, err,
			"the claude result carries no structured reply")
	}

	switch reply.Kind {
	case "tool_call":
		if reply.Tool == "" || len(reply.Arguments) == 0 {
			return llm.Decision{}, transport.Fail("connector.Claude.Ask", transport.KindInvalidAnswer, nil,
				"the claude reply is a tool call with no tool name or no arguments")
		}
		decision.Outcome = llm.OutcomeToolCalls
		decision.ToolCalls = []llm.ToolCall{{ID: transcript.CallID, Name: reply.Tool, Arguments: reply.Arguments}}
	case "message":
		decision.Outcome = llm.OutcomeMessage
		decision.Content = reply.Text
		if decision.Content == "" {
			decision.Content = transcript.Text
		}
	default:
		return llm.Decision{}, transport.Fail("connector.Claude.Ask", transport.KindInvalidAnswer, nil,
			"the claude reply has kind %q, which is neither message nor tool_call", reply.Kind)
	}
	return decision, nil
}

func render(request llm.Request) (system, prompt string, err error) {
	if len(request.Messages) == 0 {
		return "", "", transport.Fail("connector.Claude.Ask", transport.KindBadRequest, nil,
			"the request carries no messages")
	}

	var instructions, conversation strings.Builder
	instructions.WriteString("You are one step of a harness that owns the loop. Never act on your own: " +
		"you have no tools of your own, and the only thing you produce is the structured reply. " +
		"Answer with kind message and a text field when the task is done or needs an answer in words. " +
		"Answer with kind tool_call, a tool name and an arguments object when the next step is a tool.\n")
	if len(request.Tools) > 0 {
		instructions.WriteString("The tools the harness will run for you:\n")
		for _, tool := range request.Tools {
			parameters, marshalErr := json.Marshal(tool.Parameters)
			if marshalErr != nil {
				return "", "", transport.Fail("connector.Claude.Ask", transport.KindBadRequest, marshalErr,
					"encoding the parameters of tool %q", tool.Name)
			}
			fmt.Fprintf(&instructions, "%s: %s, arguments %s\n", tool.Name, tool.Description, parameters)
		}
	}

	for index, message := range request.Messages {
		switch message.Role {
		case llm.RoleSystem:
			instructions.WriteString(message.Content + "\n")
		case llm.RoleUser:
			conversation.WriteString("user: " + message.Content + "\n")
		case llm.RoleAssistant:
			for _, call := range message.ToolCalls {
				fmt.Fprintf(&conversation, "you called %s with %s\n", call.Name, call.Arguments)
			}
			if message.Content != "" {
				conversation.WriteString("you: " + message.Content + "\n")
			}
		case llm.RoleTool:
			conversation.WriteString("the harness ran it and got: " + message.Content + "\n")
		case llm.RoleUnknown:
			return "", "", transport.Fail("connector.Claude.Ask", transport.KindBadRequest, nil,
				"message %d has an unknown role", index)
		}
	}
	return instructions.String(), conversation.String(), nil
}

type Health struct {
	Bin          string
	Path         string
	Version      string
	LoggedIn     bool
	AuthMethod   string
	Subscription string
	Problem      string
}

func (h Health) String() string {
	if h.Problem != "" {
		return h.Bin + ": " + h.Problem
	}
	state := "logged out"
	if h.LoggedIn {
		state = "logged in by " + h.AuthMethod + ", " + h.Subscription
	}
	return h.Bin + ": " + h.Version + " at " + h.Path + ", " + state + ", spends subscription quota rather than money"
}

func Probe(ctx context.Context, bin string) Health {
	health := Health{Bin: bin}
	path, err := exec.LookPath(bin)
	if err != nil {
		health.Problem = "not on the path"
		return health
	}
	health.Path = path

	ctx, stop := context.WithTimeout(ctx, time.Duration(konst.ClaudeProbeTimeoutMillis)*time.Millisecond)
	defer stop()

	version, err := exec.CommandContext(ctx, path, "--version").Output()
	if err != nil {
		health.Problem = "found at " + path + " but it does not report a version: " + err.Error()
		return health
	}
	health.Version = strings.TrimSpace(string(version))

	status, err := exec.CommandContext(ctx, path, "auth", "status").Output()
	if err != nil {
		health.Problem = health.Version + " at " + path + ", login state unknown: " + err.Error()
		return health
	}
	var reported struct {
		LoggedIn         bool   `json:"loggedIn"`
		AuthMethod       string `json:"authMethod"`
		SubscriptionType string `json:"subscriptionType"`
	}
	if err := json.Unmarshal(status, &reported); err != nil {
		health.Problem = health.Version + " at " + path + ", login state unreadable"
		return health
	}
	health.LoggedIn, health.AuthMethod, health.Subscription = reported.LoggedIn, reported.AuthMethod, reported.SubscriptionType
	return health
}
