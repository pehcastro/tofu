package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"boji/interface/tui"
	"boji/internal/judge/jev"
	"boji/internal/llm"
)

func TestVerbTableIsUnchanged(t *testing.T) {
	for _, verb := range []struct {
		args []string
		code int
		says string
	}{
		{[]string{"version"}, exitOK, "version: "},
		{[]string{"doctor", "--nope"}, exitUsage, "boji doctor: unknown argument"},
		{[]string{"login", "--nope"}, exitUsage, "boji login: "},
		{[]string{"usage", "--nope"}, exitUsage, "usage: boji usage"},
		{[]string{"models", "--nope"}, exitUsage, "boji models: unknown flag"},
		{[]string{"why", "--nope"}, exitUsage, "boji why: "},
		{[]string{"run", "--nope"}, exitUsage, "boji run: "},
		{[]string{"judge", "--nope"}, exitUsage, "boji judge: "},
		{[]string{"check", "--nope"}, exitUsage, "boji check: "},
		{[]string{"label", "--nope"}, exitUsage, "boji label: "},
		{[]string{"replay", "--nope"}, exitUsage, "boji replay: "},
		{[]string{"catalog", "--nope"}, exitUsage, "boji catalog: usage"},
		{[]string{"lint", "--nope"}, exitUsage, "boji lint: usage"},
		{[]string{"rules", "--nope"}, exitUsage, "boji rules: "},
		{[]string{"nosuchverb"}, exitUsage, "unknown verb"},
		{[]string{"--help"}, exitOK, "boji is a coding agent harness"},
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

func TestBareBojiWithoutATerminalSaysSo(t *testing.T) {
	var out, errOut bytes.Buffer
	code := run(nil, strings.NewReader(""), &out, &errOut)
	if code != exitUsage {
		t.Errorf("exit %d, want %d", code, exitUsage)
	}
	if !strings.Contains(errOut.String(), noTerminal) {
		t.Errorf("stderr %q, want the terminal requirement", errOut.String())
	}
	if strings.Contains(out.String(), "Verbs:") {
		t.Error("bare boji still prints the verb list instead of starting the app")
	}
}

func TestMissingCredentialAsksForTheLogin(t *testing.T) {
	t.Setenv("USERPROFILE", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	required := appRequirements()
	if len(required) != 1 {
		t.Fatalf("requirements %v, want the one about the credential", required)
	}
	if required[0].What != noCredential || required[0].Fix != loginFix {
		t.Fatalf("requirement %+v", required[0])
	}
	app := tui.New(tui.Options{Repo: "scratch", Model: "claude-opus-5", Requirements: required})
	app.Init()
	app.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	screen := app.View().Content
	if strings.Contains(screen, "sqlite") || strings.Contains(screen, "*errors") {
		t.Errorf("the setup screen leaks a go error:\n%s", screen)
	}
	t.Log("\n" + screen)
}

func TestLiveAppRunsATaskAndShowsTheToolCalls(t *testing.T) {
	if os.Getenv("BOJI_LIVE") != "1" {
		t.Skip("set BOJI_LIVE=1 to spend the subscription quota")
	}
	if key, err := jev.Key("../../.env"); err == nil {
		t.Setenv("OPENROUTER_KEY", key)
	}
	dir := t.TempDir()
	app := tui.New(tui.Options{
		Repo:   filepath.Base(dir),
		Branch: "scratch",
		Model:  "claude-opus-5",
		Quota:  appQuota,
	})
	app.Init()
	app.Update(tea.WindowSizeMsg{Width: 100, Height: 28})
	app.Update(appQuota())

	task := "create notes.txt holding the single word ready, then read it back and say what it holds"
	app.Update(tui.Event{Kind: tui.EventText, Text: task})
	appTurn(dir)(t.Context(), task, func(event tui.Event) { app.Update(event) })

	t.Log("\n" + app.View().Content)
	body, err := os.ReadFile(filepath.Join(dir, "notes.txt"))
	if err != nil || !strings.Contains(string(body), "ready") {
		t.Fatalf("the turn did not write the file: %v %q", err, body)
	}
}

func TestCallSummaryAndResultLine(t *testing.T) {
	if got := callSummary(llm.ToolCall{Arguments: []byte(`{"command":"go test ./...","timeout":30}`)}); got != "go test ./..." {
		t.Errorf("callSummary %q", got)
	}
	if got := callSummary(llm.ToolCall{Arguments: []byte(`{"path":"internal/turn/loop.go"}`)}); got != "internal/turn/loop.go" {
		t.Errorf("callSummary %q", got)
	}
	if got := resultLine("ok  boji/internal/turn\n"); got != "ok  boji/internal/turn" {
		t.Errorf("resultLine %q", got)
	}
	if got := resultLine("ok  boji/internal/turn\nmore"); got != "ok  boji/internal/turn …  27 bytes" {
		t.Errorf("resultLine %q", got)
	}
}
