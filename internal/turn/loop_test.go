package turn

import (
	"context"
	"encoding/json"
	"slices"
	"testing"
	"time"

	"tofu/internal/judge/ledger"
	"tofu/internal/llm"
)

type slowGate struct{ took time.Duration }

func (g slowGate) Decide(context.Context, GateRequest) (GateDecision, error) {
	time.Sleep(g.took)
	return GateDecision{ID: "shadow-row", Verdict: ledger.VerdictAllow}, nil
}

type timedModel struct {
	stubModel
	asked []time.Time
}

func (m *timedModel) Ask(ctx context.Context, request llm.Request) (llm.Decision, error) {
	m.asked = append(m.asked, time.Now())
	return m.stubModel.Ask(ctx, request)
}

func TestAShadowGateThatTakes300MillisecondsAddsNoTimeToAStep(t *testing.T) {
	model := &timedModel{stubModel: stubModel{decisions: []llm.Decision{
		toolCallDecision(llm.ToolCall{ID: "call-1", Name: "noop", Arguments: json.RawMessage(`{}`)}),
		messageDecision(),
	}}}
	row, err := Run(context.Background(), Config{Model: model, Spend: SpendAPIKey, Tools: NewRegistry(namedTool("noop")), Task: "run noop",
		Gate: slowGate{took: 300 * time.Millisecond}, GateMode: GateShadow, ResultBytesCap: 4096, ArtifactDir: t.TempDir(), NoLastWord: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(model.asked) != 2 {
		t.Fatalf("the model was asked %d times, want 2", len(model.asked))
	}
	if step := model.asked[1].Sub(model.asked[0]); step >= 150*time.Millisecond {
		t.Errorf("the step between two asks took %s with a 300 ms shadow gate, want it not to wait on the gate", step)
	}
	if !slices.Contains(row.DecisionIDs, "shadow-row") {
		t.Errorf("the turn row does not carry the shadow decision: %v", row.DecisionIDs)
	}
}
