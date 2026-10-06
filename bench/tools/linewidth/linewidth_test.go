package linewidth

import (
	"encoding/json"
	"os"
	"testing"

	"tofu/bench/corpus"
	"tofu/internal/konst"
	"tofu/internal/sys"
	"tofu/internal/turn"
)

type recordedCall struct {
	tool string
	args json.RawMessage
}

func TestLineCapOverRecordedToolResults(t *testing.T) {
	if os.Getenv("TOFU_TOOLS_LINEWIDTH") != "1" {
		t.Skip("set TOFU_TOOLS_LINEWIDTH=1 to replay every recorded tool result through the line cap")
	}
	walked, err := corpus.WalkSessions(sys.RecordedStateDir("sessions"))
	if err != nil {
		t.Fatal(err)
	}
	artifacts, err := turn.NewArtifacts(t.TempDir(), true)
	if err != nil {
		t.Fatal(err)
	}
	calls := map[string]recordedCall{}
	for _, recorded := range walked.Turns {
		for _, step := range recorded.Steps {
			for _, call := range step.ToolCalls {
				calls[call.Call] = recordedCall{call.Tool, call.Args}
			}
		}
	}
	seen := map[string]bool{}
	var results, wide, skippedWide, unnamed, bytes, saved int
	byTool := map[string]int{}
	for _, recorded := range walked.Turns {
		for _, message := range recorded.Messages {
			if message.Role != "tool" || seen[message.ToolCallID] {
				continue
			}
			seen[message.ToolCallID] = true
			call, named := calls[message.ToolCallID]
			if !named {
				unnamed++
				continue
			}
			results++
			bytes += len(message.Content)
			capped, _, err := artifacts.Render(call.tool, call.args, message.Content, len(message.Content))
			if err != nil {
				t.Fatal(err)
			}
			if capped != message.Content {
				wide++
				byTool[call.tool]++
				saved += len(message.Content) - len(capped)
				continue
			}
			if uncapped, _, _ := artifacts.Render("bash", nil, message.Content, len(message.Content)); uncapped != message.Content {
				skippedWide++
			}
		}
	}
	t.Logf("turns %d skipped %d, tool results %d with their call recorded, %d without", len(walked.Turns), len(walked.Skipped), results, unnamed)
	t.Logf("results with a line over %d bytes the cap cuts: %d of %d, %.2f%%", konst.TurnResultLineWidth, wide, results, 100*float64(wide)/float64(max(results, 1)))
	t.Logf("results with a wide line in a tool the cap skips: %d", skippedWide)
	t.Logf("cut by tool: %v", byTool)
	t.Logf("tokens the cap saves on them: %d of %d tool result tokens, %.2f%%, at %d bytes a token",
		saved/konst.SearchBytesPerToken, bytes/konst.SearchBytesPerToken, 100*float64(saved)/float64(max(bytes, 1)), konst.SearchBytesPerToken)
}
