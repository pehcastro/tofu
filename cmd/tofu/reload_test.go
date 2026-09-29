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

const (
	reloadAfterASkillAModelAndARemovedSubAgent = `Reload · ~/proj                                            ✓ 3 added · 1 removed

skills
  + deploy

sub-agents
  - reviewer

models
  + claude-sub/claude-sonnet-9

usable models
  + claude-sub/claude-sonnet-9
`

	reloadOverTheSameDisk = `Reload · ~/proj                                                ✓ nothing changed
`
)

func projectInHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	project := filepath.Join(home, "proj")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(project)
	return project
}

func TestReloadPrintsOnlyThePartsThatChangedSinceTheLastRun(t *testing.T) {
	project := projectInHome(t)
	var out, errOut bytes.Buffer
	if code := run([]string{"agents", "add", "reviewer", "--description", "reviews a diff", "--model", "claude-sub/claude-opus-5"}, strings.NewReader(""), &out, &errOut); code != exitOK {
		t.Fatalf("agents add exited %d: %s", code, errOut.String())
	}
	out.Reset()
	if code := reloadVerb(nil, &out, &errOut); code != exitOK {
		t.Fatalf("the first reload exited %d: %s", code, errOut.String())
	}

	writeFile(t, project, ".tofu/skills/deploy/SKILL.md", "---\nname: deploy\ndescription: ship the build\n---\nsteps\n")
	writeFile(t, project, ".tofu/models/anthropic/claude-sonnet-9.yaml", "subscription: claude-sub\nuse: allowed\n")
	if err := os.Remove(filepath.Join(project, ".tofu", "agents", "reviewer.md")); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if code := reloadVerb(nil, &out, &errOut); code != exitOK {
		t.Fatalf("the second reload exited %d: %s", code, errOut.String())
	}
	sameText(t, "a reload after a new skill, a new model and a removed sub-agent", out.String(), reloadAfterASkillAModelAndARemovedSubAgent)

	out.Reset()
	if code := reloadVerb(nil, &out, &errOut); code != exitOK {
		t.Fatalf("the third reload exited %d: %s", code, errOut.String())
	}
	sameText(t, "a reload over the same disk", out.String(), reloadOverTheSameDisk)
}

func TestReloadJSONIsOneEnvelopeWithEveryPartAndItsChanges(t *testing.T) {
	project := projectInHome(t)
	var out, errOut bytes.Buffer
	if code := reloadVerb([]string{jsonFlag}, &out, &errOut); code != exitOK {
		t.Fatalf("the first reload exited %d: %s", code, errOut.String())
	}
	writeFile(t, project, ".tofu/skills/deploy/SKILL.md", "---\nname: deploy\ndescription: ship the build\n---\nsteps\n")
	out.Reset()
	if code := reloadVerb([]string{jsonFlag}, &out, &errOut); code != exitOK {
		t.Fatalf("the second reload exited %d: %s", code, errOut.String())
	}
	var envelope envelopeOf[reloadDiff]
	oneEnvelope(t, out.String(), &envelope)
	if envelope.Verb != "reload" || !envelope.OK || envelope.Data.First || envelope.Data.Project != project {
		t.Fatalf("the envelope is not an ok second reload of %s\n%s", project, out.String())
	}
	for _, part := range envelope.Data.Parts {
		if part.Name == "skills" && (part.Count != 1 || len(part.Added) != 1 || part.Added[0] != "deploy") {
			t.Errorf("the skills part is %+v, want one skill and deploy added", part)
		}
	}
	if len(envelope.Data.Parts) < 10 {
		t.Errorf("the JSON carries %d parts, want every part, changed or not", len(envelope.Data.Parts))
	}
	if code := reloadVerb([]string{"--nope"}, &out, &errOut); code != exitUsage {
		t.Errorf("an unknown flag exited %d rather than %d", code, exitUsage)
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
	if !strings.Contains(app.View().Content, "reloaded, the first reload here") {
		t.Errorf("the /reload note is not the one short line\n%s", app.View().Content)
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
