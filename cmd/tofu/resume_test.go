package main

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"time"

	"tofu/internal/llm"
)

func continuedScreen(t *testing.T, waitFor string) string {
	t.Helper()
	script := written(t, t.TempDir(), "continue.drive", "wait "+waitFor+"\nscreen\n")
	var out, errOut bytes.Buffer
	if code := driveVerb([]string{script, "--home", os.Getenv("HOME"), "--continue", "--plain", "--timeout", "30s"}, strings.NewReader(""), &out, &errOut); code != exitOK {
		t.Fatalf("tofu drive --continue exited %d: %s\n%s", code, errOut.String(), out.String())
	}
	return out.String()
}

func TestResumeNoteOpensTheContinuedApp(t *testing.T) {
	store := sessionProject(t)
	recorded(t, store, "turn-head", "the head task", time.Now(), llm.Message{Role: llm.RoleUser, Content: "the head task"})
	if err := store.SetHead("turn-head"); err != nil {
		t.Fatal(err)
	}
	handle := continueCarry(store).Handle
	screen := continuedScreen(t, "resumed "+handle)
	if !strings.Contains(screen, "carrying 1 message") {
		t.Errorf("the first screen does not say what the resume carried:\n%s", screen)
	}
}

func TestResumeNoteSaysAContinueWithNothingRecordedStartedFresh(t *testing.T) {
	sessionProject(t)
	screen := continuedScreen(t, "started a new session")
	if !strings.Contains(screen, sessionFresh) {
		t.Errorf("the first screen does not say why the continue started fresh:\n%s", screen)
	}
}

func TestResumeSpinnerWritesNothingWhereOutputIsNotATerminal(t *testing.T) {
	var out bytes.Buffer
	restoring(&out, sessionResume{Session: "turn-head", Handle: "turn-head"})()
	if out.Len() != 0 {
		t.Errorf("restoring wrote %q into output that is not a terminal", out.String())
	}
}
