package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"slices"
	"strings"
	"testing"

	"tofu/internal/llm"
	"tofu/internal/llm/wire/anthropic"
	"tofu/internal/llm/wire/codex"
	"tofu/internal/recall"
	"tofu/internal/transport"
)

type answer func(t *testing.T) (llm.Decision, error)

type overflowModel struct {
	t        *testing.T
	answers  []answer
	requests [][]llm.Message
}

func (m *overflowModel) Ask(_ context.Context, request llm.Request) (llm.Decision, error) {
	m.requests = append(m.requests, slices.Clone(request.Messages))
	if len(m.requests) > len(m.answers) {
		return llm.Decision{}, errors.New("overflowModel: asked once more than it holds")
	}
	return m.answers[len(m.requests)-1](m.t)
}

type longResult string

func (l longResult) Name() string { return string(l) }

func (l longResult) Definition() llm.Tool {
	return llm.Tool{Name: string(l), Parameters: map[string]any{"type": "object"}}
}

func (longResult) Run(_ context.Context, args json.RawMessage) (Result, error) {
	var lines strings.Builder
	for i := range 1500 {
		fmt.Fprintf(&lines, "%s line %d of a long file\n", args, i)
	}
	return Result{Content: lines.String()}, nil
}

func callNamed(id string) answer {
	return func(*testing.T) (llm.Decision, error) {
		return toolCallDecision(llm.ToolCall{ID: id, Name: "read", Arguments: json.RawMessage(`{"path":"` + id + `"}`)}), nil
	}
}

func failing(err error) answer {
	return func(*testing.T) (llm.Decision, error) { return llm.Decision{}, err }
}

func answered(*testing.T) (llm.Decision, error) { return messageDecision(), nil }

func fixture(t *testing.T, name string) *os.File {
	body, err := os.Open("testdata/overflow/" + name)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = body.Close() })
	return body
}

func anthropicStream(t *testing.T) (llm.Decision, error) {
	result, err := anthropic.ReadStream(fixture(t, "anthropic-window-exceeded.sse"), false, nil, nil)
	return llm.Decision{Build: result.Model, Content: result.Content, Outcome: llm.OutcomeAfter(result.Stop, len(result.ToolCalls))}, err
}

func codexStream(t *testing.T) (llm.Decision, error) {
	result, err := codex.ReadStream(fixture(t, "codex-context-length.sse"), nil)
	return llm.Decision{Build: result.Model, Content: result.Content, Outcome: llm.OutcomeAfter(result.Stop, len(result.ToolCalls))}, err
}

func wireRefused(status int, detail string) error {
	return &transport.Error{Kind: transport.StatusKind(status), Op: "anthropic.Ask", Status: status, Detail: detail}
}

const anthropicTooLong = `{"type":"error","error":{"type":"invalid_request_error","message":"prompt is too long: 213462 tokens > 200000 maximum"}}`

func runOverflow(t *testing.T, answers ...answer) (*overflowModel, Row, []string, error) {
	model := &overflowModel{t: t, answers: answers}
	var told []string
	row, err := Run(context.Background(), Config{Model: model, Spend: SpendSubscription, Task: "read two long files", ResultBytesCap: 4096,
		ArtifactDir: t.TempDir(), NoLastWord: true, Tools: NewRegistry(longResult("read")), Notify: func(line string) { told = append(told, line) }})
	return model, row, told, err
}

func TestWithHandlesOffAShrunkResultSaysItWasDroppedAndNamesNoFetch(t *testing.T) {
	model := &overflowModel{t: t, answers: []answer{callNamed("call-1"), callNamed("call-2"), failing(wireRefused(http.StatusBadRequest, anthropicTooLong)), answered}}
	_, err := Run(context.Background(), Config{Model: model, Spend: SpendSubscription, Task: "read two long files", ResultBytesCap: 4096,
		ArtifactDir: t.TempDir(), NoLastWord: true, TruncateResults: true, Tools: NewRegistry(longResult("read"))})
	if err != nil || len(model.requests) != 4 {
		t.Fatalf("the turn asked %d times and ended with %v, want 4 asks and an answer", len(model.requests), err)
	}
	if shrunk := resultOf(model.requests[3], "call-1"); !strings.Contains(shrunk, "dropped") || strings.Contains(shrunk, "artifact") {
		t.Errorf("with no artifact_fetch tool the shrunk result reads %q, want it dropped and naming no artifact", shrunk)
	}
}

func resultOf(messages []llm.Message, id string) string {
	for _, message := range messages {
		if message.Role == llm.RoleTool && message.ToolCallID == id {
			return message.Content
		}
	}
	return ""
}

