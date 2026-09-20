package models

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"tofu/internal/llm/wire/codex"
	"tofu/internal/transport"
)

func testClient(t *testing.T) *transport.Client {
	t.Helper()
	client, err := transport.New(transport.Config{AttemptTimeout: time.Second, Concurrency: 1})
	if err != nil {
		t.Fatalf("building the transport: %v", err)
	}
	return client
}

func TestDiscoverReportsTheCodexPinBesideTheResult(t *testing.T) {
	var sentVersion, sentQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sentVersion = r.Header.Get(codex.HeaderVersion)
		sentQuery = r.URL.Query().Get("client_version")
		_, _ = w.Write([]byte(`{"models":[{"slug":"gpt-5.6-sol"},{"slug":"gpt-6-astra"}]}`))
	}))
	defer server.Close()

	served, err := Discover(context.Background(), testClient(t), Account{
		Subscription: Codex,
		BaseURL:      server.URL + "/codex/models?client_version=" + codex.PinnedCodexClientVersion,
		Token:        func(context.Context) (string, error) { return "token", nil },
	})
	if err != nil {
		t.Fatalf("discovering: %v", err)
	}
	if sentVersion != codex.PinnedCodexClientVersion || sentQuery != codex.PinnedCodexClientVersion {
		t.Fatalf("the pin must gate discovery, header %q query %q", sentVersion, sentQuery)
	}
	catalog, err := Load([]Layer{layerOf("catalog", withSubscriptions(fstest.MapFS{
		"models/openai/gpt-5.6-sol.yaml":      &fstest.MapFile{Data: []byte("subscription: codex\nuse: default\n")},
		"models/openai/gpt-5.6-vanished.yaml": &fstest.MapFile{Data: []byte("subscription: codex\nuse: allowed\n")},
	}))})
	if err != nil {
		t.Fatalf("loading: %v", err)
	}
	lines := catalog.Reconcile(served).Lines()
	if !strings.Contains(lines[0], "under codex client version "+codex.PinnedCodexClientVersion) {
		t.Fatalf("the pin must be reported beside the list, got %q", lines[0])
	}
	if !strings.Contains(lines[1], "gpt-6-astra") {
		t.Fatalf("a served model the catalog does not know must be named, got %q", lines[1])
	}
	if !strings.Contains(lines[2], "gpt-5.6-vanished") {
		t.Fatalf("a catalog model the account cannot reach must be named, got %q", lines[2])
	}
}

func TestReconcileRendersAnEmptyListRatherThanNothing(t *testing.T) {
	catalog, err := Load([]Layer{layerOf("catalog", withSubscriptions(oneFile(
		"models/openai/gpt-5.6-sol.yaml", "subscription: codex\nuse: default\n")))})
	if err != nil {
		t.Fatalf("loading: %v", err)
	}
	lines := catalog.Reconcile(Served{Subscription: Codex, Pin: "pin", IDs: []string{"gpt-5.6-sol"}}).Lines()
	for _, line := range lines[1:] {
		if !strings.HasSuffix(line, "(none)") {
			t.Fatalf("an empty finding is still a line, got %q", line)
		}
	}
}

func TestDiscoverReadsTheAnthropicListShape(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"id":"claude-opus-5"},{"id":"claude-sonnet-5"}]}`))
	}))
	defer server.Close()

	served, err := Discover(context.Background(), testClient(t), Account{
		Subscription: Claude,
		BaseURL:      server.URL,
		Token:        func(context.Context) (string, error) { return "token", nil },
	})
	if err != nil {
		t.Fatalf("discovering: %v", err)
	}
	if strings.Join(served.IDs, ",") != "claude-opus-5,claude-sonnet-5" {
		t.Fatalf("served ids are %v", served.IDs)
	}
}
