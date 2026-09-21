package turn

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"tofu/internal/llm"
	"tofu/internal/session"
	"tofu/internal/transport"
)

type postingModel struct {
	client *transport.Client
	url    string
}

func (m postingModel) Ask(ctx context.Context, _ llm.Request) (llm.Decision, error) {
	if _, err := m.client.Do(ctx, transport.Request{Method: http.MethodPost, URL: m.url, Body: []byte("{}")}); err != nil {
		return llm.Decision{}, err
	}
	return llm.Decision{Build: "stub-model", Outcome: llm.OutcomeMessage, Content: "answered"}, nil
}

type reportingModel struct {
	attempts llm.Attempts
}

func (m reportingModel) Ask(context.Context, llm.Request) (llm.Decision, error) {
	return llm.Decision{Build: "stub-model", Outcome: llm.OutcomeMessage, Content: "answered", Attempts: m.attempts}, nil
}

func TestAWireThatCountsMoreAttemptsThanThisProcessSentIsWarnedAbout(t *testing.T) {
	decision, sent, err := askCountingAttempts(context.Background(), reportingModel{attempts: 3}, llm.Request{})
	if err != nil {
		t.Fatal(err)
	}
	if sent != session.FirstAttempt {
		t.Fatalf("nothing went on the wire and the trace counted %d", sent)
	}
	if len(decision.Warnings) != 1 {
		t.Fatalf("warnings are %v, want the one that names both counts", decision.Warnings)
	}
	t.Log(decision.Warnings[0])
}

func TestAWireThatReportsNoAttemptCountIsNotWarnedAbout(t *testing.T) {
	decision, _, err := askCountingAttempts(context.Background(), reportingModel{attempts: llm.AttemptsUnreported}, llm.Request{})
	if err != nil {
		t.Fatal(err)
	}
	if len(decision.Warnings) != 0 {
		t.Fatalf("warnings are %v, and a wire that reports no count disagrees with nothing", decision.Warnings)
	}
}

func stepAttempts(t *testing.T, store *session.Store, id string) []int {
	t.Helper()
	events, err := store.Body(id)
	if err != nil {
		t.Fatal(err)
	}
	var attempts []int
	for _, event := range events {
		if event.Kind == session.EventStep {
			attempts = append(attempts, event.Attempt)
		}
	}
	return attempts
}

func runAgainst(t *testing.T, handler http.HandlerFunc) (*session.Store, Row) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client, err := transport.New(transport.Config{
		AttemptTimeout: time.Second,
		Retries:        1,
		Backoff:        time.Millisecond,
		Concurrency:    1,
	})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	store := session.OpenAt(dir)
	row, err := Run(context.Background(), Config{
		Model:          postingModel{client: client, url: server.URL},
		Spend:          SpendSubscription,
		Task:           "answer once",
		ResultBytesCap: 1024,
		ArtifactDir:    filepath.Join(dir, "artifacts"),
		NoCompaction:   true,
		NoFork:         true,
		NoLastWord:     true,
		Caps:           Caps{MaxSteps: 2},
		Sessions:       store,
	})
	if err != nil {
		t.Fatal(err)
	}
	return store, row
}

func TestARetriedRequestIsRecordedAsASecondAttempt(t *testing.T) {
	served := 0
	store, row := runAgainst(t, func(w http.ResponseWriter, _ *http.Request) {
		served++
		if served == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(`{}`))
	})
	if served != 2 {
		t.Fatalf("the server saw %d requests, want a first that failed and a retry", served)
	}
	attempts := stepAttempts(t, store, row.ID)
	if len(attempts) != 1 || attempts[0] != 2 {
		t.Fatalf("the recorded steps carry attempts %v, want one step recorded as the second attempt", attempts)
	}
}

func TestARequestAnsweredFirstTimeIsRecordedAsTheFirstAttempt(t *testing.T) {
	store, row := runAgainst(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	})
	attempts := stepAttempts(t, store, row.ID)
	if len(attempts) != 1 || attempts[0] != session.FirstAttempt {
		t.Fatalf("the recorded steps carry attempts %v, want one first attempt", attempts)
	}
}

func TestASessionRecordedBeforeTheAttemptFieldReadsBackWithoutIt(t *testing.T) {
	real := filepath.Join("..", "..", ".tofu", "sessions", "turn-18d7430fd0c0c304")
	body, err := os.ReadFile(filepath.Join(real, "body.jsonl"))
	if err != nil {
		t.Skipf("no copy of a real pre-change session on this machine: %v", err)
	}
	header, err := os.ReadFile(filepath.Join(real, "header.json"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	copied := filepath.Join(dir, "sessions", "turn-18d7430fd0c0c304")
	if err := os.MkdirAll(copied, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, raw := range map[string][]byte{"body.jsonl": body, "header.json": header} {
		if err := os.WriteFile(filepath.Join(copied, name), raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	store := session.OpenAt(dir)
	events, err := store.Body("turn-18d7430fd0c0c304")
	if err != nil {
		t.Fatalf("a session recorded before the attempt field was refused: %v", err)
	}
	steps := 0
	for _, event := range events {
		if event.Attempt != 0 {
			t.Fatalf("event %s of a pre-change session reads attempt %d, want none", event.ID, event.Attempt)
		}
		if event.Kind != session.EventStep {
			continue
		}
		steps++
		var step StepRow
		if err := json.Unmarshal(event.Body, &step); err != nil {
			t.Fatal(err)
		}
		if step.Index != steps {
			t.Fatalf("step %d of a pre-change session reads back as index %d", steps, step.Index)
		}
	}
	t.Logf("%d events, %d of them steps, read back from a copy of a session written under schema 2, none carrying an attempt", len(events), steps)
}
