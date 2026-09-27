package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"testing"
)

func TestLegacyTurnsWithoutMessagesEachKeepTheirOwnToolResults(t *testing.T) {
	const id = "turn-18d6ea32da3230c0"
	dir := filepath.Join(t.TempDir(), id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	step := func(bytes int) string {
		return `{"kind":"step","body":{"index":1,"tool_calls":[{"tool":"bash","args":{"command":"ls"},"result_bytes":` + strconv.Itoa(bytes) + `,"rendered_bytes":` + strconv.Itoa(bytes) + `}]}}`
	}
	outcome := `{"kind":"outcome","body":{"id":"` + id + `","task":"t","wire":"anthropic","outcome":"stopped"}}`
	body := step(2210) + "\n" + outcome + "\n" + step(702) + "\n" + outcome + "\n"
	if err := os.WriteFile(filepath.Join(dir, legacyBodyName), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, legacyHeaderName), []byte(`{"id":"`+id+`","task":"t","wire":"anthropic"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	events, err := NewStore(filepath.Dir(dir)).Body(id)
	if err != nil {
		t.Fatalf("body: %v", err)
	}
	var rendered []int
	for _, event := range events {
		if event.Kind != EventStep {
			continue
		}
		var parsed struct {
			ToolCalls []struct {
				RenderedBytes int `json:"rendered_bytes"`
			} `json:"tool_calls"`
		}
		if err := json.Unmarshal(event.Body, &parsed); err != nil {
			t.Fatalf("step: %v", err)
		}
		for _, call := range parsed.ToolCalls {
			rendered = append(rendered, call.RenderedBytes)
		}
	}
	if want := []int{2210, 702}; !slices.Equal(rendered, want) {
		t.Fatalf("each turn's step 1 reads back its own result: got %v, want %v", rendered, want)
	}
}
