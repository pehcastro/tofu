package turn

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"tofu/internal/llm"
)

type replayedRead map[string]Result

func (r replayedRead) Name() string { return "read" }

func (r replayedRead) Definition() llm.Tool { return llm.Tool{Name: "read"} }

func (r replayedRead) Run(_ context.Context, raw json.RawMessage) (Result, error) {
	var args struct{ Call string }
	err := json.Unmarshal(raw, &args)
	return r[args.Call], err
}

func readOf(id string) llm.ToolCall {
	return llm.ToolCall{ID: id, Name: "read", Arguments: json.RawMessage(`{"path":"a.go","call":"` + id + `"}`)}
}

type sentAs struct {
	whole     string
	pointedAt string
}

func TestARepeatedReadPointsAtTheCopyTheNextRequestStillCarries(t *testing.T) {
	file := strings.Repeat("func f() int { return 1 }\n", 40)
	first, repeat := Result{Content: file}, Result{Content: file, Repeat: true}
	rows := []struct {
		name      string
		answers   replayedRead
		decisions []llm.Decision
		want      []sentAs
	}{
		{"a repeat in a later step", replayedRead{"call-1": first, "call-2": repeat},
			[]llm.Decision{toolCallDecision(readOf("call-1")), toolCallDecision(readOf("call-2")), messageDecision()},
			[]sentAs{{whole: file}, {pointedAt: "call-1"}}},
		{"a repeat in the same parallel wave", replayedRead{"call-1": first, "call-2": repeat},
			[]llm.Decision{toolCallDecision(readOf("call-1"), readOf("call-2")), messageDecision()},
			[]sentAs{{whole: file}, {pointedAt: "call-1"}}},
		{"a repeat whose copy this history never held", replayedRead{"call-1": repeat},
			[]llm.Decision{toolCallDecision(readOf("call-1")), messageDecision()},
			[]sentAs{{whole: file}}},
		{"a repeat whose earlier copy is not word for word the text", replayedRead{"call-1": {Content: file[:100]}, "call-2": repeat},
			[]llm.Decision{toolCallDecision(readOf("call-1")), toolCallDecision(readOf("call-2")), messageDecision()},
			[]sentAs{{whole: file[:100]}, {whole: file}}},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			model := &stubModel{decisions: row.decisions}
			_, err := Run(context.Background(), Config{Model: model, Spend: SpendAPIKey, Tools: NewRegistry(row.answers),
				Task: "read a.go", ResultBytesCap: 1 << 20, ArtifactDir: t.TempDir(), NoLastWord: true})
			if err != nil {
				t.Fatal(err)
			}
			var sent []string
			for _, message := range model.requests[len(model.requests)-1].Messages {
				if message.Role == llm.RoleTool {
					sent = append(sent, message.Content)
				}
			}
			if len(sent) != len(row.want) {
				t.Fatalf("the last request carries %d tool results, want %d", len(sent), len(row.want))
			}
			for i, want := range row.want {
				if want.whole != "" && !strings.HasSuffix(sent[i], want.whole) {
					t.Errorf("result %d is %q, want it to end with the whole text", i+1, sent[i])
				}
				if want.pointedAt != "" && (strings.Contains(sent[i], "\n") || !strings.Contains(sent[i], want.pointedAt)) {
					t.Errorf("result %d is %q, want one line naming %s", i+1, sent[i], want.pointedAt)
				}
			}
		})
	}
}
