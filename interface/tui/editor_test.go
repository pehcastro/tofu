package tui

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestEditorCommandReadsVisualThenEditorThenThePlatform(t *testing.T) {
	platform := []string{"vi"}
	if runtime.GOOS == "windows" {
		platform = []string{"notepad"}
	}
	for _, row := range []struct {
		name   string
		env    map[string]string
		want   []string
		refuse bool
	}{
		{"visual wins", map[string]string{"VISUAL": "nvim", "EDITOR": "nano"}, []string{"nvim"}, false},
		{"an empty visual falls through", map[string]string{"VISUAL": "  ", "EDITOR": "nano"}, []string{"nano"}, false},
		{"neither set", map[string]string{}, platform, false},
		{"arguments split", map[string]string{"EDITOR": "code --wait  -n"}, []string{"code", "--wait", "-n"}, false},
		{"a quoted windows path keeps its backslashes", map[string]string{"EDITOR": `"C:\Program Files\Notepad++\notepad++.exe" -multiInst`}, []string{`C:\Program Files\Notepad++\notepad++.exe`, "-multiInst"}, false},
		{"an unmatched quote runs nothing", map[string]string{"EDITOR": `"C:\Program Files\code.exe --wait`}, nil, true},
	} {
		t.Run(row.name, func(t *testing.T) {
			got, err := editorCommand(func(name string) string { return row.env[name] })
			if row.refuse != (err != nil) || !slices.Equal(got, row.want) {
				t.Fatalf("got %q, %v; want %q, refused %v", got, err, row.want, row.refuse)
			}
		})
	}
}

func TestEditedTextUndoesWhatEditorsAdd(t *testing.T) {
	for saved, want := range map[string]string{
		"hello\nworld\n":        "hello\nworld",
		"hello\r\nworld\r\n":    "hello\nworld",
		"\uFEFFhello\r\n":       "hello",
		"hello\n\n":             "hello\n",
		"":                      "",
		"no newline at the end": "no newline at the end",
	} {
		if got := editedText([]byte(saved)); got != want {
			t.Errorf("editedText(%q) = %q, want %q", saved, got, want)
		}
	}
}

func editorScript(t *testing.T, exit int) (argv []string, seen string) {
	t.Helper()
	dir := t.TempDir()
	seen = filepath.Join(dir, "seen")
	if runtime.GOOS == "windows" {
		script := filepath.Join(dir, "append.cmd")
		body := "@echo off\r\necho %~1>\"" + seen + "\"\r\necho world>>\"%~1\"\r\nexit /b " + strconv.Itoa(exit) + "\r\n"
		if err := os.WriteFile(script, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return []string{script}, seen
	}
	script := filepath.Join(dir, "append.sh")
	body := "#!/bin/sh\necho \"$1\" > '" + seen + "'\necho world >> \"$1\"\nexit " + strconv.Itoa(exit) + "\n"
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	return []string{script}, seen
}

func TestEditorRunReadsBackTheFileAndRemovesIt(t *testing.T) {
	for _, exit := range []int{0, 1} {
		argv, seen := editorScript(t, exit)
		run := &editorRun{argv: argv, draft: "hello"}
		err := run.Run()
		if exit == 0 && (err != nil || run.edited != "hello\nworld") {
			t.Fatalf("exit 0: edited %q, error %v", run.edited, err)
		}
		if exit == 1 && (err == nil || run.edited != "") {
			t.Fatalf("exit 1: edited %q, error %v", run.edited, err)
		}
		path, err := os.ReadFile(seen)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(strings.TrimSpace(string(path))); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("exit %d left the prompt file behind: %v", exit, err)
		}
	}
}

func TestEditorResultReplacesTheComposerOrSaysItFailed(t *testing.T) {
	app := sessionApp(t, 100, 30)
	typeText(app, "hello")
	app.Update(editedMsg{err: errors.New("exit status 1")})
	if app.view.Draft() != "hello" || !strings.Contains(ansi.Strip(app.view.View()), "editor failed") {
		t.Fatalf("a failed editor changed the composer to %q or said nothing:\n%s", app.view.Draft(), ansi.Strip(app.view.View()))
	}
	app.Update(editedMsg{text: "hello\nworld"})
	if app.view.Draft() != "hello\nworld" {
		t.Fatalf("composer after a clean edit: %q", app.view.Draft())
	}
}

func TestCtrlGOpensTheEditorFromAnyTab(t *testing.T) {
	app := sessionApp(t, 100, 30)
	app.current = screenSettings
	if cmd := app.update(tea.KeyPressMsg{Code: 'g', Mod: tea.ModCtrl}); cmd == nil || app.current != screenChat {
		t.Fatalf("ctrl+g returned %v and left the app on screen %d", cmd, app.current)
	}
}
