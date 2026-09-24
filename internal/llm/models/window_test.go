package models

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"tofu/internal/transport"
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

func shippedTable(t *testing.T) Registry {
	t.Helper()
	registry, err := ShippedRegistry()
	if err != nil {
		t.Fatalf("parsing the shipped snapshot of models.dev: %v", err)
	}
	return registry
}

func windowsTheModelFilesCarriedByHand() map[string]int {
	return map[string]int{
		"claude-sub/claude-opus-5":              1000000,
		"claude-sub/claude-opus-4-5-20251101":   200000,
		"claude-sub/claude-sonnet-4-5-20250929": 200000,
		"claude-sub/claude-haiku-4-5-20251001":  200000,
		"codex-sub/gpt-5.5":                     400000,
	}
}

func windowsThePublishedTableCorrects() map[string]int {
	return map[string]int{
		"claude-sub/claude-sonnet-4-5-20250929": 1000000,
		"codex-sub/gpt-5.5":                     1050000,
	}
}

func TestTheCodexListReportsAWindowPerModel(t *testing.T) {
	served := servedFrom(t, CodexSub, "codex-models.json")
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
	official := servedFrom(t, ClaudeSub, "anthropic-models.json")
	if len(official.Windows) != 0 {
		t.Fatalf("the anthropic model list came back with windows %v, and its entries carry id, display_name and created_at alone", official.Windows)
	}
	proxy := servedFrom(t, ClaudeSub, "anthropic-proxy-models.json")
	if proxy.Windows["claude-haiku-4-5-20251001"] != 200000 {
		t.Fatalf("an anthropic shaped list reporting max_input_tokens came back with %v", proxy.Windows)
	}
	t.Logf("the official list reports no window and a list that carries max_input_tokens reports %v", proxy.Windows)
}

func TestThePublishedTableAnswersBeforeTheAccountDoes(t *testing.T) {
	library, registry := shippedLibrary(t), shippedTable(t)
	served := servedFrom(t, CodexSub, "codex-models.json")
	if served.Windows["gpt-5.6-sol"] == registry.Window("openai/gpt-5.6-sol") {
		t.Fatal("the account and the table agree on the codex default, so this proves no order")
	}

	listed, err := library.Select("codex-sub/gpt-5.6-sol")
	if err != nil {
		t.Fatalf("selecting the codex default: %v", err)
	}
	tokens, source := WindowFor(listed, registry, served)
	if tokens != 1050000 || !strings.Contains(source, "models.dev") {
		t.Fatalf("%s resolved to %d tokens %q, want the 1050000 the table publishes", listed.Slug(), tokens, source)
	}

	custom := Model{Provider: OpenAI, ID: "gpt-custom", Subscription: CodexSub}
	reported := Served{Subscription: CodexSub, Pin: "codex client version 0.153.0", Windows: map[string]int{"gpt-custom": 300000}}
	if tokens, source := WindowFor(custom, registry, reported); tokens != 300000 || !strings.Contains(source, "account") {
		t.Fatalf("a model no table lists resolved to %d tokens %q, want the 300000 the account reports", tokens, source)
	}
	if tokens, source := WindowFor(custom, registry, Served{}); tokens != 0 || source != "" {
		t.Fatalf("a model neither the table nor the account knows resolved to %d tokens %q", tokens, source)
	}
}

func TestEveryWindowTypedByHandNowComesFromThePublishedTable(t *testing.T) {
	library, registry := shippedLibrary(t), shippedTable(t)
	for slug, byHand := range windowsTheModelFilesCarriedByHand() {
		model, found := library.Resolve(slug)
		if !found {
			t.Fatalf("%s left the library with its window", slug)
		}
		want := byHand
		if published, corrected := windowsThePublishedTableCorrects()[slug]; corrected {
			want = published
		}
		tokens, source := WindowFor(model, registry, Served{})
		if tokens != want {
			t.Fatalf("%s answers %d tokens, want the %d the table publishes", slug, tokens, want)
		}
		if !strings.Contains(source, registry.From) || !strings.Contains(source, model.VendorSlug()) {
			t.Fatalf("%s does not say it was joined on %s in %s: %q", slug, model.VendorSlug(), registry.From, source)
		}
	}
}

