package turn

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"tofu/internal/llm"
	"tofu/internal/recall"
)

const oneBigToolResultBytes = 40000

func turnWithOneBigToolResult(t *testing.T, wire string) Row {
	t.Helper()
	tool := &stubTool{name: "read", result: Result{Content: strings.Repeat("x", oneBigToolResultBytes), Command: "read big.txt"}}
	model := &stubModel{decisions: []llm.Decision{
		toolCallDecision(llm.ToolCall{ID: "call-1", Name: "read", Arguments: json.RawMessage(`{"path":"big.txt"}`)}),
		messageDecision(),
	}}
	config := baseConfig(t, model, NewRegistry(tool))
	config.Wire = wire
	config.ResultBytesCap = oneBigToolResultBytes * 2

	row, err := Run(context.Background(), config)
	if err != nil {
		t.Fatalf("run on %s: %v", wire, err)
	}
	if len(row.Steps) != 2 {
		t.Fatalf("the turn recorded %d steps, want the tool call and the answer", len(row.Steps))
	}
	for _, step := range row.Steps {
		if step.Occupancy == nil {
			t.Fatalf("step %d measured nothing, so no row says what it sent", step.Index)
		}
	}
	return row
}

func TestAStepRecordsTheOccupancyOfTheRequestItSentRatherThanTheOneAfterItsToolResult(t *testing.T) {
	row := turnWithOneBigToolResult(t, "anthropic")
	cfg, err := recall.LoadConfig()
	if err != nil {
		t.Fatalf("the elide library: %v", err)
	}
	result := cfg.OnWire("anthropic").Tokens(strings.Repeat("x", oneBigToolResultBytes))
	asked, answered := row.Steps[0].Occupancy.Total(), row.Steps[1].Occupancy.Total()

	if asked >= result {
		t.Fatalf("step 1 recorded the request it sent at %d tokens, and the %d token result it had not received yet is already inside that number",
			asked, result)
	}
	if answered-asked < result {
		t.Fatalf("the %d token result moved the recorded occupancy by %d tokens, from %d to %d",
			result, answered-asked, asked, answered)
	}
	t.Logf("step 1 sent %d tokens, step 2 sent %d, and the tool result between them is %d", asked, answered, result)
}

func TestARecordedOccupancyReadsBackWithItsWorkingSetIntact(t *testing.T) {
	written := StepRow{Index: 1, Occupancy: &recall.Occupancy{Identity: 1200, Facts: 340, WorkingSet: 19000, Recent: 31483, Target: 50000}}
	raw, err := json.Marshal(written)
	if err != nil {
		t.Fatalf("marshal a measured step: %v", err)
	}
	for _, band := range []string{`"identity":1200`, `"facts":340`, `"working_set":19000`, `"recent":31483`, `"target":50000`} {
		if !strings.Contains(string(raw), band) {
			t.Fatalf("the recorded occupancy is %s and carries no %s", raw, band)
		}
	}

	var read StepRow
	if err := json.Unmarshal(raw, &read); err != nil {
		t.Fatalf("read it back: %v", err)
	}
	if read.Occupancy == nil || *read.Occupancy != *written.Occupancy {
		t.Fatalf("wrote %+v and read %+v", *written.Occupancy, read.Occupancy)
	}
	t.Logf("%s", raw)
}

func TestTheWireASessionRunsOnReachesTheTokenEstimate(t *testing.T) {
	cfg, err := recall.LoadConfig()
	if err != nil {
		t.Fatalf("the elide library: %v", err)
	}
	onAnthropic, onCodex := cfg.OnWire("anthropic").BytesPerThousandTokens, cfg.OnWire("codex").BytesPerThousandTokens
	if onAnthropic == onCodex {
		t.Skipf("the elide library reads both wires at %d bytes per thousand tokens, so no wire can move the estimate", onCodex)
	}

	anthropic := turnWithOneBigToolResult(t, "anthropic").Steps[1].Occupancy.Total()
	codex := turnWithOneBigToolResult(t, "codex").Steps[1].Occupancy.Total()
	if codex >= anthropic {
		t.Fatalf("the same conversation measures %d tokens on codex at %d bytes per thousand and %d on anthropic at %d, so the wire never reached the estimate",
			codex, onCodex, anthropic, onAnthropic)
	}
	t.Logf("the same request measures %d tokens on anthropic at %d bytes per thousand and %d on codex at %d",
		anthropic, onAnthropic, codex, onCodex)
}
