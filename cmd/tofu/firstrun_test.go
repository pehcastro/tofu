package main

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"tofu/interface/tui"
	"tofu/interface/tui/session"
	"tofu/internal/llm"
	"tofu/internal/llm/cred"
	"tofu/internal/sys"
)

func containsAPlaceholder(text string) bool {
	for _, example := range session.PlaceholderExamples {
		if strings.Contains(text, example) {
			return true
		}
	}
	return false
}

func firstRunApp(t *testing.T, width, height int) *tui.App {
	t.Helper()
	app := tui.New(tui.Options{
		Repo:         "scratch",
		Branch:       "develop",
		Requirements: appRequirements(),
		Recheck:      appRequirements,
		Providers:    appProviders(),
	})
	app.Init()
	app.Update(tea.WindowSizeMsg{Width: width, Height: height})
	return app
}

func plainFrame(app *tui.App) string { return ansi.Strip(app.View().Content) }

func TestAFirstRunWithNothingStoredDrawsTheSetupAndNotAnError(t *testing.T) {
	scratchProject(t)
	narrow, wide := plainFrame(firstRunApp(t, 80, 24)), plainFrame(firstRunApp(t, 120, 36))
	for _, want := range []string{noCredential, noGateKey, "tofu login anthropic", "tofu login openrouter"} {
		if !strings.Contains(narrow, want) {
			t.Errorf("the first frame at 80 columns does not say %q\n%s", want, narrow)
		}
	}
	for _, want := range []string{loginFix, gateKeyFix} {
		if !strings.Contains(wide, want) {
			t.Errorf("the first frame at 120 columns does not say %q\n%s", want, wide)
		}
	}
	for _, absent := range []string{"panic", "sqlite", "no such file"} {
		if strings.Contains(narrow, absent) {
			t.Errorf("the first frame carries %q instead of the setup\n%s", absent, narrow)
		}
	}
	if containsAPlaceholder(narrow) {
		t.Errorf("the first frame carries the composer placeholder instead of the setup\n%s", narrow)
	}
	t.Log("\n" + narrow)
	t.Log("\n" + wide)
}

func TestACredentialWithoutAKeyLeavesOnlyTheKeyStep(t *testing.T) {
	scratchProject(t)
	storeCredential(t, cred.Anthropic)
	frame := plainFrame(firstRunApp(t, 80, 24))
	if !strings.Contains(frame, noGateKey) {
		t.Errorf("the key step is not drawn\n%s", frame)
	}
	if strings.Contains(frame, noCredential) {
		t.Errorf("the credential step is drawn with a credential stored\n%s", frame)
	}
	if strings.Contains(frame, "2. ") {
		t.Errorf("the setup numbers a second step with only one thing missing\n%s", frame)
	}
	t.Log("\n" + frame)
}

func TestWithBothStoredNoSetupIsDrawnAndTheComposerHasFocus(t *testing.T) {
	scratchProject(t)
	storeCredential(t, cred.Anthropic)
	storeGateKey(t, gateKeyForTests)
	app := firstRunApp(t, 80, 24)
	frame := plainFrame(app)
	for _, absent := range []string{noCredential, noGateKey, "run the fix"} {
		if strings.Contains(frame, absent) {
			t.Errorf("setup is still drawn with everything stored: %q\n%s", absent, frame)
		}
	}
	if !containsAPlaceholder(frame) {
		t.Errorf("the composer is not drawn\n%s", frame)
	}
	if app.View().Cursor == nil {
		t.Errorf("the composer has no cursor, so it never took focus\n%s", frame)
	}
	t.Log("\n" + frame)
}

func drive(t *testing.T, app *tui.App, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		return
	}
	switch msg := cmd().(type) {
	case nil:
	case tea.BatchMsg:
		for _, each := range msg {
			drive(t, app, each)
		}
	default:
		_, next := app.Update(msg)
		drive(t, app, next)
	}
}

func TestALoginRunInAnotherTerminalMovesTheAppOnWithoutARestart(t *testing.T) {
	scratchProject(t)
	app := tui.New(tui.Options{Repo: "scratch", Requirements: appRequirements(), Recheck: appRequirements})
	start := app.Init()
	app.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	if !strings.Contains(plainFrame(app), noCredential) {
		t.Fatalf("the app did not start on the setup screen\n%s", plainFrame(app))
	}

	storeCredential(t, cred.Anthropic)
	storeGateKey(t, gateKeyForTests)
	began := time.Now()
	drive(t, app, start)

	frame := plainFrame(app)
	t.Logf("the app noticed the logins %s after they were stored, with no restart", time.Since(began).Round(time.Millisecond))
	for _, absent := range []string{noCredential, noGateKey} {
		if strings.Contains(frame, absent) {
			t.Errorf("the app still asks for %q after the login ran elsewhere\n%s", absent, frame)
		}
	}
	if !containsAPlaceholder(frame) {
		t.Errorf("the app did not reach the composer\n%s", frame)
	}
	t.Log("\n" + frame)
}