func TestTheTableIsJoinedOnTheVendorFormAndTheSlugStaysThePayerForm(t *testing.T) {
	library, err := Load([]Layer{layerOf("library", withSubscriptions(oneFile(
		"models/openai/gpt-5.6-sol.yaml", "subscription: codex-sub\nuse: default\n")))})
	if err != nil {
		t.Fatalf("loading: %v", err)
	}
	registry, err := ParseRegistry([]byte(
		`{"openai":{"models":{"gpt-5.6-sol":{"limit":{"context":1050000}}}},`+
			`"codex":{"models":{"gpt-5.6-sol":{"limit":{"context":7}}}}}`), "a table keyed both ways")
	if err != nil {
		t.Fatalf("parsing: %v", err)
	}
	model, found := library.Resolve("codex-sub/gpt-5.6-sol")
	if !found || model.Slug() != "codex-sub/gpt-5.6-sol" || model.VendorSlug() != "openai/gpt-5.6-sol" {
		t.Fatalf("the payer form and the vendor form are not both spelled out: %+v", model)
	}
	if tokens, _ := WindowFor(model, registry, Served{}); tokens != 1050000 {
		t.Fatalf("the join answered %d tokens, and 7 is the entry filed under the payer form", tokens)
	}
}

func TestADatedLibraryIdStillFindsTheUndatedRegistryEntry(t *testing.T) {
	registry, err := ParseRegistry([]byte(
		`{"anthropic":{"models":{"claude-haiku-4-5":{"limit":{"context":200000}}}}}`), "a table with no dated alias")
	if err != nil {
		t.Fatalf("parsing: %v", err)
	}
	if tokens := registry.Window("anthropic/claude-haiku-4-5-20251001"); tokens != 200000 {
		t.Fatalf("a dated library id resolved to %d tokens against a table that lists claude-haiku-4-5", tokens)
	}
	if tokens := registry.Window("anthropic/claude-haiku-4-5-2025100x"); tokens != 0 {
		t.Fatalf("a suffix that is not a date was cut off anyway and resolved to %d tokens", tokens)
	}
}

func TestEveryModelTheAccountCanSendTakesAWindowFromTheTable(t *testing.T) {
	library, registry := shippedLibrary(t), shippedTable(t)
	answered := 0
	for _, model := range library.Models {
		tokens, source := WindowFor(model, registry, Served{})
		if tokens > 0 {
			answered++
		}
		if model.Use != UseExcluded && tokens <= 0 {
			t.Fatalf("%s is sendable and no table entry gives it a window, so it would compact on a guess", model.Slug())
		}
		t.Logf("%-40s %8d %s", model.Slug(), tokens, source)
	}
	if answered == len(library.Models) {
		t.Fatalf("all %d models have a window, and the case worth watching is the one that has none", answered)
	}
	t.Logf("%d of %d models the library ships take a window from %s", answered, len(library.Models), registry.From)
}

func TestReconcileNamesAWindowThePublishedTableDoesNotMatch(t *testing.T) {
	lines := shippedLibrary(t).Reconcile(servedFrom(t, CodexSub, "codex-models.json"), shippedTable(t)).Lines()
	last := lines[len(lines)-1]
	for _, want := range []string{"codex-sub/gpt-5.6-sol 272000", "codex-sub/gpt-6-astra 272000", "codex-sub/gpt-5.5 272000"} {
		if !strings.Contains(last, want) {
			t.Fatalf("the reconciliation does not name %q, so a window the account reports stays invisible:\n%s", want, last)
		}
	}
	t.Logf("%s", last)
}

func TestAModelFileCannotCarryAContextWindowAtAll(t *testing.T) {
	_, err := Load([]Layer{layerOf("library", withSubscriptions(oneFile(
		"models/openai/gpt-5.6-sol.yaml", "subscription: codex-sub\nuse: default\ncontext_tokens: 400000\n")))})
	if err == nil || !strings.Contains(err.Error(), "context_tokens: unknown field") {
		t.Fatalf("a local file still carries an upstream fact, got %v", err)
	}
}

func testClient(t *testing.T) *transport.Client {
	t.Helper()
	client, err := transport.New(transport.Config{AttemptTimeout: time.Second, Concurrency: 1})
	if err != nil {
		t.Fatalf("building the transport: %v", err)
	}
	return client
}
