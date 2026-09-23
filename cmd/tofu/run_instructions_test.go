package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"tofu/internal/llm"
	"tofu/internal/turn"
)

const (
	projectInstructionFixture  = "the project instruction fixture, which is not the task"
	personalInstructionFixture = "the personal instruction fixture, which is not the task"
)

func instructionFixtures(t *testing.T) string {
	t.Helper()
	dir, home := t.TempDir(), t.TempDir()
	writeFixture(t, filepath.Join(dir, "CLAUDE.md"), projectInstructionFixture)
	writeFixture(t, filepath.Join(home, ".claude", "CLAUDE.md"), personalInstructionFixture)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	return dir
}

func writeFixture(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func sentToTheModel(t *testing.T, dir string, args ...string) string {
	t.Helper()
	opts, err := parseRunArgs(append(append([]string{"--dir", dir}, args...), "a task"))
	if err != nil {
		t.Fatalf("parseRunArgs %v: %v", args, err)
	}
	built, _, err := buildRunTools(opts.dir, opts.toolSet)
	if err != nil {
		t.Fatal(err)
	}
	model := &sendModel{queued: []llm.Decision{{Build: "stub-model", Outcome: llm.OutcomeMessage, Content: "done"}}}
	config, _ := mustConfig(t, opts, built, runtime{model: model, spend: turn.SpendSubscription})
	if _, err := turn.Run(context.Background(), config); err != nil {
		t.Fatalf("turn.Run: %v", err)
	}
	if len(model.requests) != 1 {
		t.Fatalf("the model was asked %d times, want once", len(model.requests))
	}
	var sent []string
	for _, message := range model.requests[0].Messages {
		sent = append(sent, message.Content)
	}
	return strings.Join(sent, "\n")
}

func TestTheInstructionsOffArmSendsNeitherTheProjectNorTheHomeInstructionFile(t *testing.T) {
	sent := sentToTheModel(t, instructionFixtures(t), "--no-instructions")

	for _, unwanted := range []string{projectInstructionFixture, personalInstructionFixture, "instructions from"} {
		if strings.Contains(sent, unwanted) {
			t.Fatalf("--no-instructions still sent %q to the model", unwanted)
		}
	}
}

func TestTheDefaultStillSendsBothInstructionFiles(t *testing.T) {
	sent := sentToTheModel(t, instructionFixtures(t))

	for _, wanted := range []string{projectInstructionFixture, personalInstructionFixture, "instructions from this project's CLAUDE.md"} {
		if !strings.Contains(sent, wanted) {
			t.Fatalf("the default run never sent %q, so the flag changed the default", wanted)
		}
	}
}

func TestTheHelpSaysExactlyWhatTheInstructionsOffArmTurnsOff(t *testing.T) {
	help := oneLine(runUsage())
	if !strings.Contains(help, oneLine(turn.InstructionsOff)) {
		t.Fatalf("tofu run --help does not say what --no-instructions turns off:\nwant: %s\ngot:\n%s", turn.InstructionsOff, runUsage())
	}
	if !strings.Contains(help, "--no-instructions") {
		t.Fatalf("tofu run --help never names the flag:\n%s", runUsage())
	}
}

func TestShowPromptSaysTheInstructionBlockIsAbsentByRequestRatherThanByAccident(t *testing.T) {
	dir := instructionFixtures(t)

	var off, offErr strings.Builder
	if code := runVerb([]string{"--dir", dir, "--no-instructions", "--show-prompt", "a task"}, &off, &offErr); code != exitOK {
		t.Fatalf("tofu run --show-prompt --no-instructions exited %d: %s", code, offErr.String())
	}
	printed := off.String()
	if !strings.Contains(oneLine(printed), oneLine("instruction files: off by request, and "+turn.InstructionsOff)) {
		t.Fatalf("--show-prompt does not say the block is absent by request:\n%s", printed)
	}
	for _, unwanted := range []string{projectInstructionFixture, personalInstructionFixture} {
		if strings.Contains(printed, unwanted) {
			t.Fatalf("--show-prompt under --no-instructions still printed %q", unwanted)
		}
	}

	var on, onErr strings.Builder
	if code := runVerb([]string{"--dir", dir, "--show-prompt", "a task"}, &on, &onErr); code != exitOK {
		t.Fatalf("tofu run --show-prompt exited %d: %s", code, onErr.String())
	}
	if !strings.Contains(on.String(), "instruction files: on") {
		t.Fatalf("--show-prompt without the flag never says the instruction files are on:\n%s", on.String())
	}
}
