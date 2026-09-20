package models

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func servedFrom(t *testing.T, subscription Subscription, payload string) Served {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("testdata", payload))
	if err != nil {
		t.Fatalf("reading the recorded %s payload: %v", payload, err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(body)
	}))
	defer server.Close()

	served, err := Discover(context.Background(), testClient(t), Account{
		Subscription: subscription,
		BaseURL:      server.URL,
		Token:        func(context.Context) (string, error) { return "token", nil },
	})
	if err != nil {
		t.Fatalf("discovering against the recorded %s payload: %v", payload, err)
	}
	return served
}

func TestTheCodexListReportsAWindowPerModel(t *testing.T) {
	served := servedFrom(t, Codex, "codex-models.json")
	for slug, want := range map[string]int{"gpt-6-astra": 272000, "gpt-5.6-sol": 272000, "gpt-5.5": 272000} {
		if served.Windows[slug] != want {
			t.Fatalf("%s came back with a %d token window, want the %d the payload carries", slug, served.Windows[slug], want)
		}
	}
	if tokens, reported := served.Windows["gpt-reserve"]; reported {
		t.Fatalf("a model whose context_window is null came back with %d tokens", tokens)
	}
	t.Logf("%d of %d served models report a window", len(served.Windows), len(served.IDs))
}

func TestTheAnthropicListReportsNoWindowAndTheProxyShapeDoes(t *testing.T) {
	official := servedFrom(t, Claude, "anthropic-models.json")
	if len(official.Windows) != 0 {
		t.Fatalf("the anthropic model list came back with windows %v, and its entries carry id, display_name and created_at alone", official.Windows)
	}
	proxy := servedFrom(t, Claude, "anthropic-proxy-models.json")
	if proxy.Windows["claude-haiku-4-5-20251001"] != 200000 {
		t.Fatalf("an anthropic shaped list reporting max_input_tokens came back with %v", proxy.Windows)
	}
	t.Logf("the official list reports no window and a list that carries max_input_tokens reports %v", proxy.Windows)
}

func TestTheCatalogTheRegistryAndTheAccountResolveInThatOrder(t *testing.T) {
	catalog := shippedCatalog(t)
	registry, err := ShippedRegistry()
	if err != nil {
		t.Fatalf("the shipped registry snapshot: %v", err)
	}
	served := servedFrom(t, Codex, "codex-models.json")

	stated, err := catalog.Select("anthropic/claude-haiku-4-5-20251001")
	if err != nil {
		t.Fatalf("selecting a model the catalog states a window for: %v", err)
	}
	if tokens, source := WindowFor(stated, registry, served); tokens != 200000 || !strings.Contains(source, "catalog") {
		t.Fatalf("%s resolved to %d tokens %q, want the 200000 the catalog states", stated.Slug(), tokens, source)
	}

	listed, err := catalog.Select("openai/gpt-5.6-sol")
	if err != nil {
		t.Fatalf("selecting the codex default: %v", err)
	}
	if listed.ContextTokens != 0 {
		t.Fatalf("the catalog states %d tokens for %s, and this case needs the model it states nothing for", listed.ContextTokens, listed.Slug())
	}
	tokens, source := WindowFor(listed, registry, served)
	if tokens != 1050000 || !strings.Contains(source, "models.dev") {
		t.Fatalf("%s resolved to %d tokens %q, want the 1050000 the registry lists", listed.Slug(), tokens, source)
	}

	custom := Model{Provider: OpenAI, ID: "gpt-custom", Subscription: Codex}
	reported := Served{Subscription: Codex, Pin: "codex client version 0.153.0", Windows: map[string]int{"gpt-custom": 300000}}
	if tokens, source := WindowFor(custom, registry, reported); tokens != 300000 || !strings.Contains(source, "account") {
		t.Fatalf("a model in neither the catalog nor the registry resolved to %d tokens %q, want the 300000 the account reports", tokens, source)
	}
	if tokens, source := WindowFor(custom, registry, Served{}); tokens != 0 || source != "" {
		t.Fatalf("a model none of the three knows resolved to %d tokens %q", tokens, source)
	}
	t.Logf("catalog 200000, registry %d %s, account 300000, none 0", tokens, source)
}

func TestADatedCatalogIdStillFindsTheUndatedRegistryEntry(t *testing.T) {
	registry, err := ShippedRegistry()
	if err != nil {
		t.Fatalf("the shipped registry snapshot: %v", err)
	}
	if tokens := registry.Window("anthropic/claude-haiku-4-5-20251001"); tokens != 200000 {
		t.Fatalf("a dated catalog id resolved to %d tokens against a registry that lists claude-haiku-4-5", tokens)
	}
	if tokens := registry.Window("anthropic/claude-haiku-4-5-2025100x"); tokens != 0 {
		t.Fatalf("a suffix that is not a date was cut off anyway and resolved to %d tokens", tokens)
	}
}

func TestWhichSourceAnsweredForEveryModelWeShip(t *testing.T) {
	catalog := shippedCatalog(t)
	registry, err := ShippedRegistry()
	if err != nil {
		t.Fatalf("the shipped registry snapshot: %v", err)
	}
	served := map[Subscription]Served{
		Codex:  servedFrom(t, Codex, "codex-models.json"),
		Claude: servedFrom(t, Claude, "anthropic-models.json"),
	}
	answered := 0
	for _, model := range catalog.Models {
		tokens, source := WindowFor(model, registry, served[model.Subscription])
		if tokens > 0 {
			answered++
		}
		t.Logf("%-40s %8d %s", model.Slug(), tokens, source)
	}
	if answered == len(catalog.Models) {
		t.Fatalf("all %d models have a window, and the case worth watching is the one that has none", answered)
	}
	t.Logf("%d of %d models the catalog ships resolve to a window", answered, len(catalog.Models))
}

func TestReconcileNamesAWindowTheCatalogDoesNotCarry(t *testing.T) {
	lines := shippedCatalog(t).Reconcile(servedFrom(t, Codex, "codex-models.json")).Lines()
	last := lines[len(lines)-1]
	for _, want := range []string{"openai/gpt-5.6-sol 272000", "openai/gpt-6-astra 272000", "openai/gpt-5.5 272000"} {
		if !strings.Contains(last, want) {
			t.Fatalf("the reconciliation does not name %q, so a window the account reports stays invisible until somebody edits a file:\n%s", want, last)
		}
	}
	t.Logf("%s", last)
}
