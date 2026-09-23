package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	settingspkg "tofu/internal/settings"
)

func isolatedHomeAndProject(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Chdir(t.TempDir())
	return home
}

func TestSettingsSetPersistsAndGetReadsItBack(t *testing.T) {
	isolatedHomeAndProject(t)
	var out, errOut bytes.Buffer
	if code := settingsVerb([]string{"set", "--scope", "project", "decisionCap", "5"}, &out, &errOut); code != exitOK {
		t.Fatalf("settings set exited %d: %s", code, errOut.String())
	}
	out.Reset()
	if code := settingsVerb([]string{"get", "decisionCap"}, &out, &errOut); code != exitOK {
		t.Fatalf("settings get exited %d: %s", code, errOut.String())
	}
	if got := out.String(); got != "5\n" {
		t.Fatalf("decisionCap after set = %q, want \"5\\n\"", got)
	}
}

func TestDecisionCapDefaultsToNoCap(t *testing.T) {
	isolatedHomeAndProject(t)
	if got := appSetting(".", settingspkg.DecisionCap); got != 0 {
		t.Fatalf("the decision cap default = %d, want 0, meaning no cap", got)
	}
}

func TestTheProjectInstructionCapDefaultsToThirtyTwoKilobytes(t *testing.T) {
	isolatedHomeAndProject(t)
	if got := appSetting(".", settingspkg.ProjectInstructionsCap); got != 32*1024 {
		t.Fatalf("the project instruction cap default = %d, want %d", got, 32*1024)
	}
}

func TestAProjectInstructionCapSetOnDiskReachesThePrompt(t *testing.T) {
	isolatedHomeAndProject(t)
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	body := strings.Repeat("e", 9000)
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	if code := settingsVerb([]string{"set", "--scope", "project", settingspkg.ProjectInstructionsCap, "4096"}, &out, &errOut); code != exitOK {
		t.Fatalf("settings set exited %d: %s", code, errOut.String())
	}
	opts, err := parseRunArgs([]string{"--dir", dir, "a task"})
	if err != nil {
		t.Fatal(err)
	}
	environment, cutNotice := runEnvironment(opts)
	if len(environment) > 4096+500 {
		t.Fatalf("a cap of 4096 produced a %d byte environment block", len(environment))
	}
	for _, want := range []string{"cut at 4096 bytes", "this project's CLAUDE.md", settingspkg.ProjectInstructionsCap} {
		if !strings.Contains(cutNotice, want) {
			t.Fatalf("the notice a person reads does not say %q: %q", want, cutNotice)
		}
	}
}

func TestChatShowsToolsIsADeclaredSetting(t *testing.T) {
	isolatedHomeAndProject(t)
	var out, errOut bytes.Buffer
	if code := settingsVerb([]string{"set", "chatShowsTools", "true"}, &out, &errOut); code != exitOK {
		t.Fatalf("settings set exited %d: %s", code, errOut.String())
	}
	out.Reset()
	if code := settingsVerb([]string{"get", "chatShowsTools"}, &out, &errOut); code != exitOK {
		t.Fatalf("settings get exited %d: %s", code, errOut.String())
	}
	if got := out.String(); got != "true\n" {
		t.Fatalf("chatShowsTools after set = %q, want \"true\\n\"", got)
	}
}

func TestSettingsFileNeverWritesIntoTheHomeStateDirsCredentialFile(t *testing.T) {
	home := isolatedHomeAndProject(t)
	var out, errOut bytes.Buffer
	if code := settingsVerb([]string{"set", "decisionCap", "3"}, &out, &errOut); code != exitOK {
		t.Fatalf("settings set exited %d: %s", code, errOut.String())
	}
	global, _ := settingsPaths(".")
	if filepath.Base(global) == "agent.db" || filepath.Base(global) == ".env" {
		t.Fatalf("the settings file must not share a name with a credential file, got %s", global)
	}
	if _, err := os.Stat(filepath.Join(home, ".tofu", "agent.db")); err == nil {
		t.Fatal("writing a setting must not create or touch a credential store file")
	}
}
