package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"tofu/interface/cli"
	"tofu/internal/llm/models"
	"tofu/internal/llm/wire/anthropic"
	"tofu/internal/llm/wire/codex"
	settingspkg "tofu/internal/settings"
)

const (
	modelsFromTheBinaryAlone = `Models · 24 known                                                    ✓ 16 usable

claude-sub
  ✓ claude-sub/claude-fable-5              allowed   1M
  ✓ claude-sub/claude-fable-5-1            allowed   1M
  ✓ claude-sub/claude-haiku-4-5-20251001   allowed   200k
  ○ claude-sub/claude-opus-4-5-20251101    excluded  200k
  ○ claude-sub/claude-opus-4-6             excluded  1M
  ○ claude-sub/claude-opus-4-7             excluded  1M
  ○ claude-sub/claude-opus-4-8             excluded  1M
  ● claude-sub/claude-opus-5               default   1M
  ○ claude-sub/claude-sonnet-4-5-20250929  excluded  1M
  ○ claude-sub/claude-sonnet-4-6           excluded  1M
  ✓ claude-sub/claude-sonnet-5             allowed   1M

codex-sub
  ○ codex-sub/gpt-5.5        excluded  1.05M
  ✓ codex-sub/gpt-5.6-luna   allowed   1.05M
  ● codex-sub/gpt-5.6-sol    default   1.05M
  ✓ codex-sub/gpt-5.6-terra  allowed   1.05M
  ✓ codex-sub/gpt-6-astra    allowed   1.05M
  ○ codex-sub/gpt-reserve    excluded

api key
  ✓ meta/muse-spark-1.1              allowed  1.05M
  ✓ meta/muse-spark-1.2              allowed  1.05M
  ⚠ meta/muse-spark-1.2-contributor  allowed  1.05M
  ✓ meta/muse-spark-1.3              allowed  1.05M
  ⚠ meta/muse-spark-1.3-contributor  allowed  1.05M
  ✓ openrouter/jev-latest            allowed
  ✓ typesafe/jev-latest              allowed
  ⚠ Meta may train on what you send to this model

roles
  ○ orchestrator         unbound
  ○ classifier           unbound
  ○ (unnamed sub-agent)  unbound

windows
  table      the snapshot of models.dev taken on 2026-09-21
  listed     16
  published  5
  → tofu models reload
`

	modelsWithAProjectLayerAndAHomeRegistry = `Models · 24 known                                                    ✓ 17 usable

claude-sub
  ✓ claude-sub/claude-fable-5              allowed
  ✓ claude-sub/claude-fable-5-1            allowed
  ✓ claude-sub/claude-haiku-4-5-20251001   allowed
  ✓ claude-sub/claude-opus-4-5-20251101    allowed         project layer
  ○ claude-sub/claude-opus-4-6             excluded
  ○ claude-sub/claude-opus-4-7             excluded
  ○ claude-sub/claude-opus-4-8             excluded
  ● claude-sub/claude-opus-5               default   123k
  ○ claude-sub/claude-sonnet-4-5-20250929  excluded
  ○ claude-sub/claude-sonnet-4-6           excluded
  ✓ claude-sub/claude-sonnet-5             allowed         orchestrator

codex-sub
  ○ codex-sub/gpt-5.5        excluded
  ✓ codex-sub/gpt-5.6-luna   allowed
  ● codex-sub/gpt-5.6-sol    default
  ✓ codex-sub/gpt-5.6-terra  allowed
  ✓ codex-sub/gpt-6-astra    allowed
  ○ codex-sub/gpt-reserve    excluded

api key
  ✓ meta/muse-spark-1.1              allowed  1.05M
  ✓ meta/muse-spark-1.2              allowed  1.05M
  ⚠ meta/muse-spark-1.2-contributor  allowed  1.05M
  ✓ meta/muse-spark-1.3              allowed  1.05M
  ⚠ meta/muse-spark-1.3-contributor  allowed  1.05M
  ✓ openrouter/jev-latest            allowed
  ✓ typesafe/jev-latest              allowed
  ⚠ Meta may train on what you send to this model

roles
  ● orchestrator         claude-sub/claude-sonnet-5
  ○ classifier           unbound
  ○ (unnamed sub-agent)  unbound

windows
  table      the table this test wrote
  listed     1
  published  5
  → tofu models reload
`

	modelsJSONWithAProjectLayer = `models ok true problems 0
claude-sub/claude-fable-5 allowed 0 library
claude-sub/claude-fable-5-1 allowed 0 library
claude-sub/claude-haiku-4-5-20251001 allowed 0 library
claude-sub/claude-opus-4-5-20251101 allowed 0 project
claude-sub/claude-opus-4-6 excluded 0 library
claude-sub/claude-opus-4-7 excluded 0 library
claude-sub/claude-opus-4-8 excluded 0 library
claude-sub/claude-opus-5 default 123456 library
claude-sub/claude-sonnet-4-5-20250929 excluded 0 library
claude-sub/claude-sonnet-4-6 excluded 0 library
claude-sub/claude-sonnet-5 allowed 0 library orchestrator
codex-sub/gpt-5.5 excluded 0 library
codex-sub/gpt-5.6-luna allowed 0 library
codex-sub/gpt-5.6-sol default 0 library
codex-sub/gpt-5.6-terra allowed 0 library
codex-sub/gpt-6-astra allowed 0 library
codex-sub/gpt-reserve excluded 0 library
meta/muse-spark-1.1 allowed 1048576 library
meta/muse-spark-1.2 allowed 1048576 library
meta/muse-spark-1.2-contributor allowed 1048576 library
meta/muse-spark-1.3 allowed 1048576 library
meta/muse-spark-1.3-contributor allowed 1048576 library
openrouter/jev-latest allowed 0 library
typesafe/jev-latest allowed 0 library
`

	homeRegistryOfOneWindow = `{"from":"the table this test wrote","windows":{"anthropic/claude-opus-5":123456}}`

	reloadOfANewModelAndARefusedAccount = `Model reload                                                  ⚠ 1 new · 1 failed

  models.dev    ✓ 2 context windows
  catalog       ✓ 1 file removed

claude-sub      ✓ 3 served
  + claude-sonnet-5-5  allowed

codex-sub       ✗ model list refused (403)
  → tofu login codex-sub
`

	reloadWithNobodySignedIn = `Model reload                                                  ✓ 1 version raised

  models.dev    ✓ 2 context windows
  npm           ✓ claudeCode raised from PIN to NPM

claude-sub      ○ not signed in
  → tofu login claude-sub

codex-sub       ○ not signed in
  → tofu login codex-sub
`

	registryOfTwoWindows = `{"anthropic":{"models":{"claude-sonnet-5-5":{"limit":{"context":1000000},"tool_call":true},"claude-opus-5":{"limit":{"context":1000000},"tool_call":true}}}}`
	vendorBody           = `{"error":{"message":"the vendor refused this account"}}`
)

