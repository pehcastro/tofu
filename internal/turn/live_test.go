package turn

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"boji/internal/judge/jev"
	"boji/internal/llm"
	"boji/internal/llm/wire/openrouter"
	"boji/internal/transport"
)

const liveAttemptMillis = 30000

func TestLiveRunCompletesATrivialTask(t *testing.T) {
	if os.Getenv("BOJI_LIVE") != "1" {
		t.Skip("set BOJI_LIVE=1 to call the real route")
	}
	key, err := jev.Key("../../.env")
	if err != nil {
		t.Fatalf("no credential: %v", err)
	}

	root := t.TempDir()
	readTool, err := NewReadTool(root)
	if err != nil {
		t.Fatalf("building the read tool: %v", err)
	}
	writeTool, err := NewWriteTool(root)
	if err != nil {
		t.Fatalf("building the write tool: %v", err)
	}
	bashTool, err := NewBashTool(root)
	if err != nil {
		t.Fatalf("building the bash tool: %v", err)
	}

	wire, err := openrouter.New(openrouter.Config{
		Model: "anthropic/claude-fable-5-1",
		Key:   key,
		Transport: transport.Config{
			AttemptTimeout: liveAttemptMillis * time.Millisecond,
			Retries:        1,
			Backoff:        200 * time.Millisecond,
			MaxBackoff:     2 * time.Second,
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

	row, err := Run(context.Background(), Config{
		Model: client,
		Tools: NewRegistry(readTool, writeTool, bashTool),
		Task: "Using the write tool, write a file named hello.txt containing exactly the text " +
			"hello from boji and nothing else. After it is written, reply with a short " +
			"confirmation message in plain text and do not call any more tools.",
		Caps:           Caps{MaxSteps: 5, MaxWallClock: 60 * time.Second},
		ResultBytesCap: 4096,
	})
	if err != nil {
		t.Fatalf("the live run failed: %v", err)
	}

	t.Logf("turn %s outcome %s model %s cost $%.6f wall_clock_ms %d", row.ID, row.Outcome, row.Model, row.TotalCostUSD, row.WallClockMS)
	for _, step := range row.Steps {
		t.Logf("step %d assistant_text %q cost $%.6f", step.Index, step.AssistantText, step.CostUSD)
		for _, call := range step.ToolCalls {
			t.Logf("  tool_call tool=%s command=%q exit_code=%v error=%q", call.Tool, call.Command, call.ExitCode, call.Error)
		}
	}

	if row.Outcome != OutcomeStopped {
		t.Fatalf("expected the turn to stop cleanly, got %s", row.Outcome)
	}
	written, err := os.ReadFile(filepath.Join(root, "hello.txt"))
	if err != nil {
		t.Fatalf("the task did not produce hello.txt: %v", err)
	}
	t.Logf("hello.txt content %q", string(written))
}
