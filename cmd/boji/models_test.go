package main

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"boji/internal/llm"
	"boji/internal/llm/models"
	"boji/internal/llm/wire/anthropic"
)

func TestModelsVerbListsEveryModelWithItsDefaultAndItsReasons(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := modelsVerb(nil, &out, &errOut); code != exitOK {
		t.Fatalf("boji models exited %d: %s", code, errOut.String())
	}
	t.Logf("boji models\n%s", out.String())
	text := out.String()
	for _, want := range []string{
		"claude-opus-5 default", "gpt-5.6-sol default",
		"claude-fable-5-1 excluded", "gpt-6-astra excluded",
		"not fable or astra for now", "spends 5h and 7d",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("boji models does not report %q", want)
		}
	}
	for _, line := range strings.Split(strings.TrimSpace(text), "\n") {
		if strings.Contains(line, "excluded") && !strings.Contains(line, "reason: ") {
			t.Fatalf("an exclusion with no reason: %q", line)
		}
	}
}

func TestRunModelExcludedIsRefusedWithTheCatalogReason(t *testing.T) {
	_, err := selectModel("anthropic", "claude-fable-5-1")
	var refusal *models.Refusal
	if !errors.As(err, &refusal) || refusal.Kind != models.RefusedExcluded {
		t.Fatalf("want the catalog exclusion, got %v", err)
	}
	if !strings.Contains(err.Error(), "not fable or astra for now") {
		t.Fatalf("the refusal is generic: %v", err)
	}
}

func TestRunModelUnknownIsRefusedBeforeAnyRequest(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	ask := func(id string) error {
		model, err := selectModel("anthropic", id)
		if err != nil {
			return err
		}
		wire, err := anthropic.New(anthropic.Config{
			Model:   model.ID,
			BaseURL: server.URL,
			Proxy:   true,
			Token:   func(context.Context) (string, error) { return "sk-ant-oat-test", nil },
		})
		if err != nil {
			return err
		}
		_, _, err = wire.Ask(context.Background(), anthropic.Request{
			Messages: []llm.Message{{Role: llm.RoleUser, Content: "hello"}},
		})
		return err
	}

	if err := ask("claude-opus-latest"); err == nil || !strings.Contains(err.Error(), "claude-opus-latest") {
		t.Fatalf("an unknown model must be refused by name, got %v", err)
	}
	if calls != 0 {
		t.Fatalf("an unknown model reached the wire %d times", calls)
	}
	_ = ask("claude-opus-5")
	if calls != 1 {
		t.Fatalf("the counter never saw a request, so the zero above proves nothing: %d", calls)
	}
}

func TestLiveModelsDiscover(t *testing.T) {
	if os.Getenv("BOJI_LIVE_MODELS") != "1" {
		t.Skip("set BOJI_LIVE_MODELS=1 to ask both live subscriptions which models they serve")
	}
	var out, errOut bytes.Buffer
	code := modelsVerb([]string{"--discover"}, &out, &errOut)
	t.Logf("boji models --discover\n%s", out.String())
	if code != exitOK {
		t.Fatalf("boji models --discover exited %d: %s", code, errOut.String())
	}
}

func TestSelectModelRefusesTheKeyWireRatherThanGuessing(t *testing.T) {
	_, err := selectModel("openrouter", "anthropic/claude-opus-5")
	if err == nil || !strings.Contains(err.Error(), "money rather than a window") {
		t.Fatalf("want the key wire named as money, got %v", err)
	}
}