type envelopeOf[T any] struct {
	Verb     string        `json:"verb"`
	OK       bool          `json:"ok"`
	Data     T             `json:"data"`
	Problems []cli.Problem `json:"problems"`
}

func oneEnvelope(t *testing.T, printed string, into any) {
	t.Helper()
	decoder := json.NewDecoder(strings.NewReader(printed))
	if err := decoder.Decode(into); err != nil {
		t.Fatalf("stdout is not a JSON envelope: %v\n%s", err, printed)
	}
	if decoder.More() {
		t.Fatalf("stdout holds more than one JSON document\n%s", printed)
	}
}

func modelsJSONLines(t *testing.T, printed string) string {
	t.Helper()
	var envelope envelopeOf[modelsReport]
	oneEnvelope(t, printed, &envelope)
	lines := []string{envelope.Verb + " ok " + strconv.FormatBool(envelope.OK) + " problems " + strconv.Itoa(len(envelope.Problems))}
	for _, model := range envelope.Data.Models {
		lines = append(lines, strings.TrimSpace(strings.Join([]string{model.Slug, model.Use, strconv.Itoa(model.ContextTokens), model.Layer, strings.Join(model.Roles, "+")}, " ")))
	}
	return strings.Join(lines, "\n") + "\n"
}

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
	sameText(t, "the same project as one --json envelope",
		modelsJSONLines(t, layered.run(t, exitOK, "models", "--json")), modelsJSONWithAProjectLayer)
}

func stubbed(t *testing.T, status int, body string) string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	return server.URL
}