func TestTheAppAndDoctorCallTheSameStateReady(t *testing.T) {
	scratchProject(t)
	for _, step := range []struct {
		what  string
		store func()
	}{
		{"nothing stored", func() {}},
		{"the key alone", func() { storeGateKey(t, gateKeyForTests) }},
		{"the credential too", func() { storeCredential(t, cred.Anthropic) }},
	} {
		step.store()
		required := appRequirements()
		report := doctorState(time.Now())
		appReady, doctorSaysReady := len(required) == 0, report.Verdict == doctorReady
		t.Logf("%s: the app lists %d blockers, doctor says %s", step.what, len(required), report.Verdict)
		if appReady != doctorSaysReady {
			t.Errorf("with %s the app says ready=%v and doctor says %s", step.what, appReady, report.Verdict)
		}
		if len(required) != len(report.Blockers) {
			t.Fatalf("with %s the app lists %d blockers and doctor lists %d", step.what, len(required), len(report.Blockers))
		}
		for index, blocker := range report.Blockers {
			if blocker.What != required[index].What {
				t.Errorf("with %s the app says %q and doctor says %q", step.what, required[index].What, blocker.What)
			}
		}
	}
}

func filesUnder(t *testing.T, dir string) map[string]string {
	t.Helper()
	held := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		held[path] = string(body)
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return held
}

func TestTheKeyIsNeverInAFrameALogOrARecordedSession(t *testing.T) {
	dir := scratchProject(t)
	const key = "sk-or-v1-0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcd"
	storeGateKey(t, key)
	t.Setenv("OPENROUTER_KEY", key)
	storeCredential(t, cred.Anthropic)
	var authorization string
	jevStub(t, http.StatusOK, untrustedDenyReply, &authorization)

	model := &queuedModel{decisions: []llm.Decision{
		{Build: "stub-model", Outcome: llm.OutcomeToolCalls, ToolCalls: []llm.ToolCall{
			{ID: "call-1", Name: "write", Arguments: json.RawMessage(`{"path":"note.txt","content":"a note"}`)},
		}},
		{Build: "stub-model", Outcome: llm.OutcomeMessage, Content: "wrote the note"},
	}}
	app := firstRunApp(t, 100, 30)
	var turned eventLog
	stubbedTurn(dir, model)(t.Context(), wireSubscription, "write a note", turned.add)
	for _, event := range turned.all() {
		app.Update(event)
	}

	if !strings.Contains(authorization, key) {
		t.Fatalf("the gate never reached jev with the key, so nothing was at risk of leaking: %q", authorization)
	}

	var frames string
	for _, view := range []tea.KeyPressMsg{
		{Code: '1', Mod: tea.ModAlt}, {Code: '2', Mod: tea.ModAlt},
		{Code: '3', Mod: tea.ModAlt}, {Code: '6', Mod: tea.ModAlt},
	} {
		app.Update(view)
		frames += plainFrame(app)
	}

	state, err := sys.ProjectStateDir()
	if err != nil {
		t.Fatal(err)
	}
	logDir, err := sys.LogDir()
	if err != nil {
		t.Fatal(err)
	}
	logged, recorded := filesUnder(t, logDir), filesUnder(t, filepath.Join(state, "sessions"))
	if len(logged) == 0 {
		t.Fatalf("no decision reached the ledger under %s, so the log proves nothing", logDir)
	}
	if len(recorded) == 0 {
		t.Fatalf("no session was recorded under %s, so the record proves nothing", state)
	}
	t.Logf("greping %d ledger files and %d session files, and %d bytes of frames",
		len(logged), len(recorded), len(frames))

	assertKeyAbsent(t, "the frames", frames, key)
	for path, body := range logged {
		assertKeyAbsent(t, "the ledger file "+path, body, key)
	}
	for path, body := range recorded {
		assertKeyAbsent(t, "the session file "+path, body, key)
	}
}

const keyRunLength = 8

func assertKeyAbsent(t *testing.T, where, body, key string) {
	t.Helper()
	for at := 0; at+keyRunLength <= len(key); at++ {
		if strings.Contains(body, key[at:at+keyRunLength]) {
			t.Errorf("%s holds %d characters of the key from position %d", where, keyRunLength, at)
			return
		}
	}
}
