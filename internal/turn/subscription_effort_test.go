package turn

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"tofu/internal/llm"
	"tofu/internal/llm/wire/anthropic"
)

func TestTheSubscriptionSendsItsEffortOnEveryStep(t *testing.T) {
	var sent []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sent, _ = io.ReadAll(r.Body)
		stream, err := os.ReadFile(filepath.Join("testdata", "subscription-end-turn.sse"))
		if err != nil {
			t.Errorf("reading the recorded stream: %v", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write(stream)
	}))
	t.Cleanup(server.Close)

	wire, err := anthropic.New(anthropic.Config{
		BaseURL: server.URL,
		Proxy:   true,
		Model:   "claude-sonnet-4-5-20250929",
		Token:   func(context.Context) (string, error) { return "sk-ant-oat01-recorded", nil },
	})
	if err != nil {
		t.Fatalf("building the wire: %v", err)
	}

	model := Subscription{Wire: wire, Effort: llm.EffortMedium}
	if _, err := model.Ask(t.Context(), llm.Request{Messages: []llm.Message{{Role: llm.RoleUser, Content: "write a note"}}}); err != nil {
		t.Fatalf("asking: %v", err)
	}

	var body struct {
		Output *struct {
			Effort string `json:"effort"`
		} `json:"output_config"`
	}
	if err := json.Unmarshal(sent, &body); err != nil {
		t.Fatalf("the sent body is not json: %v", err)
	}
	if body.Output == nil || body.Output.Effort != string(llm.EffortMedium) {
		t.Fatalf("the step was sent at %+v, want output_config.effort %q", body.Output, llm.EffortMedium)
	}
}