func npmClaudeCodeAbovePin() string {
	cut := strings.LastIndex(anthropic.PinnedClaudeCodeVersion, ".") + 1
	patch, _ := strconv.Atoi(anthropic.PinnedClaudeCodeVersion[cut:])
	return anthropic.PinnedClaudeCodeVersion[:cut] + strconv.Itoa(patch+16)
}

func stubbedHome(t *testing.T) {
	t.Helper()
	chdirTemp(t)
	t.Setenv(models.RegistryURLVariable, stubbed(t, http.StatusOK, registryOfTwoWindows))
	npm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		version := codex.PinnedCodexClientVersion
		if strings.Contains(r.URL.Path, anthropic.ClaudeCodePackage) {
			version = npmClaudeCodeAbovePin()
		}
		_, _ = w.Write([]byte(`{"version":"` + version + `"}`))
	}))
	t.Cleanup(npm.Close)
	t.Setenv(settingspkg.NpmRegistryVariable, npm.URL)
}

func TestModelsReloadWithNpmDownPassesAndSaysTheVersionCheckWasSkipped(t *testing.T) {
	stubbedHome(t)
	down := httptest.NewServer(http.NotFoundHandler())
	down.Close()
	t.Setenv(settingspkg.NpmRegistryVariable, down.URL)
	var text, errOut bytes.Buffer
	if code := run([]string{"models", "reload"}, strings.NewReader(""), &text, &errOut); code != exitOK || !strings.Contains(text.String(), "npm           ⚠ unreachable, the version check was skipped") {
		t.Fatalf("tofu models reload with npm down exited %d\n%s%s", code, text.String(), errOut.String())
	}
	var printed bytes.Buffer
	if code := run([]string{"models", "reload", jsonFlag}, strings.NewReader(""), &printed, &errOut); code != exitOK {
		t.Fatalf("tofu models reload --json with npm down exited %d\n%s%s", code, printed.String(), errOut.String())
	}
	var envelope envelopeOf[modelReload]
	oneEnvelope(t, printed.String(), &envelope)
	if !envelope.OK || !strings.Contains(envelope.Data.Versions.Skipped, "skipped") || len(envelope.Data.Versions.Raised) != 0 {
		t.Fatalf("the envelope does not say the version check was skipped\n%s", printed.String())
	}
}

func TestModelsReloadShowsANewModelAndARefusedAccountWithTheVendorBodyOnlyInJSON(t *testing.T) {
	stubbedHome(t)
	catalog, err := models.CatalogDir()
	if err != nil {
		t.Fatal(err)
	}
	shadowing := filepath.Join(catalog, "models", "anthropic", "claude-opus-5.yaml")
	writeFile(t, catalog, "models/anthropic/claude-opus-5.yaml", "subscription: claude-sub\nuse: allowed\n")
	token := func(context.Context) (string, error) { return "stub", nil }
	accounts := []models.Account{
		{Subscription: models.ClaudeSub, Token: token, BaseURL: stubbed(t, http.StatusOK, `{"data":[{"id":"claude-opus-5"},{"id":"claude-sonnet-5"},{"id":"claude-sonnet-5-5"}]}`)},
		{Subscription: models.CodexSub, Token: token, BaseURL: stubbed(t, http.StatusForbidden, vendorBody)},
	}
	client, err := reloadClient()
	if err != nil {
		t.Fatal(err)
	}
	report, err := reloadModels(context.Background(), client, accounts, nil)
	if err != nil {
		t.Fatal(err)
	}
	var text bytes.Buffer
	if err := (cli.Page{Width: 80}).Print(&text, reloadLines(cli.Page{Width: 80}, report)); err != nil {
		t.Fatal(err)
	}
	sameText(t, "a reload where one account serves a new model and the other is refused", text.String(), reloadOfANewModelAndARefusedAccount)

	var printed bytes.Buffer
	if err := writeJSON(&printed, report.envelope()); err != nil {
		t.Fatal(err)
	}
	var envelope envelopeOf[modelReload]
	oneEnvelope(t, printed.String(), &envelope)
	if envelope.Verb != "models reload" || envelope.OK || len(envelope.Problems) != 1 || envelope.Problems[0].Hint != "tofu login codex-sub" {
		t.Errorf("the envelope does not carry one problem for the refused account with its login hint\n%s", printed.String())
	}
	states := map[string]string{}
	for _, source := range envelope.Data.Sources {
		states[source.Source] = source.State
		if source.Source == "codex-sub" && !strings.Contains(source.Error, "the vendor refused this account") {
			t.Errorf("the JSON drops the vendor body for the refused account: %q", source.Error)
		}
	}
	if states["claude-sub"] != sourceReloaded || states["codex-sub"] != sourceRefused {
		t.Errorf("the sources are %v, want claude-sub %s and codex-sub %s", states, sourceReloaded, sourceRefused)
	}

	if _, err := os.Stat(filepath.Join(catalog, "models", "anthropic", "claude-sonnet-5-5.yaml")); err != nil {
		t.Fatalf("the served id is not in the catalog: %v", err)
	}
	if _, err := os.Stat(shadowing); !os.IsNotExist(err) {
		t.Fatal("the catalog file for an id tofu ships is still there, so it shadows the shipped file")
	}
	var listed bytes.Buffer
	if code := run([]string{"models", jsonFlag}, strings.NewReader(""), &listed, io.Discard); code != exitOK {
		t.Fatalf("tofu models --json exited %d\n%s", code, listed.String())
	}
	if !strings.Contains(modelsJSONLines(t, listed.String()), "claude-sub/claude-sonnet-5-5 allowed 1000000 catalog") {
		t.Fatalf("tofu models --json does not show claude-sub/claude-sonnet-5-5 from the catalog\n%s", listed.String())
	}
}

