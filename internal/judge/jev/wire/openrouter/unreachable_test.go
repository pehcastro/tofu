package openrouter_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"boji/internal/judge/jev"
	"boji/internal/judge/jev/wire/openrouter"
	"boji/internal/judge/ledger"
	"boji/internal/judge/policy"
	"boji/internal/transport"
)

func TestAnUnreachableEndpointStillLeavesARow(t *testing.T) {
	wire, err := openrouter.New(openrouter.Config{
		Endpoint:  "https://jev.unreachable.invalid/api/alpha/decisions",
		Key:       "not-a-real-key",
		Transport: transport.Config{AttemptTimeout: 2 * time.Second, Retries: 0, Backoff: time.Millisecond, Concurrency: 1},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	client, err := jev.NewClient(jev.Config{Wire: wire})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	state := []byte(`{"agent":"boji","tool":"bash","input":{"command":"git push --force"},"cwd":"/home/user/project","context":{"user_recent_messages":["can you check if the tests pass?"],"flagged_untrusted_content":null}}`)
	_, askErr := client.Ask(context.Background(), jev.Request{
		State:     json.RawMessage(state),
		Questions: []jev.Question{{ID: "risk", Kind: jev.QuestionNoul, Instructions: "how risky is this call", True: "risky", False: "safe"}},
	})
	if askErr == nil {
		t.Fatal("the unreachable endpoint answered, which cannot happen")
	}

	fallback := policy.DecideUnavailable(askErr, state)
	sentence := fallback.Sentence()
	hash, err := ledger.Hash(state)
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	dir := t.TempDir()
	written, err := ledger.NewWriter(dir).Append(ledger.Row{
		Point:         "tool_gate",
		Questions:     "tool_gate",
		Version:       1,
		Model:         openrouter.Alias,
		StateHash:     hash,
		Verdict:       ledger.Verdict(fallback.Verdict),
		Policy:        "tool_gate",
		PolicyVersion: 1,
		Reason: &ledger.Reason{
			Question:   "risk",
			Comparison: string(fallback.Comparison()),
			Mode:       ledger.ModeShadow,
			ModeReason: &sentence,
		},
	})
	if err != nil {
		t.Fatalf("Append: %v", err)
	}
	stored, ok, err := ledger.NewReader(dir).ByID(written.ID)
	if err != nil || !ok {
		t.Fatalf("ByID %s: ok=%v err=%v", written.ID, ok, err)
	}
	if stored.Verdict != ledger.VerdictAsk {
		t.Fatalf("verdict = %s, want ask", stored.Verdict)
	}
	line, err := ledger.Canonical(stored)
	if err != nil {
		t.Fatalf("Canonical: %v", err)
	}
	t.Logf("ask failed with: %v", askErr)
	t.Logf("row: %s", line)
}
