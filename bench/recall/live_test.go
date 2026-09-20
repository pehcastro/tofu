package recall

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"boji/internal/llm"
	"boji/internal/llm/cred"
	"boji/internal/llm/wire/anthropic"
	rc "boji/internal/recall"
	"boji/internal/turn"
)

const liveSummaryModel = "claude-sonnet-5"

const summaryInstruction = "the session below has reached its context budget and is ending. " +
	"write the first message of a new session that continues the same work. " +
	"it is the only thing the new session will have: say what the task is, what has been done, " +
	"what is left, and anything already learned that would be expensive to learn again. " +
	"write it to be read by the agent that continues, not by a person, and do not pad it.\n\n"

func liveSubscription(t *testing.T) turn.Subscription {
	t.Helper()
	path, err := cred.Path()
	if err != nil {
		t.Fatalf("locating the credential store: %v", err)
	}
	store, err := cred.Open(path)
	if err != nil {
		t.Skipf("no credential store, run boji login anthropic: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	spec, err := cred.Lookup("anthropic")
	if err != nil {
		t.Fatalf("looking up the anthropic spec: %v", err)
	}
	wire, err := anthropic.New(anthropic.Config{
		Model:     liveSummaryModel,
		Token:     cred.NewManager(store, spec).Access,
		Watchdog:  2 * time.Minute,
		SessionID: "00000000-0000-4000-8000-0000000000f0",
		InstallID: "boji-bench-recall",
	})
	if err != nil {
		t.Fatalf("building the wire: %v", err)
	}
	return turn.Subscription{Wire: wire}
}

func TestLiveSummaryCarryAtEveryForkOfTheRecordedTurn(t *testing.T) {
	if os.Getenv("BOJI_LIVE") != "1" {
		t.Skip("set BOJI_LIVE=1 to spend subscription quota writing one summary per fork")
	}
	cfg, session := recordedTurn(t)
	bands := measuringBands()
	model := liveSubscription(t)

	recorded := Summaries{Session: session.ID, Target: bands.Target()}
	arm := Arm{Name: "fork, summary", Carry: func(_ *rc.Store, _ rc.Config, c rc.Conversation) (rc.Carry, error) {
		started := time.Now()
		decision, err := model.Ask(context.Background(), llm.Request{
			Messages: []llm.Message{{Role: llm.RoleUser, Content: summaryInstruction + Transcript(c)}},
		})
		if err != nil {
			return rc.Carry{}, err
		}
		recorded.Calls = append(recorded.Calls, Summary{
			Text:         decision.Content,
			Model:        decision.Build,
			Spend:        string(turn.SpendSubscription),
			LatencyMS:    time.Since(started).Milliseconds(),
			InputTokens:  decision.Usage.InputTokens,
			OutputTokens: decision.Usage.OutputTokens,
			CostUSD:      decision.Usage.Cost,
		})
		return rc.Carry{Text: decision.Content}, nil
	}}

	result, err := ReplaySession(rc.NewStore(t.TempDir()), cfg, bands, session, arm)
	if err != nil {
		t.Fatalf("replay with a live summary at every fork: %v", err)
	}
	if len(recorded.Calls) != len(result.Forks) {
		t.Fatalf("%d forks and %d summaries", len(result.Forks), len(recorded.Calls))
	}
	for i, call := range recorded.Calls {
		recorded.Calls[i].Step = result.Forks[i].Step
		t.Logf("fork at step %d: %s answered in %d ms, %d in, %d out, $%.6f, carry %d tokens",
			result.Forks[i].Step, call.Model, call.LatencyMS, call.InputTokens, call.OutputTokens, call.CostUSD,
			cfg.Tokens(call.Text))
	}

	body, err := json.MarshalIndent(recorded, "", "  ")
	if err != nil {
		t.Fatalf("marshal the recorded summaries: %v", err)
	}
	path := filepath.Join("testdata", "fork-summaries.json")
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
	t.Logf("wrote bench/recall/%s, %d calls, $%.6f in all", path, len(recorded.Calls), recorded.TotalCostUSD())
}
