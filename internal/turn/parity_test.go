package turn

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"tofu/internal/llm"
	"tofu/internal/llm/wire/openrouter"
	"tofu/internal/session"
	"tofu/internal/transport"
)

const keyPathAnswer = `{"id":"gen-parity","model":"openai/gpt-5.6","choices":[
{"finish_reason":"stop","message":{"content":"wrote hello.txt"}}],
"usage":{"prompt_tokens":8016,"completion_tokens":14,"cost":0.0012,
"prompt_tokens_details":{"cached_tokens":5120}}}`

func recordedKeyPath(t *testing.T) Model {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(keyPathAnswer))
	}))
	t.Cleanup(server.Close)

	wire, err := openrouter.New(openrouter.Config{
		Endpoint: server.URL,
		Model:    "openai/gpt-5.6",
		Key:      "not-a-credential",
		Transport: transport.Config{
			AttemptTimeout: time.Second,
			Concurrency:    1,
		},
	})
	if err != nil {
		t.Fatalf("building the wire: %v", err)
	}
	client, err := llm.NewClient(wire)
	if err != nil {
		t.Fatalf("building the client: %v", err)
	}
	return client
}

type recordedAnswers struct {
	wire      string
	stop      string
	cacheRead int
	fresh     int
	billed    int
	attempt   int
}

func answeredOnce(t *testing.T, wire string, spend Spend, model Model) recordedAnswers {
	t.Helper()
	dir := t.TempDir()
	store := session.OpenAt(dir)
	row, err := Run(context.Background(), Config{
		Model:          model,
		Wire:           wire,
		Spend:          spend,
		Task:           "write hello.txt",
		Caps:           Caps{MaxSteps: 2},
		ResultBytesCap: 4096,
		ArtifactDir:    filepath.Join(dir, "artifacts"),
		NoLastWord:     true,
		Sessions:       store,
	})
	if err != nil {
		t.Fatalf("%s: Run returned an error: %v", wire, err)
	}
	read := writeAndReadBack(t, row)
	if len(read.Steps) != 1 {
		t.Fatalf("%s recorded %d steps, want the one answer it was given", wire, len(read.Steps))
	}
	attempts := stepAttempts(t, store, read.ID)
	if len(attempts) != 1 {
		t.Fatalf("%s recorded %d step attempts, want one", wire, len(attempts))
	}
	step, accounting := read.Steps[0], read.PromptAccounting()
	return recordedAnswers{
		wire:      read.Wire,
		stop:      step.StopReason,
		cacheRead: step.CacheReadTokens,
		fresh:     accounting.FreshTokens(step.PromptTokens, step.CacheReadTokens),
		billed:    accounting.BilledTokens(step.PromptTokens, step.CacheReadTokens),
		attempt:   attempts[0],
	}
}

func TestASubscriptionDecisionSaysItsWireReportedNoAttemptCount(t *testing.T) {
	decision, err := recordedSubscription(t, "subscription-end-turn.sse").
		Ask(context.Background(), llm.Request{Messages: []llm.Message{{Role: llm.RoleUser, Content: "hi"}}})
	if err != nil {
		t.Fatal(err)
	}
	if decision.Attempts != llm.AttemptsUnreported {
		t.Fatalf("the decision reports attempts %d, and the anthropic wire counts none", decision.Attempts)
	}
	if got := decision.Attempts.String(); got != "the wire did not report an attempt count" {
		t.Fatalf("the decision reads its attempt count as %q", got)
	}
}

func TestARowFromTheKeyPathAndOneFromASubscriptionAnswerTheSameQuestions(t *testing.T) {
	for _, answers := range []recordedAnswers{
		answeredOnce(t, llm.WireOpenRouter, SpendAPIKey, recordedKeyPath(t)),
		answeredOnce(t, llm.WireAnthropic, SpendSubscription, recordedSubscription(t, "subscription-end-turn.sse")),
	} {
		if answers.stop == "" {
			t.Errorf("%s: the row does not say why the model stopped", answers.wire)
		}
		if answers.cacheRead == 0 {
			t.Errorf("%s: the row reports no cache read, and both answers were served from a cache", answers.wire)
		}
		if answers.fresh <= 0 {
			t.Errorf("%s: the row reads %d fresh prompt tokens", answers.wire, answers.fresh)
		}
		if answers.fresh+answers.cacheRead != answers.billed {
			t.Errorf("%s: fresh %d plus cache read %d is not the billed %d",
				answers.wire, answers.fresh, answers.cacheRead, answers.billed)
		}
		if answers.attempt < session.FirstAttempt {
			t.Errorf("%s: the row reads attempt %d", answers.wire, answers.attempt)
		}
		t.Logf("%s: stop %q, %d cached, %d fresh, %d billed, attempt %d",
			answers.wire, answers.stop, answers.cacheRead, answers.fresh, answers.billed, answers.attempt)
	}
}