func TestModelsReloadWithNobodySignedInSaysSoInTextAndInJSON(t *testing.T) {
	stubbedHome(t)
	var text, errOut bytes.Buffer
	if code := run([]string{"models", "reload"}, strings.NewReader(""), &text, &errOut); code != exitOK {
		t.Fatalf("tofu models reload exited %d\n%s%s", code, text.String(), errOut.String())
	}
	npm := npmClaudeCodeAbovePin()
	sameText(t, "a reload with no account signed in", text.String(), strings.NewReplacer("PIN", anthropic.PinnedClaudeCodeVersion, "NPM", npm).Replace(reloadWithNobodySignedIn))
	global, _ := settingsPaths(".")
	if written, _ := os.ReadFile(global); !strings.Contains(string(written), `"claudeCode": "`+npm+`"`) {
		t.Fatalf("npm at %s did not raise the global file:\n%s", npm, written)
	}

	var printed bytes.Buffer
	if code := run([]string{"models", "reload", jsonFlag}, strings.NewReader(""), &printed, &errOut); code != exitOK {
		t.Fatalf("tofu models reload --json exited %d\n%s%s", code, printed.String(), errOut.String())
	}
	var envelope envelopeOf[modelReload]
	oneEnvelope(t, printed.String(), &envelope)
	if !envelope.OK || len(envelope.Data.Sources) != 2 {
		t.Fatalf("the envelope is not ok with two sources\n%s", printed.String())
	}
	for _, source := range envelope.Data.Sources {
		if source.State != sourceNotSignedIn || source.Hint != "tofu login "+source.Source {
			t.Errorf("%s is %q with hint %q, want %q with its login hint", source.Source, source.State, source.Hint, sourceNotSignedIn)
		}
	}
}

func TestNoColourWritesNoEscapeWhereForcedColourWritesOne(t *testing.T) {
	stubbedHome(t)
	t.Setenv("FORCE_COLOR", "1")
	verbs := [][]string{{"models"}, {"models", "reload"}, {"reload"}}
	for _, verb := range verbs {
		var forced bytes.Buffer
		run(verb, strings.NewReader(""), &forced, io.Discard)
		if !strings.Contains(forced.String(), "\x1b[") {
			t.Fatalf("tofu %s with FORCE_COLOR wrote no escape, so the NO_COLOR half proves nothing\n%s", strings.Join(verb, " "), forced.String())
		}
	}
	t.Setenv("NO_COLOR", "1")
	for _, verb := range verbs {
		var plain bytes.Buffer
		run(verb, strings.NewReader(""), &plain, &plain)
		if strings.Contains(plain.String(), "\x1b") {
			t.Errorf("tofu %s under NO_COLOR wrote an escape\n%q", strings.Join(verb, " "), plain.String())
		}
	}
}
