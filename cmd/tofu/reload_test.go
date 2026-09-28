package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"tofu/interface/tui"
)

func reloadLine(t *testing.T, said, part string) string {
	t.Helper()
	for _, line := range strings.Split(said, "\n") {
		if strings.HasPrefix(line, part+" ") {
			return line
		}
	}
	t.Fatalf("tofu reload printed no %s line:\n%s", part, said)
	return ""
}

func TestReloadPrintsWhatWasAddedAndRemovedSinceTheLastRun(t *testing.T) {
	project := chdirTemp(t)
	var out, errOut bytes.Buffer
	if code := run([]string{"agents", "add", "reviewer", "--description", "reviews a diff", "--model", "claude-sub/claude-opus-5"}, strings.NewReader(""), &out, &errOut); code != exitOK {
		t.Fatalf("agents add exited %d: %s", code, errOut.String())
	}
	out.Reset()
	if code := reloadVerb(&out, &errOut); code != exitOK {
		t.Fatalf("the first reload exited %d: %s", code, errOut.String())
	}
	first := out.String()

	writeFile(t, project, ".tofu/skills/deploy/SKILL.md", "---\nname: deploy\ndescription: ship the build\n---\nsteps\n")
	writeFile(t, project, ".tofu/models/anthropic/claude-sonnet-9.yaml", "subscription: claude-sub\nuse: allowed\n")
	if err := os.Remove(filepath.Join(project, ".tofu", "agents", "reviewer.md")); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if code := reloadVerb(&out, &errOut); code != exitOK {
		t.Fatalf("the second reload exited %d: %s", code, errOut.String())
	}
	second := out.String()
	t.Log(second)

	for part, want := range map[string]string{"skills": "+1 added: deploy", "models": "+1 added: claude-sub/claude-sonnet-9", "sub-agents": "-1 removed: reviewer"} {
		if line := reloadLine(t, second, part); !strings.Contains(line, want) {
			t.Errorf("the %s line is %q, want %q\nfirst run:\n%s", part, line, want, first)
		}
	}
	if line := reloadLine(t, second, "rules"); strings.ContainsAny(line, "+-") {
		t.Errorf("rules did not change and the line says %q", line)
	}

	out.Reset()
	if code := reloadVerb(&out, &errOut); code != exitOK || strings.Contains(out.String(), " added: ") || strings.Contains(out.String(), " removed: ") {
		t.Errorf("a third reload over the same disk exited %d and still reports a change:\n%s", code, out.String())
	}
}

func TestReloadInTheAppPicksUpAKeymapEdit(t *testing.T) {
	project := chdirTemp(t)
	keys := filepath.Join(t.TempDir(), "keybindings.json")
	writeFile(t, filepath.Dir(keys), "keybindings.json", `{"version":1,"bindings":{"Search":"ctrl+k"}}`)
	app := tui.New(tui.Options{Repo: "scratch", Root: project, Keymap: keys, Reload: appReload(project)})
	app.Init()
	app.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	const searchHint = "Screens, agents and events"
	ctrlO := tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl}

	writeFile(t, filepath.Dir(keys), "keybindings.json", `{"version":1,"bindings":{"Search":"ctrl+o"}}`)
	app.Update(ctrlO)
	if strings.Contains(app.View().Content, searchHint) {
		t.Fatal("the new binding fired before /reload, so this test proves nothing")
	}

	for _, letter := range "/reload" {
		app.Update(tea.KeyPressMsg{Code: letter, Text: string(letter)})
	}
	app.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !strings.Contains(app.View().Content, "usable models") {
		t.Errorf("the /reload note does not show the inventory\n%s", app.View().Content)
	}
	app.Update(ctrlO)
	if !strings.Contains(app.View().Content, searchHint) {
		t.Fatalf("ctrl+o does not open search after /reload\n%s", app.View().Content)
	}
	app.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	app.Update(tea.KeyPressMsg{Code: 'k', Mod: tea.ModCtrl})
	if strings.Contains(app.View().Content, searchHint) {
		t.Error("the old binding ctrl+k still opens search after /reload")
	}
}
