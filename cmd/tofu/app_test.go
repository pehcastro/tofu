package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"tofu/interface/tui"
	"tofu/interface/tui/frame"
	"tofu/interface/tui/markdown"
	"tofu/interface/tui/session"
	"tofu/interface/tui/settings"
	"tofu/interface/tui/subagent"
	"tofu/internal/judge/jev"
	"tofu/internal/judge/ledger"
	"tofu/internal/konst"
	"tofu/internal/llm"
	"tofu/internal/llm/cred"
	"tofu/internal/llm/models"
	"tofu/internal/recall"
	sessionstore "tofu/internal/session"
	settingspkg "tofu/internal/settings"
	"tofu/internal/shell"
	roster "tofu/internal/subagent"
	"tofu/internal/sys"
	"tofu/internal/transport"
	"tofu/internal/turn"
	"tofu/internal/widget"
)

func TestMain(m *testing.M) {
	if len(os.Args) > 1 && !strings.HasPrefix(os.Args[1], "-") {
		_, _ = os.Stdout.WriteString("stand-in tofu ran " + strings.Join(os.Args[1:], " ") + "\n")
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestVerbTableIsUnchanged(t *testing.T) {
	for _, verb := range []struct {
		args []string
		code int
		says string
	}{
		{[]string{"version"}, exitOK, "version: "},
		{[]string{"doctor", "--nope"}, exitUsage, "tofu doctor: unknown argument"},
		{[]string{"login", "--nope"}, exitUsage, "tofu login: "},
		{[]string{"usage", "--nope"}, exitUsage, "usage: tofu usage"},
		{[]string{"models", "--nope"}, exitUsage, "tofu models: unknown flag"},
		{[]string{"why", "--nope"}, exitUsage, "tofu why: "},
		{[]string{"run", "--nope"}, exitUsage, "tofu run: "},
		{[]string{"judge", "--nope"}, exitUsage, "tofu judge: "},
		{[]string{"check", "--nope"}, exitUsage, "tofu check: "},
		{[]string{"label", "--nope"}, exitUsage, "tofu label: "},
		{[]string{"replay", "--nope"}, exitUsage, "tofu replay: "},
		{[]string{"library", "--nope"}, exitUsage, "tofu library: usage"},
		{[]string{"lint", "--nope"}, exitUsage, "tofu lint: usage"},
		{[]string{"rules", "--nope"}, exitUsage, "tofu rules: "},
		{[]string{"nosuchverb"}, exitUsage, "unknown verb"},
		{[]string{"--help"}, exitOK, "tofu is a coding agent harness"},
	} {
		t.Run(strings.Join(verb.args, " "), func(t *testing.T) {
			var out, errOut bytes.Buffer
			code := run(verb.args, strings.NewReader(""), &out, &errOut)
			if code != verb.code {
				t.Errorf("exit %d, want %d, stdout %q stderr %q", code, verb.code, out.String(), errOut.String())
			}
			if said := out.String() + errOut.String(); !strings.Contains(said, verb.says) {
				t.Errorf("output %q does not contain %q", said, verb.says)
			}
		})
	}
}

func TestBareTofuWithoutATerminalSaysSo(t *testing.T) {
	var out, errOut bytes.Buffer
	code := run(nil, strings.NewReader(""), &out, &errOut)
	if code != exitUsage {
		t.Errorf("exit %d, want %d", code, exitUsage)
	}
	if !strings.Contains(errOut.String(), noTerminal) {
		t.Errorf("stderr %q, want the terminal requirement", errOut.String())
	}
	if strings.Contains(out.String(), "Verbs:") {
		t.Error("bare tofu still prints the verb list instead of starting the app")
	}
}

const gateKeyForTests = "sk-or-v1-000000000000000000Q9W4"

func setupScreen(t *testing.T, required []tui.Requirement) string {
	t.Helper()
	app := tui.New(tui.Options{Repo: "scratch", Requirements: required})
	app.Init()
	app.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	screen := app.View().Content
	if strings.Contains(screen, "sqlite") || strings.Contains(screen, "*errors") {
		t.Errorf("the setup screen leaks a go error:\n%s", screen)
	}
	return screen
}

func TestSetupAsksForBothLoginsAndTheSecondOneStoresTheKey(t *testing.T) {
	emptyHome(t)
	required := appRequirements()
	if len(required) != 2 {
		t.Fatalf("requirements %+v, want the credential and the key", required)
	}
	if required[0].What != noCredential || required[0].Fix != loginFix {
		t.Fatalf("requirement %+v", required[0])
	}
	if required[1].What != noGateKey || required[1].Fix != gateKeyFix {
		t.Fatalf("requirement %+v", required[1])
	}
	if args := required[1].Run().Args[1:]; len(args) != 2 || args[0] != "login" || args[1] != openRouterName {
		t.Fatalf("the second fix runs %v, want login openrouter", args)
	}
	t.Log("\n" + setupScreen(t, required))

	storeGateKey(t, gateKeyForTests)
	left := appRequirements()
	if len(left) != 1 || left[0].What != noCredential {
		t.Fatalf("requirements %+v after the key was stored, want only the credential", left)
	}
	t.Log("\n" + setupScreen(t, left))
}

func TestAWireWithNoCredentialIsNeverOffered(t *testing.T) {
	emptyHome(t)
	if wires := appWires(); len(wires) != 0 {
		t.Fatalf("wires %+v with nothing signed in", wires)
	}
	storeCredential(t, cred.CodexSub)
	codexAlone := appWires()
	if len(codexAlone) != 1 || codexAlone[0].Name != "codex" || codexAlone[0].Model != "gpt-5.6-sol" {
		t.Fatalf("wires %+v with only codex signed in, want codex and its default model alone", codexAlone)
	}
	if codexAlone[0].Provider != "codex-sub" {
		t.Fatalf("the codex wire reports provider %q, want codex-sub, the subscription form the frame header composes", codexAlone[0].Provider)
	}
	storeCredential(t, cred.ClaudeSub)
	both := appWires()
	if len(both) != 2 || both[0].Name != "anthropic" || both[0].Model != "claude-opus-5" || both[1].Name != "codex" {
		t.Fatalf("wires %+v with both signed in", both)
	}
}

func TestLiveAppRunsATurnOnEachWire(t *testing.T) {
	if os.Getenv("TOFU_LIVE") != "1" {
		t.Skip("set TOFU_LIVE=1 to spend the subscription quota")
	}
	jev.AllowLiveCredential(t)
	if key, err := jev.Key("../../.env"); err == nil {
		t.Setenv("OPENROUTER_KEY", key)
	}
	offered := appWires()
	if len(offered) != 2 {
		t.Fatalf("wires %+v, want both subscriptions signed in", offered)
	}
	for _, wire := range offered {
		t.Run(wire.Name, func(t *testing.T) {
			dir := t.TempDir()
			app := tui.New(tui.Options{
				Repo:   filepath.Base(dir),
				Branch: "scratch",
				Wires:  func() []tui.Wire { return []tui.Wire{wire} },
				Quota:  appQuota,
			})
			app.Init()
			app.Update(tea.WindowSizeMsg{Width: 100, Height: 28})
			app.Update(appQuota())

			task := "create notes.txt holding the single word ready, then read it back and say what it holds"
			app.Update(tui.Event{Kind: tui.EventText, Text: task})
			var turned eventLog
			newAppSession(dir, openAppWire, nil, time.Now, sessionResume{}).run(t.Context(), tui.Pick{Wire: wire.Name}, task, turned.add)
			answered := ""
			for _, event := range turned.all() {
				if event.Model != "" {
					answered = event.Model
				}
				app.Update(event)
			}

			t.Logf("wire %s asked for %s and the answer reported %s", wire.Name, wire.Model, answered)
			t.Log("\n" + app.View().Content)
			if answered == "" {
				t.Error("no answer reported a model name")
			}
			body, err := os.ReadFile(filepath.Join(dir, "notes.txt"))
			if err != nil || !strings.Contains(string(body), "ready") {
				t.Fatalf("the turn did not write the file: %v %q", err, body)
			}
		})
	}
}

func storeCredential(t *testing.T, provider cred.Provider) {
	t.Helper()
	path, err := cred.Path()
	if err != nil {
		t.Fatal(err)
	}
	store, err := cred.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	credential := cred.Credential{
		Provider: provider,
		Kind:     "oauth",
		Access:   "access-token",
		Expires:  time.Now().Add(time.Hour),
		Identity: cred.Identity{Email: string(provider) + "@example.com"},
	}
	if err := store.Save(credential, time.Now()); err != nil {
		t.Fatal(err)
	}
}

func storeGateKey(t *testing.T, key string) {
	t.Helper()
	path, err := cred.OpenRouterPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := cred.SaveOpenRouter(path, key); err != nil {
		t.Fatal(err)
	}
}

func settingsScreen(t *testing.T, providers []settings.Provider) string {
	t.Helper()
	store, _ := openSettings(t.TempDir())
	app := tui.New(tui.Options{Repo: "bob", Branch: "develop", Providers: providers, Settings: store})
	app.Init()
	app.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	openSettingsMenu(app)
	app.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	return app.View().Content
}

func openSettingsMenu(app *tui.App) {
	for _, letter := range "/settings" {
		app.Update(tea.KeyPressMsg{Code: letter, Text: string(letter)})
	}
	app.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
}

func TestSettingsNamesEveryProviderAndTheSourceThatDecidedIt(t *testing.T) {
	emptyHome(t)
	storeCredential(t, cred.ClaudeSub)
	storeCredential(t, cred.CodexSub)
	storeGateKey(t, gateKeyForTests)

	providers := appProviders()
	if len(providers) != 4 {
		t.Fatalf("providers %+v, want four rows", providers)
	}
	for _, provider := range providers {
		if provider.Name == "" || provider.Source == "" {
			t.Errorf("provider %+v names no source", provider)
		}
	}
	screen := settingsScreen(t, providers)
	for _, name := range []string{string(cred.ClaudeSub), openRouterName, "jev", string(cred.CodexSub)} {
		if !strings.Contains(screen, name) {
			t.Errorf("the settings view does not name %q:\n%s", name, screen)
		}
	}
	t.Log("\n" + screen)
}

func TestProvidersHoldNoMoreOfTheKeyThanItsTail(t *testing.T) {
	emptyHome(t)
	const key = gateKeyForTests
	storeGateKey(t, key)

	var held string
	for _, provider := range appProviders() {
		held += provider.Name + provider.State + provider.Key + provider.Fix + provider.Source
	}
	if !strings.Contains(held, key[len(key)-4:]) {
		t.Errorf("no row shows the last four characters of the key:\n%s", held)
	}
	for tail := 5; tail <= len(key); tail++ {
		if strings.Contains(held, key[len(key)-tail:]) {
			t.Fatalf("a row holds the last %d characters of the key:\n%s", tail, held)
		}
	}
}

func TestAProviderThatCannotBeReadSaysSo(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	if err := os.WriteFile(home, []byte("this is a file, not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("OPENROUTER_KEY", "")

	providers := appProviders()
	if len(providers) != 4 {
		t.Fatalf("providers %+v, want four rows", providers)
	}
	said := providers[0].State + providers[0].Fix
	if !strings.HasPrefix(said, unreadableSource) {
		t.Errorf("the anthropic row says %q, want it to start with %q", said, unreadableSource)
	}
	t.Log("\n" + settingsScreen(t, providers))
}

func TestTheGateOffEventCarriesTheReasonTheKeyLookupFound(t *testing.T) {
	t.Setenv(envVarName(), "")
	dir := t.TempDir()
	unnamed := filepath.Join(dir, "unnamed", ".env")
	unreadable := filepath.Join(dir, "unreadable", ".env")
	if err := os.MkdirAll(filepath.Dir(unnamed), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(unnamed, []byte("OTHER=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(unreadable, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, state := range []struct {
		name string
		path string
		want jev.Why
	}{
		{"no file", filepath.Join(dir, "gone", ".env"), jev.WhyNoFile},
		{"a file without the name in it", unnamed, jev.WhyFileLacksName},
		{"a file that will not read", unreadable, jev.WhyUnreadable},
	} {
		_, err := jev.Key(state.path)
		if err == nil {
			t.Fatalf("%s was accepted as a key", state.name)
		}
		event := gateOffEvent(err)
		if event.Kind != tui.EventGateOff || event.Text != err.Error() {
			t.Errorf("%s produced %+v, want the gate-off event carrying %q", state.name, event, err.Error())
		}
		if event.GateWhy != state.want {
			t.Errorf("%s reads as reason %d, want %d: %v", state.name, event.GateWhy, state.want, err)
		}
	}
}

func TestGateOffNoteNamesTheLogin(t *testing.T) {
	const want = "gate off: no tool call is judged until tofu login openrouter stores the key"
	if gateOffNote != want {
		t.Errorf("gateOffNote %q, want %q", gateOffNote, want)
	}
}

func recordedGateDecision() turn.GateDecision {
	modeReason := "the rule came from the library as tool_gate@3.yaml"
	return turn.GateDecision{
		ID:      "2026-09-19-6f1c",
		Verdict: ledger.VerdictAsk,
		Answers: []ledger.Answer{
			{Question: "approval", Kind: ledger.AnswerNoul, Noul: 0.75},
			{Question: "from_untrusted", Kind: ledger.AnswerNoul, Noul: 0.02},
			{Question: "risk", Kind: ledger.AnswerScore, Score: 2, Dist: []ledger.Slice{
				{Option: "0", P: 0.01}, {Option: "1", P: 0.08}, {Option: "2", P: 0.88}, {Option: "3", P: 0.03},
			}},
			{Question: "user_requested", Kind: ledger.AnswerNoul, Noul: 0.11},
		},
		Reason: &ledger.Reason{
			Question:   "risk",
			Comparison: "risk_ask_at",
			Threshold:  1.5,
			Value:      2,
			Mode:       ledger.ModeShadow,
			ModeReason: &modeReason,
		},
	}
}

func TestTheInterfaceIsHandedNumbersAndNotFormattedText(t *testing.T) {
	decision := gateDecision("write", recordedGateDecision())
	if decision.Verdict != session.Ask || decision.Tool != "write" {
		t.Fatalf("decision %+v", decision)
	}
	if len(decision.Answers) != 4 {
		t.Fatalf("answers %+v, want the four the row carries", decision.Answers)
	}
	for _, want := range []session.Answer{
		{Question: "approval", Value: 0.75, Max: 1},
		{Question: "from_untrusted", Value: 0.02, Max: 1},
		{Question: "risk", Value: 2, Max: 3},
		{Question: "user_requested", Value: 0.11, Max: 1},
	} {
		if !slices.Contains(decision.Answers, want) {
			t.Errorf("answer %+v is missing from %+v", want, decision.Answers)
		}
	}
	if decision.Reason.Value != 2 || decision.Reason.Threshold != 1.5 || decision.Reason.Limit != "risk_ask_at" {
		t.Errorf("reason %+v", decision.Reason)
	}
	for _, text := range stringsIn(reflect.ValueOf(decision)) {
		if strings.ContainsAny(text, "0123456789%▓░") {
			t.Errorf("the decision carries formatted text %q", text)
		}
	}
}

func stringsIn(value reflect.Value) []string {
	var out []string
	switch value.Kind() {
	case reflect.String:
		return []string{value.String()}
	case reflect.Struct:
		for index := range value.NumField() {
			out = append(out, stringsIn(value.Field(index))...)
		}
	case reflect.Slice:
		for index := range value.Len() {
			out = append(out, stringsIn(value.Index(index))...)
		}
	}
	return out
}

func TestTheSessionFormatsTheNumbersItWasHanded(t *testing.T) {
	view := session.New(time.Now, new(markdown.Renderer).Lines)
	view.SetSize(100, 20)
	view.Append(session.Entry{Kind: session.Tool, Head: "write", Body: "README.md"})
	view.Decide(gateDecision("write", recordedGateDecision()))
	content := view.View()
	for _, want := range []string{"ask", "risk", "2.00", "approval", "0.75", "▓", "risk 2.00 is over risk_ask_at 1.50"} {
		if !strings.Contains(content, want) {
			t.Errorf("the session view does not render %q\n%s", want, content)
		}
	}
}

func TestACallReadsAsIntentAndKeepsTheWholeCommandBehindIt(t *testing.T) {
	long := `cd /mnt/f/localhost/ephem-sh/bob; for d in internal/* interface/* cmd/*; ` +
		`do n=$(find "$d" -name '*.go' | wc -l); echo "$d $n"; done`
	for _, want := range []struct {
		arguments string
		intent    string
		detail    string
	}{
		{`{"command":"go test ./...","timeout":30}`, "go test ./...", "go test ./..."},
		{`{"command":"` + strings.ReplaceAll(long, `"`, `\"`) + `"}`, "for d in internal/* interface/* cmd/* +3 more", long},
		{`{"path":"internal/turn/loop.go"}`, "internal/turn/loop.go", ""},
		{`{"pattern":"Decide","glob":"*.go"}`, "Decide in *.go", ""},
		{`{"pattern":"Decide"}`, "Decide in " + workingDirectory, ""},
		{`{"task":"rename the judge\n\nkeep the wire","owns":["internal/judge/**"]}`, "rename the judge", "rename the judge\n\nkeep the wire"},
		{`{"task":"rename the judge","mission":"judge rename","owns":["internal/judge/**"]}`, "judge rename", "rename the judge"},
		{`{"handle":"99248324d40bbf11be9cd47093978332","offset":0,"length":512}`, moreOfAStoredResult, ""},
	} {
		intent, detail := callIntent(llm.ToolCall{Arguments: []byte(want.arguments)})
		if intent != want.intent || detail != want.detail {
			t.Errorf("%s reads as %q with %q behind it, want %q and %q", want.arguments, intent, detail, want.intent, want.detail)
		}
	}
}

func TestAResultSaysSomethingOrSaysNothing(t *testing.T) {
	for _, want := range []struct {
		content string
		summary string
	}{
		{"ok  tofu/internal/turn\n", "ok tofu/internal/turn"},
		{"", noOutput},
		{"a\nb\nc\n", "3 lines, 6 bytes"},
		{strings.Repeat("x", 40), "1 line, 40 bytes"},
		{strings.Repeat("a line of it\n", 200), "200 lines, 2.5 KB"},
	} {
		if got := resultSummary(want.content); got != want.summary {
			t.Errorf("a result of %d bytes reads as %q, want %q", len(want.content), got, want.summary)
		}
	}
}

func TestAnOversizeResultNeverPutsTheModelsHandleOnTheScreen(t *testing.T) {
	state, err := sys.ProjectStateDirAt(scratchProject(t))
	if err != nil {
		t.Fatal(err)
	}
	artifacts, err := turn.NewArtifacts(filepath.Join(state, "artifacts"), true)
	if err != nil {
		t.Fatal(err)
	}
	whole := strings.Repeat("a line of the file it read\n", 460)
	message, handle, err := artifacts.Render(whole, 1024)
	if err != nil {
		t.Fatal(err)
	}
	if handle == "" || !strings.Contains(message, "artifact_fetch") {
		t.Fatalf("the turn did not compose a handle preamble, so this test proves nothing:\n%s", message)
	}

	driver := driveApp(t)
	driver.emit(tui.Event{Kind: tui.EventToolCall, ID: "c1", Tool: "read", Text: "CLAUDE.md"})
	driver.emit(tui.Event{Kind: tui.EventToolResult, ID: "c1", Text: resultSummary(message)})
	screen := driver.view(tea.KeyPressMsg{Code: '2', Mod: tea.ModAlt})
	for _, forbidden := range []string{"artifact_fetch", "not pasted", "holds this result whole", handle} {
		if strings.Contains(screen, forbidden) {
			t.Errorf("the screen carries %q, which is written for the model\n%s", forbidden, screen)
		}
	}
	for _, want := range []string{"read", "CLAUDE.md", "12.1 KB" + storedNote} {
		if !strings.Contains(screen, want) {
			t.Errorf("the screen does not name the file and its size with %q\n%s", want, screen)
		}
	}
	t.Log("\n" + screen)
}

type appDriver struct {
	app    *tui.App
	mutex  sync.Mutex
	events []tui.Event
	frames []string
}

func driveApp(t *testing.T) *appDriver {
	t.Helper()
	return driveAppOn(t, nil)
}

func driveAppOn(t *testing.T, store *settingspkg.Store) *appDriver {
	t.Helper()
	app := tui.New(tui.Options{
		Repo:     "scratch",
		Branch:   "develop",
		Wires:    func() []tui.Wire { return []tui.Wire{{Name: wireSubscription, Model: "stub-model"}} },
		Settings: store,
	})
	app.Init()
	app.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	return &appDriver{app: app}
}

func (d *appDriver) emit(event tui.Event) {
	d.mutex.Lock()
	defer d.mutex.Unlock()
	d.app.Update(event)
	d.events = append(d.events, event)
	d.frames = append(d.frames, d.app.View().Content)
}

func (d *appDriver) of(kind tui.EventKind) []tui.Event {
	d.mutex.Lock()
	defer d.mutex.Unlock()
	var held []tui.Event
	for _, event := range d.events {
		if event.Kind == kind {
			held = append(held, event)
		}
	}
	return held
}

func (d *appDriver) view(keys ...tea.Msg) string {
	for _, key := range keys {
		d.app.Update(key)
	}
	return d.app.View().Content
}

var onTheSubscription = tui.Pick{Wire: wireSubscription}

func stubbedTurn(dir string, model turn.Model, answers ...bool) tui.Turn {
	var person chan tui.Answer
	if len(answers) > 0 {
		person = make(chan tui.Answer, len(answers))
		for _, answer := range answers {
			if answer {
				person <- tui.AllowedOnce
				continue
			}
			person <- tui.Denied
		}
	}
	return resumedTurn(dir, model, person, sessionResume{})
}

var stubSelection = models.Model{ID: "stub-model", Windows: []string{"stub"}}

func wireOn(model turn.Model) appWire {
	return appWire{held: &accounts{fixed: model, now: time.Now}, spend: turn.SpendSubscription, selected: stubSelection}
}

func resumedTurn(dir string, model turn.Model, person chan tui.Answer, resumed sessionResume) tui.Turn {
	return newAppSession(dir, func(runOpts) (appWire, error) {
		return wireOn(model), nil
	}, person, time.Now, resumed).run
}

func scratchProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Chdir(dir)
	emptyHome(t)
	return dir
}

func disableReadBeforeEdit(t *testing.T) {
	t.Helper()
	var out, errOut bytes.Buffer
	if code := settingsVerb([]string{"set", "readBeforeEdit", "false"}, &out, &errOut); code != exitOK {
		t.Fatalf("settings set readBeforeEdit false exited %d: %s", code, errOut.String())
	}
}

func writeNote(id string) llm.ToolCall {
	return llm.ToolCall{ID: id, Name: "write", Arguments: json.RawMessage(`{"path":"note.txt","content":"a note"}`)}
}

func noteThenStop() *queuedModel {
	return &queuedModel{decisions: []llm.Decision{
		{Build: "stub-model", Outcome: llm.OutcomeToolCalls, ToolCalls: []llm.ToolCall{writeNote("call-1")}},
		{Build: "stub-model", Outcome: llm.OutcomeMessage, Content: "wrote it"},
	}}
}

func TestAToolCallsVendorIDNeverReachesTheScreenAndTheCallStillPairsWithItsResult(t *testing.T) {
	dir := scratchProject(t)
	driver := driveApp(t)
	stubbedTurn(dir, &queuedModel{decisions: []llm.Decision{
		{Build: "stub-model", Outcome: llm.OutcomeToolCalls, ToolCalls: []llm.ToolCall{writeNote("toolu_01AbCdEfGhIjKlMn")}},
		{Build: "stub-model", Outcome: llm.OutcomeMessage, Content: "wrote it"},
	}})(t.Context(), onTheSubscription, "write a note", driver.emit)

	calls := driver.of(tui.EventToolCall)
	results := driver.of(tui.EventToolResult)
	if len(calls) != 1 || len(results) != 1 {
		t.Fatalf("want one call and one result, got %d and %d", len(calls), len(results))
	}
	if calls[0].ID == "toolu_01AbCdEfGhIjKlMn" {
		t.Fatalf("the call carries the vendor id instead of a minted event id: %q", calls[0].ID)
	}
	if calls[0].ID == "" || calls[0].ID != results[0].ID {
		t.Fatalf("the call and its result do not share an event id: %q vs %q", calls[0].ID, results[0].ID)
	}
	if strings.Contains(driver.view(), "toolu_") {
		t.Fatalf("the vendor id leaked onto the screen:\n%s", driver.view())
	}
}

type modelStoppingTheTurnWhileItAnswers struct {
	stop context.CancelFunc
	call llm.ToolCall
}

func (m *modelStoppingTheTurnWhileItAnswers) Ask(context.Context, llm.Request) (llm.Decision, error) {
	m.stop()
	return llm.Decision{Build: "stub-model", Outcome: llm.OutcomeToolCalls, ToolCalls: []llm.ToolCall{m.call}}, nil
}

func TestAToolResultThatArrivesAfterTheTurnIsStoppedStillReachesTheView(t *testing.T) {
	dir := scratchProject(t)
	driver := driveApp(t)
	ctx, stop := context.WithCancel(t.Context())
	defer stop()
	model := &modelStoppingTheTurnWhileItAnswers{stop: stop, call: writeNote("call-1")}

	stubbedTurn(dir, model)(ctx, onTheSubscription, "write a note", driver.emit)

	calls, results := driver.of(tui.EventToolCall), driver.of(tui.EventToolResult)
	if len(calls) != 1 || len(results) != 1 {
		t.Fatalf("the stopped turn emitted %d calls and %d results, want the one write and its result", len(calls), len(results))
	}
	if results[0].ID != calls[0].ID {
		t.Fatalf("the result carries %q and the call %q, so the view cannot pair them", results[0].ID, calls[0].ID)
	}
}

const shellStartGraceMillis = 1000

func screenAfterTwoInterrupts(t *testing.T, name, command string) string {
	t.Helper()
	dir := drivenProject(t)
	var set, errOut bytes.Buffer
	if code := settingsVerb([]string{"set", "chatShowsTools", "true"}, &set, &errOut); code != exitOK {
		t.Fatalf("settings set chatShowsTools true exited %d: %s", code, errOut.String())
	}
	deck := written(t, dir, name+".cassette",
		`{"text":"running the shell","tools":[{"name":"bash","args":{"command":"`+command+`"}}]}`+"\n")
	steps := []string{
		"wait " + session.Placeholder,
		"type run the shell",
		"key enter",
		"wait working",
	}
	for range shellStartGraceMillis / konst.DriveSettleMillis {
		steps = append(steps, "wait working")
	}
	steps = append(steps, "key ctrl+c", "key ctrl+c", "wait "+cancelledAt, "screen")
	script := written(t, dir, name+".drive", strings.Join(steps, "\n"))
	var out bytes.Buffer
	if code := driveVerb([]string{script, "--cassette", deck, "--plain", "--timeout", "60s"}, strings.NewReader(""), &out, &errOut); code != exitOK {
		t.Fatalf("tofu drive exited %d: %s", code, errOut.String())
	}
	return out.String()
}

func TestAShellStoppedPartWayThroughShowsWhatItPrintedAndSaysNoResultOnlyWhenItPrintedNothing(t *testing.T) {
	printed := screenAfterTwoInterrupts(t, "printing", "printf HALFWAY; sleep 3")
	if said := strings.Count(printed, "HALFWAY"); said < 2 {
		t.Errorf("HALFWAY appears %d times, so the call is drawn without the output it had already printed:\n%s", said, printed)
	}
	if strings.Contains(printed, "no result") {
		t.Errorf("the chat says the stopped shell had no result while it printed one:\n%s", printed)
	}
	t.Log("\n" + printed)

	silent := screenAfterTwoInterrupts(t, "silent", "sleep 3")
	if !strings.Contains(silent, "no result") {
		t.Errorf("a killed shell printed nothing and the chat claims something came back:\n%s", silent)
	}
	t.Log("\n" + silent)
}

func TestATurnDrivenThroughTheAppFillsTheContextMeter(t *testing.T) {
	dir := scratchProject(t)
	driver := driveApp(t)
	stubbedTurn(dir, noteThenStop())(t.Context(), onTheSubscription, strings.Repeat("carry this task. ", 600), driver.emit)

	carried := driver.of(tui.EventContext)
	if len(carried) == 0 {
		t.Fatal("the turn emitted no context event, so the bar can only say context unread")
	}
	first := carried[0].Context
	if first.Used <= 0 || first.Budget != konst.ContextCeilingTokens {
		t.Fatalf("context %+v, want what is carried against the ceiling", first)
	}
	screen := driver.view()
	if strings.Contains(screen, "context unread") {
		t.Errorf("the bar still says context unread:\n%s", screen)
	}
	if !strings.Contains(screen, "/250k") {
		t.Errorf("the bar does not show what is carried against the budget:\n%s", screen)
	}
	t.Logf("context events %d, first %+v", len(carried), first)
	t.Log("\n" + screen)
}

func TestADrivenTurnNamesTheSessionAndItsIDInTheHeader(t *testing.T) {
	dir := scratchProject(t)
	driver := driveApp(t)
	stubbedTurn(dir, noteThenStop())(t.Context(), onTheSubscription, "write a note", driver.emit)

	screen := ansi.Strip(driver.view(tea.WindowSizeMsg{Width: 120, Height: 24}))
	if !strings.Contains(screen, "[session#") {
		t.Fatalf("the header does not carry the session id:\n%s", screen)
	}
	if strings.Contains(screen, "#turn-1") {
		t.Fatalf("the header shows the id's constant literal prefix instead of a distinguishing suffix:\n%s", screen)
	}
	store, err := sessionstore.Open()
	if err != nil {
		t.Fatal(err)
	}
	listing, err := store.Listing()
	if err != nil {
		t.Fatal(err)
	}
	if len(listing.Sessions) != 1 || listing.Sessions[0].Name == nil {
		t.Fatalf("listing %+v, want the one session with a name", listing.Sessions)
	}
	if !strings.Contains(screen, " "+*listing.Sessions[0].Name+" [session#") {
		t.Fatalf("the header does not carry the session name %q:\n%s", *listing.Sessions[0].Name, screen)
	}
}

func TestAForkLeavesALineOnTheScreenAPersonCanRead(t *testing.T) {
	dir := scratchProject(t)
	driver := driveApp(t)
	overBudget := strings.Repeat("x", recall.ShippedBands().Target()*3)
	stubbedTurn(dir, noteThenStop())(t.Context(), onTheSubscription, overBudget, driver.emit)

	screen := driver.view()
	if !strings.Contains(screen, "forked into ") {
		t.Fatalf("the screen carries no fork line, so a person never learns the turn forked:\n%s", screen)
	}
	if strings.Contains(screen, frame.ForkNotice) {
		t.Errorf("the screen still carries the notice a person cannot read %q:\n%s", frame.ForkNotice, screen)
	}
	t.Log("\n" + screen)
}

func forkingStep(t *testing.T, store *sessionstore.Store) turn.StepRow {
	t.Helper()
	listing, err := store.Listing()
	if err != nil {
		t.Fatalf("list the sessions the turn wrote: %v", err)
	}
	for _, header := range listing.Sessions {
		events, err := store.Body(header.ID)
		if err != nil {
			t.Fatalf("read the body of %s: %v", header.ID, err)
		}
		steps, err := contextSteps(events)
		if err != nil {
			t.Fatalf("read the steps of %s: %v", header.ID, err)
		}
		for _, step := range steps {
			if step.Fork != nil && step.Occupancy != nil {
				return step
			}
		}
	}
	t.Fatal("no recorded step both measured the request it sent and forked, so there is nothing to compare the bar against")
	return turn.StepRow{}
}

func TestTheBarShowsTheRequestAsSentAndTheForkDecidedOnWhatCameBackAfterIt(t *testing.T) {
	dir := scratchProject(t)
	driver := driveApp(t)
	overBudget := strings.Repeat("x", recall.ShippedBands().Target()*3)
	stubbedTurn(dir, noteThenStop())(t.Context(), onTheSubscription, overBudget, driver.emit)

	store, err := sessionstore.Open()
	if err != nil {
		t.Fatalf("open the sessions the turn wrote: %v", err)
	}
	forking := forkingStep(t, store)
	cfg, err := recall.LoadConfig()
	if err != nil {
		t.Fatalf("read the recall library: %v", err)
	}
	onWire := cfg.OnWire(wireSubscription)

	asked, appended := "", 0
	for _, call := range forking.ToolCalls {
		asked += "\n" + call.Tool + " " + string(call.Args)
		appended += onWire.MessageTokens(strings.Repeat("x", call.RenderedBytes))
	}
	appended += onWire.MessageTokens(asked)

	asSent, decided := forking.Occupancy.Total(), forking.Fork.TokensBefore
	if decided-asSent != appended {
		t.Fatalf("step %d sent %d tokens and the fork decided on %d, a gap of %d, where the call and its %d results are %d",
			forking.Index, asSent, decided, decided-asSent, len(forking.ToolCalls), appended)
	}

	var shown []int
	for _, event := range driver.of(tui.EventContext) {
		shown = append(shown, event.Context.Used)
	}
	if !slices.Contains(shown, asSent) {
		t.Fatalf("step %d sent a request of %d tokens and the bar was shown %v", forking.Index, asSent, shown)
	}
	if slices.Contains(shown, decided) {
		t.Fatalf("the bar was shown %d, the conversation after step %d's results, rather than the request it sent", decided, forking.Index)
	}
	t.Logf("step %d sent %d tokens, the fork decided on %d, and the %d between them are the call and its results", forking.Index, asSent, decided, appended)
}

func TestASpawnedChildShowsInTheSubAgentViewWithTheGlobsItHolds(t *testing.T) {
	dir := scratchProject(t)
	spawnCall := llm.ToolCall{ID: "call-1", Name: "spawn", Arguments: json.RawMessage(`{"task":"write note.txt","owns":["note.txt"]}`)}
	model := &queuedModel{decisions: []llm.Decision{
		{Build: "stub-model", Outcome: llm.OutcomeToolCalls, ToolCalls: []llm.ToolCall{spawnCall}},
		{Build: "stub-model", Outcome: llm.OutcomeToolCalls, ToolCalls: []llm.ToolCall{writeNote("call-2")}},
		{Build: "stub-model", Outcome: llm.OutcomeMessage, Content: "the child wrote it"},
		{Build: "stub-model", Outcome: llm.OutcomeMessage, Content: "the child did it"},
	}}
	driver := driveApp(t)
	stubbedTurn(dir, model)(t.Context(), onTheSubscription, "hand the note to a child", driver.emit)

	subAgentEvents := driver.of(tui.EventSubAgent)
	if len(subAgentEvents) < 2 {
		t.Fatalf("sub-agent events %d, want one when the child starts, one per step it takes, and one when it reports", len(subAgentEvents))
	}
	started := subAgentEvents[0].Children
	if len(started) != 1 || started[0].State != roster.Working || !slices.Equal(started[0].Owns, []string{"note.txt"}) {
		t.Fatalf("the first sub-agent event carries %+v, want one running child holding note.txt", started)
	}
	ended := subAgentEvents[len(subAgentEvents)-1].Children[0]
	if ended.State != roster.Finished || ended.Steps != 2 || ended.Report == "" {
		t.Fatalf("the child ended as %+v, want it finished, since no done review runs in the app, with the steps and the report the roster carries", ended)
	}
	screen := pickedFirstChild(t, driver)
	for _, want := range []string{"1 agents", "[&sub-1]", "write note.txt", "owns", "note.txt"} {
		if !strings.Contains(screen, want) {
			t.Errorf("the sub-agent view does not show %q:\n%s", want, screen)
		}
	}
	t.Log("\n" + screen)
}

func pickedFirstChild(t *testing.T, driver *appDriver) string {
	t.Helper()
	rail := ansi.Strip(driver.view(tea.WindowSizeMsg{Width: 120, Height: 40}, tea.KeyPressMsg{Code: '2', Mod: tea.ModAlt}))
	row := slices.IndexFunc(strings.Split(rail, "\n"), func(line string) bool { return strings.HasPrefix(strings.TrimSpace(line), "[&sub-1]") })
	if row < 0 {
		t.Fatalf("the sub-agents rail names no [&sub-1]:\n%s", rail)
	}
	const railColumn = 14
	return ansi.Strip(driver.view(tea.MouseClickMsg{X: railColumn, Y: row, Button: tea.MouseLeft}, tea.MouseReleaseMsg{X: railColumn, Y: row, Button: tea.MouseLeft}))
}

type toolNamesCapture struct {
	names []string
}

func (m *toolNamesCapture) Ask(_ context.Context, req llm.Request) (llm.Decision, error) {
	for _, tool := range req.Tools {
		m.names = append(m.names, tool.Name)
	}
	return llm.Decision{Build: "stub-model", Outcome: llm.OutcomeMessage, Content: "done"}, nil
}

func TestATurnStartedInTheAppOffersSpawnWhenTurnMaySpawnIsOn(t *testing.T) {
	dir := scratchProject(t)
	driver := driveApp(t)
	capture := &toolNamesCapture{}
	stubbedTurn(dir, capture)(t.Context(), onTheSubscription, "say hello", driver.emit)
	if !slices.Contains(capture.names, "spawn") {
		t.Fatalf("turnMaySpawn defaults to on and the app offered %v, want spawn among them", capture.names)
	}
}

func TestTheAppResolvesReadBeforeEditFromSettingsAndRefusesABlindEditByDefault(t *testing.T) {
	dir := scratchProject(t)
	if err := os.WriteFile(filepath.Join(dir, "note.txt"), []byte("a note\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	driver := driveApp(t)
	model := &queuedModel{decisions: []llm.Decision{
		{Build: "stub-model", Outcome: llm.OutcomeToolCalls, ToolCalls: []llm.ToolCall{
			callTo("call-1", "edit", `{"path":"note.txt","old_string":"a note","new_string":"another note"}`)}},
		{Build: "stub-model", Outcome: llm.OutcomeMessage, Content: "done"},
	}}
	stubbedTurn(dir, model)(t.Context(), onTheSubscription, "edit the note", driver.emit)

	results := driver.of(tui.EventToolResult)
	if len(results) != 1 || !results[0].Failed {
		t.Fatalf("the app resolves readBeforeEdit on by default and must refuse a blind edit: %+v", results)
	}
}

func TestTheAppResolvesReadBeforeEditOffAndAllowsTheSameBlindEdit(t *testing.T) {
	dir := scratchProject(t)
	disableReadBeforeEdit(t)
	if err := os.WriteFile(filepath.Join(dir, "note.txt"), []byte("a note\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	driver := driveApp(t)
	model := &queuedModel{decisions: []llm.Decision{
		{Build: "stub-model", Outcome: llm.OutcomeToolCalls, ToolCalls: []llm.ToolCall{
			callTo("call-1", "edit", `{"path":"note.txt","old_string":"a note","new_string":"another note"}`)}},
		{Build: "stub-model", Outcome: llm.OutcomeMessage, Content: "done"},
	}}
	stubbedTurn(dir, model)(t.Context(), onTheSubscription, "edit the note", driver.emit)

	results := driver.of(tui.EventToolResult)
	if len(results) != 1 || results[0].Failed {
		t.Fatalf("readBeforeEdit off must let the app's blind edit through: %+v", results)
	}
}

func TestATurnStartedInTheAppDoesNotOfferSpawnWhenTurnMaySpawnIsOff(t *testing.T) {
	dir := scratchProject(t)
	var out, errOut bytes.Buffer
	if code := settingsVerb([]string{"set", "turnMaySpawn", "false"}, &out, &errOut); code != exitOK {
		t.Fatalf("settings set exited %d: %s", code, errOut.String())
	}
	driver := driveApp(t)
	capture := &toolNamesCapture{}
	stubbedTurn(dir, capture)(t.Context(), onTheSubscription, "say hello", driver.emit)
	if slices.Contains(capture.names, "spawn") {
		t.Fatalf("turnMaySpawn is off and the app still offered spawn: %v", capture.names)
	}
}

type clockedStep struct {
	waited   time.Duration
	holds    time.Duration
	decision llm.Decision
}

type clockedModel struct {
	at    atomic.Int64
	steps []clockedStep
}

func clockedFrom(at time.Time, steps ...clockedStep) *clockedModel {
	model := &clockedModel{steps: steps}
	model.at.Store(at.UnixNano())
	return model
}

func (m *clockedModel) clock() time.Time { return time.Unix(0, m.at.Load()) }

func (m *clockedModel) Ask(_ context.Context, _ llm.Request) (llm.Decision, error) {
	if len(m.steps) == 0 {
		return llm.Decision{}, errors.New("clockedModel: no more decisions queued")
	}
	next := m.steps[0]
	m.steps = m.steps[1:]
	if next.holds == 0 {
		m.at.Add(int64(next.waited))
		return next.decision, nil
	}
	for range heldSlices {
		time.Sleep(next.holds / heldSlices)
		m.at.Add(int64(next.waited) / heldSlices)
	}
	return next.decision, nil
}

func TestAChildRunningForTenSecondsReadsTenSecondsAndWhatItSpent(t *testing.T) {
	dir := scratchProject(t)
	spawnCall := llm.ToolCall{ID: "call-1", Name: "spawn", Arguments: json.RawMessage(`{"task":"write note.txt","owns":["note.txt"]}`)}
	model := clockedFrom(time.Date(2026, 9, 20, 9, 0, 0, 0, time.UTC),
		clockedStep{decision: llm.Decision{Build: "stub-model", Outcome: llm.OutcomeToolCalls, ToolCalls: []llm.ToolCall{spawnCall}}},
		clockedStep{waited: 10 * time.Second, decision: llm.Decision{
			Build:     "stub-model",
			Outcome:   llm.OutcomeToolCalls,
			ToolCalls: []llm.ToolCall{writeNote("call-2")},
			Usage:     llm.Usage{InputTokens: 11000, OutputTokens: 1000},
		}},
		clockedStep{decision: llm.Decision{Build: "stub-model", Outcome: llm.OutcomeMessage, Content: "the child wrote it"}},
		clockedStep{decision: llm.Decision{Build: "stub-model", Outcome: llm.OutcomeMessage, Content: "the child did it"}},
	)
	driver := driveApp(t)
	newAppSession(dir, func(runOpts) (appWire, error) {
		return wireOn(model), nil
	}, nil, model.clock, sessionResume{}).run(t.Context(), onTheSubscription, "hand the note to a child", driver.emit)

	running := ""
	for _, framed := range driver.frames {
		for _, row := range strings.Split(framed, "\n") {
			if strings.Contains(row, "sub-1") && strings.Contains(row, "write note.txt") && !strings.Contains(row, "spawning") {
				running = row
			}
		}
	}
	if running == "" {
		t.Fatal("no frame carried the running child at all")
	}
	t.Logf("the activity row read %q", running)
	if !strings.Contains(running, "10s") {
		t.Errorf("the child ran for ten seconds and its row reads %q", running)
	}
	if !strings.Contains(running, "12k") {
		t.Errorf("the child spent 12000 tokens and its row reads %q", running)
	}
}

const (
	heldInsideOneCall = 700 * time.Millisecond
	heldSlices        = 7
	childHeldFor      = 70 * time.Second
	parentHeldFor     = 30 * time.Second
)

func childHeldInsideOneCall(t *testing.T) *appDriver {
	t.Helper()
	dir := scratchProject(t)
	spawnCall := llm.ToolCall{ID: "call-1", Name: "spawn", Arguments: json.RawMessage(`{"task":"write note.txt","owns":["note.txt"]}`)}
	reply := func(text string) llm.Decision {
		return llm.Decision{Build: "stub-model", Outcome: llm.OutcomeMessage, Content: text}
	}
	model := clockedFrom(time.Date(2026, 9, 24, 9, 0, 0, 0, time.UTC),
		clockedStep{decision: llm.Decision{Build: "stub-model", Outcome: llm.OutcomeToolCalls, ToolCalls: []llm.ToolCall{spawnCall}}},
		clockedStep{decision: llm.Decision{Build: "stub-model", Outcome: llm.OutcomeToolCalls, ToolCalls: []llm.ToolCall{writeNote("call-2")}}},
		clockedStep{waited: childHeldFor, holds: heldInsideOneCall, decision: reply("the child wrote it")},
		clockedStep{waited: parentHeldFor, holds: heldInsideOneCall, decision: reply("the child did it")},
	)
	driver := driveApp(t)
	newAppSession(dir, func(runOpts) (appWire, error) {
		return wireOn(model), nil
	}, nil, model.clock, sessionResume{}).run(t.Context(), onTheSubscription, "hand the note to a child", driver.emit)
	return driver
}

func (d *appDriver) childClocks(state subagent.State) map[time.Duration]string {
	d.mutex.Lock()
	defer d.mutex.Unlock()
	drawn := map[time.Duration]string{}
	for index, event := range d.events {
		if event.Kind != tui.EventSubAgent {
			continue
		}
		for _, child := range event.Children {
			if child.State == state {
				drawn[child.Since] = d.frames[index]
			}
		}
	}
	return drawn
}

func TestARunningChildsClockAdvancesWhileItIsHeldInsideOneCall(t *testing.T) {
	driver := childHeldInsideOneCall(t)

	drawn := driver.childClocks(roster.Working)
	var moved []time.Duration
	for clock := range drawn {
		if clock > 0 && clock < childHeldFor {
			moved = append(moved, clock)
		}
	}
	if len(moved) == 0 {
		t.Fatalf("a child held %s inside one call was drawn only at %v: a clock that moves once the call answers says the child is stuck while it works",
			widget.Until(childHeldFor), slices.Sorted(maps.Keys(drawn)))
	}
	t.Logf("the running child was drawn at %v", slices.Sorted(maps.Keys(drawn)))
	for _, clock := range moved {
		if reads := widget.Until(clock); !strings.Contains(drawn[clock], reads) {
			t.Errorf("the child was drawn at %s and no screen of that frame reads it:\n%s", reads, drawn[clock])
		}
	}
}

func TestAChildThatHasHandedBackKeepsTheClockItStoppedAt(t *testing.T) {
	driver := childHeldInsideOneCall(t)

	drawn := driver.childClocks(roster.Finished)
	if len(drawn) == 0 {
		t.Fatal("no frame carried a child that had handed back")
	}
	t.Logf("the child that had stopped was drawn at %v", slices.Sorted(maps.Keys(drawn)))
	for clock := range drawn {
		if clock != childHeldFor {
			t.Errorf("the child took %s and was drawn at %s after it stopped: a stopped clock may not keep counting",
				widget.Until(childHeldFor), widget.Until(clock))
		}
	}
}

func TestAFullEventChannelDropsTheSnapshotsAndNeverBlocksTheTurn(t *testing.T) {
	const kept, dropped = 200, 4096
	finished := make(chan struct{})
	app := tui.New(tui.Options{
		Repo:  "scratch",
		Wires: func() []tui.Wire { return []tui.Wire{{Name: wireSubscription, Model: "stub-model"}} },
		Turn: func(_ context.Context, _ tui.Pick, _ string, emit tui.CalledFromInsideTheTurnAndNeverAfterItReturns) {
			for step := range kept {
				emit(tui.Event{Kind: tui.EventText, Text: "step " + strconv.Itoa(step)})
			}
			for carried := range dropped {
				emit(tui.Event{Kind: tui.EventContext, Context: frame.Context{Used: carried + 1, Budget: konst.ContextCeilingTokens}})
			}
			close(finished)
		},
	})
	app.Init()
	app.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	for _, letter := range "fill the channel and never read it" {
		app.Update(tea.KeyPressMsg{Code: letter, Text: string(letter)})
	}
	_, started := app.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	select {
	case <-finished:
	case <-time.After(20 * time.Second):
		t.Fatal("the turn never finished: the interface stopped reading and the engine waited for it")
	}

	texts, contexts := 0, 0
	for pending := []tea.Cmd{started}; len(pending) > 0; pending = pending[1:] {
		if pending[0] == nil {
			continue
		}
		switch msg := pending[0]().(type) {
		case tea.BatchMsg:
			pending = append(pending, msg...)
		case tui.Event:
			if msg.Kind == tui.EventText {
				texts++
			}
			if msg.Kind == tui.EventContext {
				contexts++
			}
			_, next := app.Update(msg)
			pending = append(pending, next)
		}
	}
	if texts != kept {
		t.Errorf("the channel carried %d of the %d text events: an increment may never be dropped", texts, kept)
	}
	if contexts == 0 || contexts >= dropped {
		t.Errorf("the channel carried %d of the %d context events, want some dropped and the state still shown", contexts, dropped)
	}
	t.Logf("kept every one of the %d text events and %d of the %d snapshots", texts, contexts, dropped)
}

func TestATurnWithNoChildrenSendsNoSubAgentEventAtAll(t *testing.T) {
	dir := scratchProject(t)
	driver := driveApp(t)
	stubbedTurn(dir, noteThenStop())(t.Context(), onTheSubscription, "write the note yourself", driver.emit)

	if sent := driver.of(tui.EventSubAgent); len(sent) != 0 {
		t.Fatalf("sub-agent events %+v, want none: an empty sub-agent view has to keep saying what it says today", sent)
	}
	screen := driver.view(tea.KeyPressMsg{Code: '2', Mod: tea.ModAlt})
	if !strings.Contains(screen, "0 agents") {
		t.Errorf("the sub-agent view is not the empty one:\n%s", screen)
	}
}

func callTo(id, tool, arguments string) llm.ToolCall {
	return llm.ToolCall{ID: id, Name: tool, Arguments: json.RawMessage(arguments)}
}

func TestEveryToolSaysWhetherItFailedAndHowBigItsResultWas(t *testing.T) {
	const note = "a note\n"
	for _, one := range []struct {
		tool    string
		failing llm.ToolCall
		ran     llm.ToolCall
		bytes   int
	}{
		{
			tool:    "bash",
			failing: callTo("call-1", "bash", `{"command":"exit 3"}`),
			ran:     callTo("call-2", "bash", `{"command":"printf hello"}`),
			bytes:   len("hello"),
		},
		{
			tool:    "read",
			failing: callTo("call-1", "read", `{"path":"absent.txt"}`),
			ran:     callTo("call-2", "read", `{"path":"note.txt"}`),
			bytes:   len(note),
		},
		{
			tool:    "write",
			failing: callTo("call-1", "write", `{"path":"../outside.txt","content":"x"}`),
			ran:     callTo("call-2", "write", `{"path":"note.txt","content":"a note"}`),
		},
		{
			tool:    "edit",
			failing: callTo("call-1", "edit", `{"path":"note.txt","old_string":"no line reads this","new_string":"x"}`),
			ran:     callTo("call-2", "edit", `{"path":"note.txt","old_string":"a note","new_string":"another note"}`),
		},
	} {
		t.Run(one.tool, func(t *testing.T) {
			dir := scratchProject(t)
			disableReadBeforeEdit(t)
			if err := os.WriteFile(filepath.Join(dir, "note.txt"), []byte(note), 0o644); err != nil {
				t.Fatal(err)
			}
			model := &queuedModel{decisions: []llm.Decision{
				{Build: "stub-model", Outcome: llm.OutcomeToolCalls, ToolCalls: []llm.ToolCall{one.failing}},
				{Build: "stub-model", Outcome: llm.OutcomeToolCalls, ToolCalls: []llm.ToolCall{one.ran}},
				{Build: "stub-model", Outcome: llm.OutcomeMessage, Content: "done"},
			}}
			driver := driveApp(t)
			stubbedTurn(dir, model)(t.Context(), onTheSubscription, "fail once and then succeed", driver.emit)

			results := driver.of(tui.EventToolResult)
			if len(results) != 2 {
				t.Fatalf("the interface saw %d results, want the failed call and the one that ran: %+v", len(results), results)
			}
			if !results[0].Failed {
				t.Errorf("%s failed and the interface was told %+v", one.tool, results[0])
			}
			if results[1].Failed {
				t.Errorf("%s ran and the interface was told it failed: %+v", one.tool, results[1])
			}
			if results[0].Bytes <= 0 || results[1].Bytes <= 0 {
				t.Errorf("a result reached the interface with no size: %+v and %+v", results[0], results[1])
			}
			if one.bytes > 0 && results[1].Bytes != one.bytes {
				t.Errorf("%s produced %d bytes and the interface was handed %d", one.tool, one.bytes, results[1].Bytes)
			}
			t.Logf("failed %+v, ran %+v", results[0], results[1])
		})
	}
}

func TestTheFoldLineOnADrivenTurnNamesTheCountNotTheSize(t *testing.T) {
	dir := scratchProject(t)
	driver := driveApp(t)
	model := &queuedModel{decisions: []llm.Decision{
		{Build: "stub-model", Outcome: llm.OutcomeToolCalls, ToolCalls: []llm.ToolCall{writeNote("call-1")}},
		{Build: "stub-model", Outcome: llm.OutcomeToolCalls, ToolCalls: []llm.ToolCall{writeNote("call-2")}},
		{Build: "stub-model", Outcome: llm.OutcomeToolCalls, ToolCalls: []llm.ToolCall{writeNote("call-3")}},
		{Build: "stub-model", Outcome: llm.OutcomeMessage, Content: "wrote it three times"},
	}}
	stubbedTurn(dir, model)(t.Context(), onTheSubscription, "write the note three times", driver.emit)

	total := 0
	for _, result := range driver.of(tui.EventToolResult) {
		total += result.Bytes
	}
	if total == 0 {
		t.Fatal("the three results carried no bytes at all, so this test proves nothing")
	}
	screen := driver.view()
	if !strings.Contains(screen, "(3) tools") {
		t.Errorf("the fold line does not name the count\n%s", screen)
	}
	if strings.Contains(screen, widget.Size(total)) {
		t.Errorf("the fold line still carries a byte figure\n%s", screen)
	}
	t.Log("\n" + screen)
}

func editsKey() tea.KeyPressMsg { return tea.KeyPressMsg{Code: '3', Mod: tea.ModAlt} }

func diffRows(feed, sign, text string) int {
	drawn := 0
	for _, row := range strings.Split(feed, "\n") {
		fields, at := strings.Fields(row), len(strings.Fields(text))+2
		if len(fields) >= at && fields[len(fields)-at] == sign && strings.HasSuffix(strings.TrimSpace(row), text) {
			drawn++
		}
	}
	return drawn
}

func readingTheEdit() []tea.Msg {
	return []tea.Msg{tea.WindowSizeMsg{Width: 120, Height: 40}, editsKey(), tea.KeyPressMsg{Code: tea.KeyEnter}, tea.KeyPressMsg{Code: tea.KeyEnter}}
}

func TestARealEditReachesTheFileEditsViewAndLeavesOneRowBehind(t *testing.T) {
	dir := scratchProject(t)
	disableReadBeforeEdit(t)
	if err := os.WriteFile(filepath.Join(dir, "note.txt"), []byte("a note\nand another\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	driver := driveApp(t)
	model := &queuedModel{decisions: []llm.Decision{
		{Build: "stub-model", Outcome: llm.OutcomeToolCalls, ToolCalls: []llm.ToolCall{
			callTo("call-1", "edit", `{"path":"note.txt","old_string":"a note","new_string":"a longer note"}`)}},
		{Build: "stub-model", Outcome: llm.OutcomeMessage, Content: "done"},
	}}
	stubbedTurn(dir, model)(t.Context(), onTheSubscription, "make the note longer", driver.emit)

	results := driver.of(tui.EventToolResult)
	if len(results) != 1 {
		t.Fatalf("the interface saw %d results, want the one edit", len(results))
	}
	if !strings.HasPrefix(results[0].Diff, "--- note.txt") {
		t.Fatalf("the edit tool's diff did not reach the interface: %+v", results[0])
	}

	transcript := driver.view()
	if strings.Contains(transcript, "edit note.txt") {
		t.Errorf("chat drew the call that made the edit, it belongs in work\n%s", transcript)
	}
	if strings.Contains(transcript, "a longer note") {
		t.Errorf("the transcript drew the diff body\n%s", transcript)
	}
	worked := ansi.Strip(driver.view(tea.KeyPressMsg{Code: '2', Mod: tea.ModAlt}))
	if !strings.Contains(worked, "file edit") || !strings.Contains(worked, "note.txt") || !strings.Contains(worked, "+1  -1") {
		t.Errorf("the sub-agents feed does not say which file changed and by how much\n%s", worked)
	}
	feed := ansi.Strip(driver.view(readingTheEdit()...))
	for _, want := range []string{"MODIFIED", "note.txt", "+1  -1", "and another"} {
		if !strings.Contains(feed, want) {
			t.Errorf("the file edits view does not show %q\n%s", want, feed)
		}
	}
	if diffRows(feed, "-", "a note") != 1 || diffRows(feed, "+", "a longer note") != 1 {
		t.Errorf("the file edits view does not draw the removed and the added line\n%s", feed)
	}
	t.Log("\n" + feed)
}

const createdLines = 16

func sixteenLines() string {
	var content strings.Builder
	for line := 1; line <= createdLines; line++ {
		content.WriteString("line " + strconv.Itoa(line) + "\n")
	}
	return content.String()
}

func writeCall(t *testing.T, id, path, content string) llm.ToolCall {
	t.Helper()
	args, err := json.Marshal(map[string]string{"path": path, "content": content})
	if err != nil {
		t.Fatal(err)
	}
	return callTo(id, "write", string(args))
}

func TestACreatedFileShowsEveryLineItWroteMarkedAsAdded(t *testing.T) {
	dir := scratchProject(t)
	driver := driveApp(t)
	model := &queuedModel{decisions: []llm.Decision{
		{Build: "stub-model", Outcome: llm.OutcomeToolCalls, ToolCalls: []llm.ToolCall{
			writeCall(t, "call-1", "note.txt", sixteenLines())}},
		{Build: "stub-model", Outcome: llm.OutcomeToolCalls, ToolCalls: []llm.ToolCall{
			callTo("call-2", "read", `{"path":"note.txt"}`)}},
		{Build: "stub-model", Outcome: llm.OutcomeMessage, Content: "done"},
	}}
	stubbedTurn(dir, model)(t.Context(), onTheSubscription, "write the note", driver.emit)

	feed := ansi.Strip(driver.view(readingTheEdit()...))
	drawn := 0
	for line := 1; line <= createdLines; line++ {
		drawn += diffRows(feed, "+", "line "+strconv.Itoa(line))
	}
	if drawn != createdLines {
		t.Errorf("the feed drew %d added lines for a %d line file\n%s", drawn, createdLines, feed)
	}
	for _, want := range []string{"ADDED", "note.txt", "+16  +0"} {
		if !strings.Contains(feed, want) {
			t.Errorf("the file edits view does not show %q\n%s", want, feed)
		}
	}
	t.Log("\n" + feed)
}

func TestAWholeReplacementShowsItsDiffRatherThanItsContent(t *testing.T) {
	dir := scratchProject(t)
	disableReadBeforeEdit(t)
	if err := os.WriteFile(filepath.Join(dir, "note.txt"), []byte("a note\nand another\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	driver := driveApp(t)
	model := &queuedModel{decisions: []llm.Decision{
		{Build: "stub-model", Outcome: llm.OutcomeToolCalls, ToolCalls: []llm.ToolCall{
			writeCall(t, "call-1", "note.txt", "a longer note\nand another\n")}},
		{Build: "stub-model", Outcome: llm.OutcomeMessage, Content: "done"},
	}}
	stubbedTurn(dir, model)(t.Context(), onTheSubscription, "replace the note", driver.emit)

	results := driver.of(tui.EventToolResult)
	if len(results) != 1 || !strings.HasPrefix(results[0].Diff, "--- note.txt") {
		t.Fatalf("a whole replacement did not reach the interface as a diff: %+v", results)
	}
	if results[0].Created != "" {
		t.Errorf("a replacement was carried as a created file: %+v", results[0])
	}
	feed := ansi.Strip(driver.view(readingTheEdit()...))
	for _, want := range []string{"MODIFIED", "+1  -1"} {
		if !strings.Contains(feed, want) {
			t.Errorf("the file edits view does not show %q\n%s", want, feed)
		}
	}
	if diffRows(feed, "-", "a note") != 1 || diffRows(feed, "+", "a longer note") != 1 {
		t.Errorf("a whole replacement is not drawn as its diff\n%s", feed)
	}
}

func TestAFailedWriteIsNotAnEdit(t *testing.T) {
	dir := scratchProject(t)
	driver := driveApp(t)
	model := &queuedModel{decisions: []llm.Decision{
		{Build: "stub-model", Outcome: llm.OutcomeToolCalls, ToolCalls: []llm.ToolCall{
			callTo("call-1", "write", `{"path":"../outside.txt","content":"one\ntwo\n"}`)}},
		{Build: "stub-model", Outcome: llm.OutcomeMessage, Content: "done"},
	}}
	stubbedTurn(dir, model)(t.Context(), onTheSubscription, "write outside the root", driver.emit)

	results := driver.of(tui.EventToolResult)
	if len(results) != 1 || !results[0].Failed {
		t.Fatalf("the interface saw %d results and the first did not fail: %+v", len(results), results)
	}
	if results[0].Created != "" || results[0].Diff != "" {
		t.Errorf("a refused write was carried into the file edits view: %+v", results[0])
	}
	if feed := driver.view(editsKey()); !strings.Contains(feed, "no file has changed") {
		t.Errorf("the file edits view claims a refused write changed something\n%s", feed)
	}
}

func answeredByKeys(t *testing.T, dir string, model turn.Model, keys ...string) []tui.Event {
	t.Helper()
	answers := make(chan tui.Answer, 1)
	app := tui.New(tui.Options{
		Repo:    "scratch",
		Wires:   func() []tui.Wire { return []tui.Wire{{Name: wireSubscription, Model: "stub-model"}} },
		Answers: answers,
		Turn:    resumedTurn(dir, model, answers, sessionResume{}),
	})
	app.Init()
	app.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	for _, letter := range "do the work" {
		app.Update(tea.KeyPressMsg{Code: letter, Text: string(letter)})
	}
	var seen []tui.Event
	waits, unanswered := 0, ""
	_, started := app.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	for pending := []tea.Cmd{started}; len(pending) > 0; pending = pending[1:] {
		if pending[0] == nil {
			continue
		}
		switch msg := pending[0]().(type) {
		case tea.BatchMsg:
			pending = append(pending, msg...)
		case tui.Event:
			seen = append(seen, msg)
			_, next := app.Update(msg)
			pending = append(pending, next)
			if msg.Kind != tui.EventAwaitPerson || unanswered != "" {
				continue
			}
			key := keys[min(waits, len(keys)-1)]
			app.Update(tea.KeyPressMsg{Code: rune(key[0]), Text: key})
			if strings.Contains(app.View().Content, "[1] allow once") {
				unanswered = key
				app.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
			}
			waits++
		}
	}
	if unanswered != "" {
		t.Fatalf("%q did not answer the open question and the turn waited until it was stopped", unanswered)
	}
	return seen
}

type refusingModel struct{ err error }

func (m refusingModel) Ask(context.Context, llm.Request) (llm.Decision, error) {
	return llm.Decision{}, m.err
}

func TestACancelledTurnReportsStoppedAndCarriesNoGoError(t *testing.T) {
	dir := scratchProject(t)
	cancelled := transport.Fail("anthropic.Ask", transport.KindProvider, context.Canceled,
		"posting the request: Post %q", "https://api.anthropic.com/v1/messages?beta=true")
	driver := driveApp(t)
	stubbedTurn(dir, refusingModel{err: cancelled})(t.Context(), onTheSubscription, "a task", driver.emit)

	if failures := driver.of(tui.EventFailure); len(failures) != 0 {
		t.Fatalf("a cancelled turn reported a failure: %q", failures[0].Text)
	}
	done := driver.of(tui.EventDone)
	if len(done) != 1 {
		t.Fatalf("the turn closed with %d done events, want one", len(done))
	}
	if done[0].Text != cancelledAt {
		t.Errorf("the closing line reads %q, want a turn that reports as stopped", done[0].Text)
	}
	screen := driver.view()
	for _, forbidden := range []string{"http", "context canceled"} {
		if strings.Contains(screen, forbidden) {
			t.Errorf("the screen carries %q after a cancel\n%s", forbidden, screen)
		}
	}
	t.Logf("the cancel was reported as %q", done[0].Text)
}

func TestATurnThatFailsForAnotherReasonStillReportsTheError(t *testing.T) {
	dir := scratchProject(t)
	broken := errors.New("the provider answered with no content")
	driver := driveApp(t)
	stubbedTurn(dir, refusingModel{err: broken})(t.Context(), onTheSubscription, "a task", driver.emit)

	failures := driver.of(tui.EventFailure)
	if len(failures) != 1 || !strings.Contains(failures[0].Text, broken.Error()) {
		t.Fatalf("the failure was reported as %+v, want the error text", failures)
	}
	done := driver.of(tui.EventDone)
	if len(done) != 1 || done[0].Text != doneWords(turn.OutcomeError, nil) {
		t.Fatalf("the closing line reads %+v, want a turn that reports as an error", done)
	}
}

func countedKind(events []tui.Event, kind tui.EventKind) int {
	counted := 0
	for _, event := range events {
		if event.Kind == kind {
			counted++
		}
	}
	return counted
}

func TestTheKeyForAllowRunsTheAskedCallAndTheKeyForDenyRefusesIt(t *testing.T) {
	for _, pressed := range []struct {
		key string
		ran bool
	}{{"1", true}, {"2", false}} {
		t.Run(pressed.key, func(t *testing.T) {
			dir, _, _ := gateScratch(t, gateFixtureBuild)
			stubJev(t, 200, middlingRiskAskReply)
			model := &sendModel{queued: []llm.Decision{
				{Build: "stub-model", Outcome: llm.OutcomeToolCalls, ToolCalls: []llm.ToolCall{writeNote("call-1")}},
				{Build: "stub-model", Outcome: llm.OutcomeMessage, Content: "that is done"},
			}}

			events := answeredByKeys(t, dir, model, pressed.key)

			if waits := countedKind(events, tui.EventAwaitPerson); waits != 1 {
				t.Fatalf("the app entered the wait %d times, want once", waits)
			}
			_, statErr := os.Stat(filepath.Join(dir, "note.txt"))
			if pressed.ran != (statErr == nil) {
				t.Fatalf("%q ran the call %v, want %v (stat err %v)", pressed.key, statErr == nil, pressed.ran, statErr)
			}
			shown := ""
			for _, request := range model.requests {
				for _, message := range request.Messages {
					if message.Role == llm.RoleTool {
						shown = message.Content
					}
				}
			}
			if shown == "" {
				t.Fatal("the model was never shown a tool result")
			}
			if pressed.ran && strings.Contains(shown, "refused") {
				t.Fatalf("the person allowed it and the model was told %q", shown)
			}
			if !pressed.ran && !strings.Contains(shown, "the person did not allow it") {
				t.Fatalf("the person refused it and the model was told %q", shown)
			}
			t.Logf("%q: note.txt present %v, the model reads: %s", pressed.key, statErr == nil, shown)
		})
	}
}

func TestAlwaysHereAnswersTheSamePlaceAndNothingElse(t *testing.T) {
	dir, _, _ := gateScratch(t, gateFixtureBuild)
	stubJev(t, 200, middlingRiskAskReply)
	calls := []llm.ToolCall{
		writeNote("call-1"),
		writeNote("call-2"),
		{ID: "call-3", Name: "read", Arguments: json.RawMessage(`{"path":"note.txt"}`)},
		{ID: "call-4", Name: "write", Arguments: json.RawMessage(`{"path":"other.txt","content":"a note"}`)},
	}
	model := &sendModel{}
	for _, call := range calls {
		model.queued = append(model.queued, llm.Decision{
			Build:     "stub-model",
			Outcome:   llm.OutcomeToolCalls,
			ToolCalls: []llm.ToolCall{call},
		})
	}
	model.queued = append(model.queued, llm.Decision{Build: "stub-model", Outcome: llm.OutcomeMessage, Content: "all four"})

	events := answeredByKeys(t, dir, model, "3", "1")

	waits := countedKind(events, tui.EventAwaitPerson)
	if waits != 3 {
		t.Fatalf("four asked calls entered the wait %d times, want three: the second write is the granted place", waits)
	}
	if resumed := countedKind(events, tui.EventResumed); resumed != waits {
		t.Fatalf("the app entered the wait %d times and resumed %d times", waits, resumed)
	}
	for _, path := range []string{"note.txt", "other.txt"} {
		if _, err := os.Stat(filepath.Join(dir, path)); err != nil {
			t.Fatalf("%s was not written: %v", path, err)
		}
	}
	t.Logf("four asked calls, %d waits, and the grant held only for write note.txt", waits)
}

func TestTheDrainTakesEveryQueuedMessageInOrderAndSaysSoOnce(t *testing.T) {
	queue := make(chan string, 4)
	queue <- "read the policy first"
	queue <- "and leave the changelog alone"
	var told []string
	taken := steered(queue, func(event tui.Event) {
		if event.Kind != tui.EventSteered {
			t.Errorf("the drain emitted kind %v, want a steered event", event.Kind)
		}
		told = append(told, event.Text)
	})
	want := []string{"read the policy first", "and leave the changelog alone"}
	if !slices.Equal(taken, want) || !slices.Equal(told, want) {
		t.Fatalf("the drain took %q and said %q, want %q for both", taken, told, want)
	}
	if again := steered(queue, func(tui.Event) { t.Error("an empty queue said something") }); again != nil {
		t.Fatalf("a second drain took %q from an empty queue", again)
	}
}

func TestThePlaceAGrantCoversIsTheToolAndItsSubject(t *testing.T) {
	for _, one := range []struct {
		tool  string
		args  string
		place string
	}{
		{"write", `{"path":"note.txt","content":"a"}`, "write note.txt"},
		{"read", `{"path":"note.txt"}`, "read note.txt"},
		{"bash", `{"command":"git push --force origin main"}`, "bash git push --force"},
		{"bash", `{"command":"git push --force origin develop"}`, "bash git push --force"},
		{"bash", `{"command":"rm -rf build"}`, "bash rm -rf build"},
		{"glob", `{"pattern":"*.go"}`, `glob {"pattern":"*.go"}`},
	} {
		got := askedPlace(turn.GateRequest{Tool: one.tool, Args: json.RawMessage(one.args)})
		if got != one.place {
			t.Errorf("%s %s is the place %q, want %q", one.tool, one.args, got, one.place)
		}
	}
}

func TestEveryOutcomeClosesTheTurnInWordsAndNeverInItsEnumName(t *testing.T) {
	want := map[turn.Outcome]string{
		turn.OutcomeUnset:               "finished in",
		turn.OutcomeForked:              "finished in",
		turn.OutcomeStopped:             "cooked for",
		turn.OutcomeStepCap:             "stopped at the step cap after",
		turn.OutcomeDecisionCap:         "stopped at the decision cap after",
		turn.OutcomeError:               "failed after",
		turn.OutcomeTruncated:           "stopped on a reply it could not finish, after",
		turn.OutcomeRetiredCostCap:      "stopped at a cap this build no longer sets, after",
		turn.OutcomeRetiredWallClockCap: "stopped at a cap this build no longer sets, after",
		turn.OutcomeLoopGuard:           loopGuardWords(nil) + ", after",
	}
	for _, outcome := range turn.AllOutcomes() {
		expected, named := want[outcome]
		if !named {
			t.Fatalf("%s carries no expected closing words in this test, so a new outcome can reach doneWords untested", outcome)
		}
		got := doneWords(outcome, nil)
		if got != expected {
			t.Errorf("%s closes the turn with %q, want %q", outcome, got, expected)
		}
		if strings.Contains(got, "_") {
			t.Errorf("%s closes the turn with its own enum name: %q", outcome, got)
		}
	}
}

func TestSessionVerdictNamesEveryLedgerVerdict(t *testing.T) {
	want := map[ledger.Verdict]session.Verdict{
		ledger.VerdictUnset: session.Ask,
		ledger.VerdictAllow: session.Allow,
		ledger.VerdictAsk:   session.Ask,
		ledger.VerdictDeny:  session.Deny,
	}
	for _, v := range ledger.AllVerdicts() {
		expected, named := want[v]
		if !named {
			t.Fatalf("%s carries no expected session verdict, so a new ledger verdict can reach sessionVerdict untested", v)
		}
		if got := sessionVerdict(v); got != expected {
			t.Errorf("sessionVerdict(%s) = %s, want %s", v, got, expected)
		}
	}
}

func TestTheSubAgentBarFollowsTheCapTheTurnWasGiven(t *testing.T) {
	for _, one := range []struct{ maxSteps, total int }{{0, konst.TurnMaxSteps}, {12, 12}} {
		var sent []tui.Event
		watch := &appWatcher{
			emit:     func(event tui.Event) { sent = append(sent, event) },
			now:      time.Now,
			maxSteps: one.maxSteps,
			held:     rosterHolding(t, roster.SubAgent{ID: "parent-c1", Mission: "do it", Owns: []string{"x"}}),
		}
		watch.sendSubAgents()
		if got := sent[0].Children[0].Total; got != one.total {
			t.Fatalf("a bar under a cap of %d draws %d steps, want %d", one.maxSteps, got, one.total)
		}
	}
}

func gateRowFixture(t *testing.T) (string, ledger.Row) {
	t.Helper()
	dir, err := sys.LogDir()
	if err != nil {
		t.Fatalf("sys.LogDir: %v", err)
	}
	row, err := ledger.NewWriter(dir).Append(ledger.Row{Point: "tool_gate"})
	if err != nil {
		t.Fatalf("writing the row fixture: %v", err)
	}
	return dir, row
}

func TestAwaitPersonWritesTheAnswerOntoTheRowAsAnOutcome(t *testing.T) {
	dir, row := gateRowFixture(t)
	answers := make(chan tui.Answer, 1)
	answers <- tui.AllowedOnce
	person := awaitPerson(func(tui.Event) {}, answers, map[string]bool{})

	answer, err := person(context.Background(), turn.GateRequest{Tool: "write"}, turn.GateDecision{ID: row.ID})
	if err != nil || answer != turn.PersonAllowedOnce {
		t.Fatalf("person returned %v, %v", answer, err)
	}

	read, ok, err := ledger.NewReader(dir).ByID(row.ID)
	if err != nil || !ok {
		t.Fatalf("reading the row back: ok=%v err=%v", ok, err)
	}
	if read.Outcome == nil || read.Outcome.Kind != turn.OutcomeKindGateAnswer || read.Outcome.Detail != "allow" {
		t.Fatalf("the row's outcome is %+v, want kind %q detail allow", read.Outcome, turn.OutcomeKindGateAnswer)
	}
}

func TestAwaitPersonUnderAnAlreadyGrantedRuleWritesNoOutcome(t *testing.T) {
	dir, row := gateRowFixture(t)
	request := turn.GateRequest{Tool: "write", Args: json.RawMessage(`{"path":"a.txt"}`)}
	granted := map[string]bool{askedPlace(request): true}
	person := awaitPerson(func(tui.Event) {}, make(chan tui.Answer), granted)

	answer, err := person(context.Background(), request, turn.GateDecision{ID: row.ID})
	if err != nil || answer != turn.PersonAlwaysHere {
		t.Fatalf("person returned %v, %v", answer, err)
	}

	read, ok, err := ledger.NewReader(dir).ByID(row.ID)
	if err != nil || !ok {
		t.Fatalf("reading the row back: ok=%v err=%v", ok, err)
	}
	if read.Outcome != nil {
		t.Fatalf("a decision nobody was asked about must stay unlabelled, got %+v", read.Outcome)
	}
}

func TestTheAppsBashToolResolvesThroughTheSameShellSettingAsTofuRun(t *testing.T) {
	dir := scratchProject(t)
	bogus := filepath.Join(dir, "no-such-shell.exe")
	var out, errOut bytes.Buffer
	if code := settingsVerb([]string{"set", "shell", bogus}, &out, &errOut); code != exitOK {
		t.Fatalf("settings set shell %s exited %d: %s", bogus, code, errOut.String())
	}

	shellOverride, unreadable := appTextSetting(dir, settingspkg.Shell)
	if unreadable != "" {
		t.Fatalf("reading the shell setting: %s", unreadable)
	}
	if _, err := turn.ResolveRunShell(shellOverride); err == nil {
		t.Fatal("tofu run's own resolution did not fail on a shell setting that does not exist, so this test proves nothing")
	}

	driver := driveApp(t)
	capture := &toolNamesCapture{}
	stubbedTurn(dir, capture)(t.Context(), onTheSubscription, "say hello", driver.emit)

	failures := driver.of(tui.EventFailure)
	if len(failures) != 1 {
		t.Fatalf("the app resolves its bash tool on its own path rather than the setting tofu run reads, and the turn ran anyway: failures %+v", failures)
	}
	if !strings.Contains(failures[0].Text, bogus) {
		t.Fatalf("the failure %q does not name the shell setting %q that tofu run's own resolution rejects", failures[0].Text, bogus)
	}
}

func TestTheAppResolvesTheShellOnceAndSharesItWithTheEnvironmentBlock(t *testing.T) {
	source, err := os.ReadFile("app.go")
	if err != nil {
		t.Fatal(err)
	}
	start := strings.Index(string(source), "func (s *appSession) run(")
	if start < 0 {
		t.Fatal("app.go no longer defines appSession.run, so this test cannot check its wiring")
	}
	body := string(source)[start:]
	if end := strings.Index(body, "\nfunc "); end >= 0 {
		body = body[:end]
	}
	if calls := strings.Count(body, "turn.ResolveRunShell("); calls != 1 {
		t.Fatalf("appSession.run calls turn.ResolveRunShell %d times, want 1: a second call resolves the shell again instead of sharing the first", calls)
	}
	if !strings.Contains(body, "opts.shell = shell") {
		t.Fatal("appSession.run resolves a shell but never carries it on opts, so buildRunToolsForRun and the environment block cannot share it")
	}
	if !strings.Contains(body, "buildRunToolsForRun(") {
		t.Fatal("appSession.run still builds its tools through a path that resolves its own shell instead of the one already carried on opts")
	}
}

func TestEveryVerbBuildsTheAppOptionsInOnePlace(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	var built []string
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range parsed.Decls {
			function, isFunction := decl.(*ast.FuncDecl)
			if !isFunction {
				continue
			}
			where := name + " " + function.Name.Name
			if where == "app.go appOptions" {
				continue
			}
			ast.Inspect(function, func(node ast.Node) bool {
				if selector, isSelector := node.(*ast.SelectorExpr); isSelector && selector.Sel.Name == "Options" {
					if pkg, isIdent := selector.X.(*ast.Ident); isIdent && pkg.Name == "tui" {
						built = append(built, where+" names tui.Options")
					}
				}
				assign, isAssign := node.(*ast.AssignStmt)
				if !isAssign || len(assign.Rhs) != 1 {
					return true
				}
				if call, isCall := assign.Rhs[0].(*ast.CallExpr); isCall {
					if callee, isIdent := call.Fun.(*ast.Ident); isIdent && callee.Name == "appOptions" {
						built = append(built, where+" holds what appOptions returned instead of passing it on")
					}
				}
				return true
			})
		}
	}
	if len(built) > 0 {
		t.Fatalf("only appOptions may build or hold the app options, or the driven verb and the real verb drift apart: %s", strings.Join(built, "; "))
	}
}

func TestTheEndOfASessionNamesEachProcessItStops(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a real process")
	}
	registry := shell.OpenAt(filepath.Join(t.TempDir(), "shells"))
	if _, err := registry.Start(t.TempDir(), "dev-server", "sleep 10", ""); err != nil {
		t.Fatalf("starting a background process: %v", err)
	}
	t.Cleanup(func() { _ = registry.Kill("dev-server") })

	line := sessionEndLine(registry, nil)

	if !strings.Contains(line, "dev-server") || !strings.Contains(line, "sleep 10") {
		t.Fatalf("the line printed on the way out is %q, and it never names the process that stops with the session", line)
	}
}

func TestTheEndOfASessionSaysNothingExtraWhenNothingWasRunning(t *testing.T) {
	registry := shell.OpenAt(filepath.Join(t.TempDir(), "shells"))
	if line := sessionEndLine(registry, nil); line != sessionEnded {
		t.Fatalf("with nothing running the line is %q, want %q", line, sessionEnded)
	}
	if line := sessionEndLine(nil, errors.New("no state directory")); line != sessionEnded {
		t.Fatalf("with no registry the line is %q, want %q", line, sessionEnded)
	}
}

type modelStoppingTheTurnWhileTheChildIsAnswering struct {
	stop    context.CancelFunc
	spawn   llm.ToolCall
	spawned bool
}

func (m *modelStoppingTheTurnWhileTheChildIsAnswering) Ask(context.Context, llm.Request) (llm.Decision, error) {
	if !m.spawned {
		m.spawned = true
		return llm.Decision{Build: "stub-model", Outcome: llm.OutcomeToolCalls, ToolCalls: []llm.ToolCall{m.spawn}}, nil
	}
	m.stop()
	return llm.Decision{Build: "stub-model", Outcome: llm.OutcomeToolCalls, ToolCalls: []llm.ToolCall{writeNote("call-2")}}, nil
}

func childStoppedWhileItAnswered(t *testing.T) *appDriver {
	t.Helper()
	dir := scratchProject(t)
	driver := driveApp(t)
	ctx, stop := context.WithCancel(t.Context())
	defer stop()
	model := &modelStoppingTheTurnWhileTheChildIsAnswering{stop: stop,
		spawn: llm.ToolCall{ID: "call-1", Name: "spawn", Arguments: json.RawMessage(`{"task":"write note.txt","owns":["note.txt"]}`)}}

	stubbedTurn(dir, model)(ctx, onTheSubscription, "hand the note to a child", driver.emit)
	return driver
}

func TestAParkedChildsReportReachesThePanelWhenTheTurnIsStoppedAndNeverAsksAgain(t *testing.T) {
	driver := childStoppedWhileItAnswered(t)

	done := driver.of(tui.EventDone)
	if len(done) != 1 || len(done[0].Children) == 0 {
		t.Fatalf("the stopped turn closed with %+v, want one close carrying the child", done)
	}
	last := done[0].Children[0]
	if last.State != roster.Parked || last.Report == "" {
		t.Fatalf("the panel was last told %+v, want a parked child carrying the report the roster holds", last)
	}
	screen := pickedFirstChild(t, driver)
	if !strings.Contains(screen, "act_on: the child was stopped") {
		t.Fatalf("the sub-agent view does not carry the parked report:\n%s", screen)
	}
	t.Log("\n" + screen)
}

func TestTheSpawnRowCarriesItsResultWhenTheTurnIsStoppedAndNeverAsksAgain(t *testing.T) {
	driver := childStoppedWhileItAnswered(t)

	var spawned tui.Event
	for _, call := range driver.of(tui.EventToolCall) {
		if call.Tool == "spawn" {
			spawned = call
		}
	}
	if spawned.ID == "" {
		t.Fatal("the stopped turn drew no spawn call")
	}
	for _, result := range driver.of(tui.EventToolResult) {
		if result.ID == spawned.ID && result.Text != "" {
			t.Log("the spawn row reads " + result.Text)
			return
		}
	}
	t.Fatalf("no result reached the spawn call %q, so its row stays at no result", spawned.ID)
}

type toolFixture struct {
	arguments  string
	notRunHere string
}

func TestEveryToolInTheRunRegistryRecordsACommandThatDoesNotRepeatItsName(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string]string{
		"main.go":  "package main\n\nfunc Greet() string { return \"hi\" }\n",
		"note.txt": "one\ntwo\nthree\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatalf("seeding %s: %v", name, err)
		}
	}
	page := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = writer.Write([]byte("<html><body><main><h1>Pools</h1><p>Size the pool from measured use.</p></main></body></html>"))
	}))
	t.Cleanup(page.Close)

	fixtures := map[string]toolFixture{
		"read":               {arguments: `{"path":"note.txt"}`},
		"write":              {arguments: `{"path":"fresh.txt","content":"a line\n"}`},
		"bash":               {arguments: `{"command":"echo ran"}`},
		"plan":               {arguments: `{"op":"set","items":[{"text":"walk the registry"}]}`},
		"project_report":     {arguments: `{}`},
		"glob":               {arguments: `{"pattern":"*.go"}`},
		"search":             {arguments: `{"pattern":"Greet"}`},
		"symbols":            {arguments: `{"name":"Greet"}`},
		"edit":               {arguments: `{"path":"note.txt","old_string":"two","new_string":"four"}`},
		"fetch":              {arguments: `{"url":` + strconv.Quote(page.URL) + `}`},
		"tofu_lint_comments": {arguments: `{}`},
		"tofu_rules_check":   {arguments: `{}`},
		"tofu_judge":         {arguments: `{"state":"the work is done","battery":"stop_check@1"}`},
		"tofu_why":           {arguments: `{"id":"dec-1"}`},
		"tofu_replay":        {arguments: `{"point":"tool_gate"}`},
		"github_pr_diff":     {notRunHere: "it shells out to gh against a real github repository"},
		"web_search":         {notRunHere: "it reaches a paid search provider over the network"},
		turn.ShellToolName:   {notRunHere: "it stops, restarts or reads a background shell by name, and this test starts none"},
	}

	built, err := buildTestRunTools(dir, toolSetFull)
	if err != nil {
		t.Fatalf("building the run tools: %v", err)
	}
	var notRun []string
	for _, tool := range built {
		name := tool.Name()
		fixture, known := fixtures[name]
		if !known {
			t.Fatalf("%s is in the registry and has no fixture here, so nothing proves its command leaves the tool name out: add one", name)
		}
		if fixture.notRunHere != "" {
			notRun = append(notRun, name+", because "+fixture.notRunHere)
			continue
		}
		result, runErr := tool.Run(context.Background(), json.RawMessage(fixture.arguments))
		if runErr != nil {
			t.Errorf("%s did not run against its fixture: %v", name, runErr)
			continue
		}
		t.Logf("%s recorded %q", name, result.Command)
		if result.Command == "" {
			t.Errorf("%s recorded an empty command, so the panel and the ledger have nothing of what it did", name)
		}
		if result.Command == name || strings.HasPrefix(result.Command, name+" ") {
			t.Errorf("%s recorded %q, and the tool name belongs in Tool beside it rather than twice", name, result.Command)
		}
	}
	t.Logf("%d tools in the registry, %d not run here: %s", len(built), len(notRun), strings.Join(notRun, "; "))
}

func emptyHome(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)
	t.Setenv("OPENROUTER_KEY", "")
}
