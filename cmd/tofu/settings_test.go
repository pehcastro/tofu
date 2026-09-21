package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
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
	if got := appDecisionCap("."); got != 0 {
		t.Fatalf("appDecisionCap default = %d, want 0, meaning no cap", got)
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
