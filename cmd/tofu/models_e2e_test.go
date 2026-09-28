package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"tofu/internal/llm/models"
)

const (
	modelsFromTheBinaryAlone = `claude-sub/claude-opus-5, codex-sub/gpt-5.6-sol 7 of 18 usable

  claude-sub  claude-sub/claude-opus-5 (kind llm, pays subscription) by
                default on --wire anthropic, a 1000000 token window, spends
                5h and 7d
              also allowed, claude-sub/claude-haiku-4-5-20251001 (kind llm,
                pays subscription), claude-sub/claude-sonnet-5 (kind llm,
                pays subscription)
              2 excluded, the owner on 2026-09-19, not fable or astra for
                now, those are not cheap. A current preference recorded as
                data, not a permanent rule
              6 excluded, a previous generation the account still serves,
                superseded by claude-opus-5 and claude-sonnet-5

  codex-sub   codex-sub/gpt-5.6-sol (kind llm, pays subscription) by default
                on --wire codex, a 1050000 token window, spends 5h and 7d
              also allowed, codex-sub/gpt-5.6-luna (kind llm, pays
                subscription), codex-sub/gpt-5.6-terra (kind llm, pays
                subscription)
              1 excluded, a previous generation the account still serves,
                superseded by gpt-5.6-sol
              1 excluded, the owner on 2026-09-19, not fable or astra for
                now, those are not cheap. A current preference recorded as
                data, not a permanent rule
              1 excluded, nobody has ruled on it, so tofu does not send it
                until somebody does

  key         typesafe/jev-latest (kind classifier, pays key), use allowed

  windows     16 of 18 models take a context window from the snapshot of
                models.dev taken on 2026-09-21, and tofu models reload reads
                the table again

  roles       orchestrator: the model that plans and hands work to
                sub-agents. nothing is bound, so it runs on the subscription
                default

              (unnamed sub-agent): a spawn that names no sub-agent. nothing
                is bound, so it runs on the orchestrator's model
`

	modelsWithAProjectLayerAndAHomeRegistry = `claude-sub/claude-opus-5, codex-sub/gpt-5.6-sol 8 of 18 usable

  claude-sub  claude-sub/claude-opus-5 (kind llm, pays subscription) by
                default on --wire anthropic, a 123456 token window, spends
                5h and 7d
              also allowed, claude-sub/claude-haiku-4-5-20251001 (kind llm,
                pays subscription), claude-sub/claude-opus-4-5-20251101
                (kind llm, pays subscription, from the project layer),
                claude-sub/claude-sonnet-5 (kind llm, pays subscription)
                [orchestrator]
              2 excluded, the owner on 2026-09-19, not fable or astra for
                now, those are not cheap. A current preference recorded as
                data, not a permanent rule
              5 excluded, a previous generation the account still serves,
                superseded by claude-opus-5 and claude-sonnet-5

  codex-sub   codex-sub/gpt-5.6-sol (kind llm, pays subscription) by default
                on --wire codex, a 0 token window, spends 5h and 7d
              also allowed, codex-sub/gpt-5.6-luna (kind llm, pays
                subscription), codex-sub/gpt-5.6-terra (kind llm, pays
                subscription)
              1 excluded, a previous generation the account still serves,
                superseded by gpt-5.6-sol
              1 excluded, the owner on 2026-09-19, not fable or astra for
                now, those are not cheap. A current preference recorded as
                data, not a permanent rule
              1 excluded, nobody has ruled on it, so tofu does not send it
                until somebody does

  key         typesafe/jev-latest (kind classifier, pays key), use allowed

  windows     1 of 18 models take a context window from the table this test
                wrote, and tofu models reload reads the table again

  roles       (unnamed sub-agent): a spawn that names no sub-agent. nothing
                is bound, so it runs on the orchestrator's model
`

	homeRegistryOfOneWindow = `{"from":"the table this test wrote","windows":{"anthropic/claude-opus-5":123456}}`
)

func TestE2EModelsMergesTheProjectLayerOverTheBinaryAndReadsTheRegistryFromHome(t *testing.T) {
	shipped := newProject(t, "shipped")
	sameText(t, "a project that overrides nothing",
		shipped.run(t, exitOK, "models"), modelsFromTheBinaryAlone)

	layered := newProject(t, "layered")
	writeFile(t, layered.dir, ".tofu/models/anthropic/claude-opus-4-5-20251101.yaml", "use: allowed\n")
	writeFile(t, layered.dir, ".tofu/roles/turn.yaml", "model: claude-sub/claude-sonnet-5\n")
	writeFile(t, layered.home, ".tofu/model-windows.json", homeRegistryOfOneWindow)
	sameText(t, "a project that allows one excluded model, binds the orchestrator through the legacy turn.yaml, and carries its own window table",
		layered.run(t, exitOK, "models"), modelsWithAProjectLayerAndAHomeRegistry)
}

func stubbed(t *testing.T, body string) string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	return server.URL
}

func TestModelsReloadWritesWhatTheAccountServesIntoTheCatalog(t *testing.T) {
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)
	t.Setenv(models.RegistryURLVariable, stubbed(t, `{"anthropic":{"models":{"claude-sonnet-5-5":{"limit":{"context":1000000},"tool_call":true}}}}`))
	catalog, err := models.CatalogDir()
	if err != nil {
		t.Fatal(err)
	}
	shadowing := filepath.Join(catalog, "models", "anthropic", "claude-opus-5.yaml")
	writeFile(t, catalog, "models/anthropic/claude-opus-5.yaml", "subscription: claude-sub\nuse: allowed\n")

	account := models.Account{
		Subscription: models.ClaudeSub,
		Token:        func(context.Context) (string, error) { return "stub", nil },
		BaseURL:      stubbed(t, `{"data":[{"id":"claude-opus-5"},{"id":"claude-sonnet-5"},{"id":"claude-sonnet-5-5"}]}`),
	}
	var said strings.Builder
	if code := reloadModels(context.Background(), []models.Account{account}, &said); code != exitOK {
		t.Fatalf("reload exited %d\n%s", code, said.String())
	}
	if _, err := os.Stat(filepath.Join(catalog, "models", "anthropic", "claude-sonnet-5-5.yaml")); err != nil {
		t.Fatalf("the served id is not in the catalog: %v\n%s", err, said.String())
	}
	if _, err := os.Stat(shadowing); !os.IsNotExist(err) {
		t.Fatalf("the catalog file for an id tofu ships is still there, so it shadows the shipped file\n%s", said.String())
	}

	var listed strings.Builder
	if code := modelsVerb([]string{jsonFlag}, &listed, io.Discard, 0); code != exitOK {
		t.Fatalf("tofu models --json exited %d\n%s", code, listed.String())
	}
	var report modelsReport
	if err := json.Unmarshal([]byte(listed.String()), &report); err != nil {
		t.Fatal(err)
	}
	layer := "absent"
	for _, model := range report.Models {
		if model.Slug == "claude-sub/claude-sonnet-5-5" {
			layer = model.Layer
		}
	}
	if layer != "catalog" {
		t.Fatalf("tofu models --json shows claude-sub/claude-sonnet-5-5 with layer %s rather than catalog\n%s", layer, said.String())
	}

	var refreshed strings.Builder
	if code := modelsVerb([]string{"--refresh"}, &refreshed, io.Discard, 0); code != exitOK || !strings.Contains(refreshed.String(), models.ReloadVerb) {
		t.Fatalf("tofu models --refresh exited %d and does not name %s\n%s", code, models.ReloadVerb, refreshed.String())
	}
	t.Log("\n" + said.String() + refreshed.String())
}