func handleIn(text string) string {
	_, after, _ := strings.Cut(text, "artifact ")
	handle, _, _ := strings.Cut(after, " ")
	return strings.TrimSuffix(handle, ":")
}

func TestAnOverflowShrinksTheOldestResultsAndAsksOnceMore(t *testing.T) {
	for name, overflow := range map[string]answer{
		"anthropic 400 prompt is too long":              failing(wireRefused(http.StatusBadRequest, anthropicTooLong)),
		"413 request_too_large":                         failing(wireRefused(http.StatusRequestEntityTooLarge, `{"error":{"type":"request_too_large","message":"Request exceeds the maximum size"}}`)),
		"codex 400 exceeds the context window":          failing(wireRefused(http.StatusBadRequest, `{"error":{"message":"Your input exceeds the context window of this model."}}`)),
		"anthropic stop model_context_window_exceeded":  anthropicStream,
		"codex response.failed context_length_exceeded": codexStream,
		"tofu's own window guard":                       failing(&recall.OverWindow{Model: "m1", WindowTokens: 1000, RequestTokens: 4000}),
		"a 400 wrapped by a caller":                     failing(fmt.Errorf("asking the lead: %w", wireRefused(http.StatusBadRequest, anthropicTooLong))),
	} {
		t.Run(name, func(t *testing.T) {
			model, row, told, err := runOverflow(t, callNamed("call-1"), callNamed("call-2"), overflow, answered)
			if err != nil || row.Outcome != OutcomeStopped {
				t.Fatalf("the turn ended %s with %v, want it stopped on the answer after one shrink", row.Outcome, err)
			}
			if len(model.requests) != 4 {
				t.Fatalf("the model was asked %d times, want 4", len(model.requests))
			}
			failed, retried := model.requests[2], model.requests[3]
			if len(retried) != len(failed) {
				t.Errorf("the retry carries %d messages and the failed attempt %d, want the failed attempt dropped", len(retried), len(failed))
			}
			before, after := resultOf(failed, "call-1"), resultOf(retried, "call-1")
			if !recall.AlreadyDropped(after) || len(after) >= len(before) || handleIn(after) == "" || handleIn(after) != handleIn(before) {
				t.Errorf("the oldest result was not shrunk to the handle it already had:\nbefore %.120q\nafter  %q", before, after)
			}
			if resultOf(retried, "call-2") != resultOf(failed, "call-2") {
				t.Errorf("the last step's result changed: %.200q", resultOf(retried, "call-2"))
			}
			t.Log(told)
			if len(told) != 1 || !strings.Contains(told[0], "shrank") || !strings.Contains(strings.Join(row.Warnings, "\n"), told[0]) {
				t.Errorf("the person was told %q and the row warns %q, want one shrink line in both", told, row.Warnings)
			}
		})
	}
}

func TestAnOverflowThatIsNotRecoverableEndsTheTurnSayingWhy(t *testing.T) {
	overflowed := failing(wireRefused(http.StatusBadRequest, anthropicTooLong))
	for name, run := range map[string]struct {
		answers []answer
		asked   int
		says    string
	}{
		"a second overflow in the same step": {[]answer{callNamed("call-1"), callNamed("call-2"), overflowed, overflowed}, 4, "overflowed again"},
		"nothing old enough to shrink":       {[]answer{overflowed}, 1, "no old tool result"},
		"rate-limit text that names tokens":  {[]answer{callNamed("call-1"), callNamed("call-2"), failing(wireRefused(http.StatusBadRequest, "rate limit reached: too many tokens per minute"))}, 3, "rate limit"},
		"a 429":                              {[]answer{callNamed("call-1"), callNamed("call-2"), failing(wireRefused(http.StatusTooManyRequests, "prompt is too long"))}, 3, "rate_limit"},
	} {
		t.Run(name, func(t *testing.T) {
			model, row, _, err := runOverflow(t, run.answers...)
			if err == nil || row.Outcome != OutcomeError || !strings.Contains(err.Error(), run.says) {
				t.Fatalf("the turn ended %s with %v, want an error saying %q", row.Outcome, err, run.says)
			}
			if len(model.requests) != run.asked {
				t.Errorf("the model was asked %d times, want %d", len(model.requests), run.asked)
			}
			for i, message := range model.requests[len(model.requests)-1] {
				for _, call := range message.ToolCalls {
					if resultOf(model.requests[len(model.requests)-1][i:], call.ID) == "" {
						t.Errorf("tool call %s lost its result", call.ID)
					}
				}
			}
			if run.asked == 4 && !strings.Contains(err.Error(), "shrank 1") {
				t.Errorf("the second overflow does not say what was tried: %v", err)
			}
		})
	}
}
