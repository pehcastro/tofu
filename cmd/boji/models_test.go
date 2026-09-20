package main

import (
	"bytes"
	"context"
	"encoding/json"
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

func TestModelsAnswersWithTheDefaultsAndCountsWhatItCollapsed(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := modelsVerb(nil, &out, &errOut, plain); code != exitOK {
		t.Fatalf("boji models exited %d: %s", code, errOut.String())
	}
	t.Logf("boji models\n%s", out.String())
	text := out.String()
	first := strings.Split(text, "\n")[0]
	for _, want := range []string{"anthropic/claude-opus-5", "openai/gpt-5.6-sol", "usable"} {
		if !strings.Contains(first, want) {
			t.Fatalf("the first line %q does not answer with %q", first, want)
		}
	}
	if !strings.Contains(oneLine(text), "not fable or astra for now") {
		t.Fatalf("an exclusion lost its reason:\n%s", text)
	}
	for _, line := range strings.Split(strings.TrimSpace(text), "\n") {
		if !strings.Contains(line, "excluded") {
			continue
		}
		if count := strings.Fields(strings.TrimSpace(line))[0]; count == "excluded" {
			t.Fatalf("a collapsed exclusion does not say how many it collapsed: %q", line)
		}
	}
}

func TestModelsNamesEveryModelByProviderAndName(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := modelsVerb([]string{jsonFlag}, &out, &errOut, plain); code != exitOK {
		t.Fatalf("boji models --json exited %d: %s", code, errOut.String())
	}
	var report modelsReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("boji models --json does not parse: %v\n%s", err, out.String())
	}
	var text bytes.Buffer
	if code := modelsVerb(nil, &text, &errOut, plain); code != exitOK {
		t.Fatalf("boji models exited %d", code)
	}
	for _, model := range report.Models {
		if model.Slug != model.Provider+"/"+model.ID {
			t.Fatalf("the slug is not provider/name: %+v", model)
		}
		if model.Use == string(models.UseExcluded) {
			continue
		}
		if !strings.Contains(text.String(), model.Slug) {
			t.Fatalf("boji models never printed %q:\n%s", model.Slug, text.String())
		}
	}
}

func TestModelsJSONKeepsEveryModelTheTextCollapsed(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := modelsVerb([]string{jsonFlag}, &out, &errOut, plain); code != exitOK {
		t.Fatalf("boji models --json exited %d: %s", code, errOut.String())
	}
	var report modelsReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("boji models --json does not parse: %v\n%s", err, out.String())
	}
	var text bytes.Buffer
	if code := modelsVerb(nil, &text, &errOut, plain); code != exitOK {
		t.Fatalf("boji models exited %d", code)
	}
	excluded := 0
	for _, model := range report.Models {
		if model.Use == string(models.UseExcluded) {
			excluded++
		}
	}
	if printed := strings.Count(text.String(), "excluded"); printed >= excluded {
		t.Fatalf("the text printed %d exclusion lines for %d models, so it collapsed nothing", printed, excluded)
	}
	for _, model := range report.Models {
		if model.ID == "" || model.Use == "" || model.File == "" || len(model.Windows) == 0 || model.Subscription == "" {
			t.Fatalf("a model lost a field: %+v", model)
		}
		if model.Use == string(models.UseExcluded) && model.Reason == "" {
			t.Fatalf("an exclusion with no reason: %+v", model)
		}
	}
	if len(report.Defaults) != len(report.Subscriptions) {
		t.Fatalf("the json names %d defaults for %d subscriptions", len(report.Defaults), len(report.Subscriptions))
	}
}

func TestModelsNamesTheRoleEachModelIsBoundTo(t *testing.T) {
	project := t.TempDir()
	writeProjectRole(t, project, "child", "openai/gpt-5.6-luna")
	t.Chdir(project)

	var out, errOut bytes.Buffer
	if code := modelsVerb(nil, &out, &errOut, plain); code != exitOK {
		t.Fatalf("boji models exited %d: %s", code, errOut.String())
	}
	t.Logf("boji models\n%s", out.String())
	text := oneLine(out.String())
	if !strings.Contains(text, "openai/gpt-5.6-luna [child]") {
		t.Fatalf("boji models does not name the role the model is bound to:\n%s", out.String())
	}
	if !strings.Contains(text, "turn has nothing bound") {
		t.Fatalf("boji models does not say which role is unbound:\n%s", out.String())
	}
	if strings.Contains(text, "child has nothing bound") {
		t.Fatalf("boji models calls a bound role unbound:\n%s", out.String())
	}
}

func TestRunModelExcludedIsRefusedWithTheCatalogReason(t *testing.T) {
	_, err := selectModel("anthropic", "anthropic/claude-fable-5-1")
	var refusal *models.Refusal
	if !errors.As(err, &refusal) || refusal.Kind != models.RefusedExcluded {
		t.Fatalf("want the catalog exclusion, got %v", err)
	}
	if !strings.Contains(err.Error(), "not fable or astra for now") {
		t.Fatalf("the refusal is generic: %v", err)
	}
}

func TestSelectModelRefusesAModelTheWireCannotReach(t *testing.T) {
	_, err := selectModel("codex", "anthropic/claude-opus-5")
	if err == nil || !strings.Contains(err.Error(), "belongs to the claude subscription") {
		t.Fatalf("want the wire mismatch named, got %v", err)
	}
}

func TestSelectModelRefusesABareNameSoOneStringMeansOneModel(t *testing.T) {
	_, err := selectModel("anthropic", "claude-opus-5")
	if err == nil || !strings.Contains(err.Error(), "claude-opus-5") {
		t.Fatalf("a bare name must be refused by name, got %v", err)
	}
}

func TestRunModelUnknownIsRefusedBeforeAnyRequest(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	ask := func(slug string) error {
		model, err := selectModel("anthropic", slug)
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

	if err := ask("anthropic/claude-opus-latest"); err == nil || !strings.Contains(err.Error(), "claude-opus-latest") {
		t.Fatalf("an unknown model must be refused by name, got %v", err)
	}
	if calls != 0 {
		t.Fatalf("an unknown model reached the wire %d times", calls)
	}
	_ = ask("anthropic/claude-opus-5")
	if calls != 1 {
		t.Fatalf("the counter never saw a request, so the zero above proves nothing: %d", calls)
	}
}

func TestLiveModelsDiscover(t *testing.T) {
	if os.Getenv("BOJI_LIVE_MODELS") != "1" {
		t.Skip("set BOJI_LIVE_MODELS=1 to ask both live subscriptions which models they serve")
	}
	var out, errOut bytes.Buffer
	code := modelsVerb([]string{"--discover"}, &out, &errOut, plain)
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
