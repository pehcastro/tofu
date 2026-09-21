package turn

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"tofu/internal/konst"
	"tofu/internal/llm"
)

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestTheToolGuidanceNamesFindWhyItIsSlowHereAndWhatToUseInstead(t *testing.T) {
	for _, want := range []string{
		"never run find", "-not -path", "descends into every directory git ignores",
		"153 seconds", "15 milliseconds", "project_report", "glob", "search",
	} {
		if !strings.Contains(PreferTheToolOverTheShell, want) {
			t.Fatalf("the tool guidance never says %q:\n%s", want, PreferTheToolOverTheShell)
		}
	}
}

func TestEnvironmentCarriesTheDirectoryThePlatformTheDateAndTheBranch(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, ".git", "HEAD"), "ref: refs/heads/develop\n")
	nested := filepath.Join(root, "internal", "turn")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}

	block := Environment(nested, time.Date(2026, 9, 19, 11, 0, 0, 0, time.UTC))

	for _, want := range []string{nested, "platform: ", "today's date: 2026-09-19", "git repository: yes", "git branch: develop"} {
		if !strings.Contains(block, want) {
			t.Fatalf("the environment block is missing %q:\n%s", want, block)
		}
	}
}

func TestEnvironmentSaysSoWhenTheTreeIsNotAGitRepository(t *testing.T) {
	root := t.TempDir()
	if _, found := findUp(root, ".git", "HEAD"); found {
		t.Skip("this machine has a git repository above the temporary directory")
	}

	block := Environment(root, time.Now())

	if !strings.Contains(block, "git repository: no") || strings.Contains(block, "git branch") {
		t.Fatalf("the environment block is:\n%s", block)
	}
}

func TestProjectInstructionsTakeTheNearerAgentsFileOverTheFurtherOne(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "packages", "engine")
	write(t, filepath.Join(root, "AGENTS.md"), "the further rule")
	write(t, filepath.Join(nested, "AGENTS.md"), "the nearer rule")

	block := ProjectInstructions(nested, "")

	if !strings.Contains(block, "the nearer rule") {
		t.Fatalf("the nearer AGENTS.md is not in the block:\n%s", block)
	}
	if strings.Contains(block, "the further rule") {
		t.Fatalf("the further AGENTS.md won:\n%s", block)
	}
}

func TestProjectInstructionsReadTheClaudeFileInTheWorkingDirectory(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "CLAUDE.md"), "never an em dash")

	block := ProjectInstructions(root, "")

	if !strings.Contains(block, "never an em dash") || !strings.Contains(block, filepath.Join(root, "CLAUDE.md")) {
		t.Fatalf("CLAUDE.md did not reach the prompt:\n%s", block)
	}
}

func TestProjectInstructionsAreCappedAndSayWhatWasDropped(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "CLAUDE.md"), strings.Repeat("a", konst.ProjectInstructionsBytes+500))

	block := ProjectInstructions(root, "")

	if len(block) > konst.ProjectInstructionsBytes+200 {
		t.Fatalf("the block is %d bytes, past the %d cap plus its own notice", len(block), konst.ProjectInstructionsBytes)
	}
	if !strings.Contains(block, "capped at "+strconv.Itoa(konst.ProjectInstructionsBytes)+" bytes") || !strings.Contains(block, "dropped") {
		t.Fatalf("the block never says it was capped:\n%s", block)
	}
	if !strings.Contains(block, filepath.Join(root, "CLAUDE.md")) {
		t.Fatalf("the drop notice never names the file it cut:\n%s", block)
	}
}

func TestProjectInstructionsAreEmptyWhenTheTreeHasNeitherFile(t *testing.T) {
	if block := ProjectInstructions(t.TempDir(), t.TempDir()); block != "" {
		t.Fatalf("a tree with no AGENTS.md and no CLAUDE.md produced:\n%q", block)
	}
}

func TestTheRequestCarriesTheEnvironmentAheadOfTheTask(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, ".git", "HEAD"), "ref: refs/heads/develop\n")
	model := &stubModel{decisions: []llm.Decision{messageDecision()}}
	config := baseConfig(t, model, NewRegistry())
	config.Environment = Environment(root, time.Date(2026, 9, 19, 11, 0, 0, 0, time.UTC))

	if _, err := Run(context.Background(), config); err != nil {
		t.Fatal(err)
	}

	sent := model.requests[0].Messages
	first := sent[len(sent)-1]
	for _, want := range []string{root, "today's date: 2026-09-19", "git branch: develop", "platform: "} {
		if !strings.Contains(first.Content, want) {
			t.Fatalf("the model was never told %q, it received:\n%s", want, first.Content)
		}
	}
	if !strings.HasSuffix(first.Content, config.Task) {
		t.Fatalf("the task is not the last thing in the message:\n%s", first.Content)
	}
}

func TestTheDateAndTheBranchAreOutsideTheCachedPrefix(t *testing.T) {
	history := []llm.Message{
		{Role: llm.RoleUser, Content: "the first task"},
		{Role: llm.RoleAssistant, Content: "the first answer"},
	}
	sentFor := func(environment string) (prefix, whole []byte) {
		t.Helper()
		model := &stubModel{decisions: []llm.Decision{messageDecision()}}
		config := baseConfig(t, model, NewRegistry())
		config.System = "the fixed instructions, and the project file under them"
		config.History = history
		config.Environment = environment
		if _, err := Run(context.Background(), config); err != nil {
			t.Fatal(err)
		}
		sent := model.requests[0].Messages
		cached, err := json.Marshal(sent[:1+len(history)])
		if err != nil {
			t.Fatal(err)
		}
		all, err := json.Marshal(sent)
		if err != nil {
			t.Fatal(err)
		}
		return cached, all
	}

	today, todayWhole := sentFor("<env>\ntoday's date: 2026-09-19\ngit branch: develop\n</env>")
	later, laterWhole := sentFor("<env>\ntoday's date: 2027-01-01\ngit branch: main\n</env>")

	if !bytes.Equal(today, later) {
		t.Fatalf("a new date moved the cached prefix:\n%s\n%s", today, later)
	}
	if !bytes.Contains(today, []byte("the project file under them")) || !bytes.Contains(today, []byte("the first answer")) {
		t.Fatalf("the prefix compared holds neither the instructions nor the history:\n%s", today)
	}
	if bytes.Equal(todayWhole, laterWhole) {
		t.Fatal("the whole request is identical too, so the environment never reached it and this proves nothing")
	}
}
