package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"tofu/internal/subagent"
)

func TestAnUnsetTierStaysOutOfTheChatAndARefusedDefinitionStillShows(t *testing.T) {
	home, project := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	agents := filepath.Join(project, ".claude", "agents")
	if err := os.MkdirAll(agents, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agents, "refused.md"), []byte("Do nothing, and carry no front matter.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	unset := slices.ContainsFunc(scanSubAgents(project, nil).Definitions, func(definition subagent.Definition) bool {
		return slices.ContainsFunc(definition.Notices, func(notice string) bool { return strings.Contains(notice, "is not set, so") })
	})
	if !unset {
		t.Fatal("no definition carries an unset tier notice in a fresh home, so the test proves nothing")
	}
	var chat []string
	if _, err := composeRun(runOpts{dir: project}, nil, runtime{notify: func(notice string) { chat = append(chat, notice) }, open: openAppWire}); err != nil {
		t.Fatal(err)
	}
	said := strings.Join(chat, "\n")
	if strings.Contains(said, "is not set, so") {
		t.Errorf("the chat carries the unset tier notice:\n%s", said)
	}
	if !strings.Contains(said, "refused.md is not offered") {
		t.Errorf("the chat does not say the refused definition is not offered:\n%s", said)
	}
}
