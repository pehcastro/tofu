package openrouter_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"tofu/internal/judge/jev"
	"tofu/internal/judge/jev/wire/openrouter"
	"tofu/internal/konst"
	"tofu/internal/transport"
)

const fastAPIDetail = `{"detail":[{"loc":["body","questions","urgency","score","criteria"],"msg":"Field required","type":"missing","input":{"type":"score"},"ctx":{"min_length":1}}]}`

func TestAnUnprocessableRequestKeepsTheServersOwnDetail(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(fastAPIDetail))
	}))
	defer server.Close()

	wire, err := openrouter.New(openrouter.Config{
		Endpoint:  server.URL,
		Key:       "test-key",
		Transport: transport.Config{AttemptTimeout: 2 * time.Second, Retries: 0, Backoff: time.Millisecond, Concurrency: 1},
	})
	if err != nil {
		t.Fatalf("building the wire: %v", err)
	}
	client, err := jev.NewClient(jev.Config{Wire: wire})
	if err != nil {
		t.Fatalf("building the client: %v", err)
	}
	_, err = client.Ask(context.Background(), jev.Request{
		State:     map[string]any{"tool": "bash", "input": "ls -la"},
		Questions: []jev.Question{{ID: "risk", Kind: jev.QuestionNoul, Instructions: "how risky is this call", True: "risky", False: "safe"}},
	})
	if err == nil {
		t.Fatal("expected the 422 to reach the caller")
	}
	var failure *transport.Error
	if !errors.As(err, &failure) {
		t.Fatalf("expected a transport error, got %T", err)
	}
	if failure.Status != http.StatusUnprocessableEntity {
		t.Fatalf("expected status 422, got %d", failure.Status)
	}
	if transport.KindOf(err) != transport.KindBadRequest {
		t.Fatalf("expected kind bad_request, got %s", transport.KindOf(err))
	}
	if len(fastAPIDetail) > konst.TransportErrorDetailBytes {
		t.Fatalf("this fixture is %d bytes and the detail is capped at %d", len(fastAPIDetail), konst.TransportErrorDetailBytes)
	}
	if failure.Detail != fastAPIDetail {
		t.Fatalf("the detail arrived as %q", failure.Detail)
	}
	if !strings.Contains(err.Error(), `"loc":["body","questions","urgency","score","criteria"]`) {
		t.Fatalf("the message lost the field path: %v", err)
	}
	if calls != 1 {
		t.Fatalf("expected one call, the server saw %d", calls)
	}
}
