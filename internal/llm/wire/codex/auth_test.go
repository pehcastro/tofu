package codex_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"tofu/internal/llm"
	"tofu/internal/llm/wire/codex"
	"tofu/internal/transport"
)

func askRevoked(t *testing.T, revoked ...string) (codex.Result, int32, error) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		bearer := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		for _, token := range revoked {
			if bearer == token {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":{"message":"token revoked"}}`))
				return
			}
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprintf(w, "data: %s\n\ndata: %s\n\n",
			`{"type":"response.output_text.delta","delta":"answered by `+bearer+`"}`,
			`{"type":"response.completed","response":{"id":"resp_1","model":"gpt-test","status":"completed"}}`)
	}))
	t.Cleanup(server.Close)
	wire, err := codex.New(codex.Config{BaseURL: server.URL, Model: "gpt-test", Token: func(_ context.Context, rejected string) (string, error) {
		if rejected == "" {
			return "stale", nil
		}
		return "renewed", nil
	}})
	if err != nil {
		t.Fatalf("new wire: %v", err)
	}
	result, _, err := wire.Ask(context.Background(), codex.Request{Messages: []llm.Message{{Role: llm.RoleUser, Content: "hi"}}})
	return result, calls.Load(), err
}

func TestA401OnAFreshCodexTokenRenewsItAndSendsOnceMore(t *testing.T) {
	result, calls, err := askRevoked(t, "stale")
	if err != nil || result.Content != "answered by renewed" || calls != 2 {
		t.Fatalf("a 401 then a 200 gave %q, %v, after %d posts", result.Content, err, calls)
	}
}

func TestA401OnTheRenewedCodexTokenIsFatalAfterOneResend(t *testing.T) {
	_, calls, err := askRevoked(t, "stale", "renewed")
	if transport.KindOf(err) != transport.KindAuth || calls != 2 {
		t.Fatalf("two 401s ended with %v after %d posts", err, calls)
	}
}
