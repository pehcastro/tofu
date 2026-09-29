package turn

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"tofu/internal/settings"
)

func TestOldInstructionSourcesReadAsAChoice(t *testing.T) {
	for old, want := range map[string]string{
		"AGENTS.md,CLAUDE.md": settings.InstructionsAgentsFirst,
		"AGENTS.md":           settings.InstructionsAgentsFirst,
		"CLAUDE.md,AGENTS.md": settings.InstructionsClaudeFirst,
		"CLAUDE.md":           settings.InstructionsClaudeFirst,
		"both":                settings.InstructionsBoth,
	} {
		path := filepath.Join(t.TempDir(), settings.FileName)
		if err := os.WriteFile(path, []byte(`{"instructionSources": "`+old+`"}`), 0o644); err != nil {
			t.Fatal(err)
		}
		store, err := settings.Open(path, "")
		if err != nil {
			t.Fatal(err)
		}
		if got := store.Text(settings.InstructionSources); got != want {
			t.Errorf("%q reads as %q, want %q", old, got, want)
		}
		if _, fromFile := store.Source(settings.InstructionSources); !fromFile {
			t.Errorf("%q was dropped for the default instead of read", old)
		}
	}
}

func TestInstructionSourcesSetMapsOldValuesAndRefusesUnknown(t *testing.T) {
	path := filepath.Join(t.TempDir(), settings.FileName)
	store, err := settings.Open(path, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetText(settings.Global, settings.InstructionSources, "CLAUDE.md,AGENTS.md"); err != nil {
		t.Fatal(err)
	}
	if written, _ := os.ReadFile(path); !strings.Contains(string(written), settings.InstructionsClaudeFirst) {
		t.Errorf("an old value was written as itself:\n%s", written)
	}
	err = store.SetText(settings.Global, settings.InstructionSources, "GEMINI.md")
	if err == nil {
		t.Fatal("an unknown value was accepted")
	}
	for _, choice := range []string{settings.InstructionsAgentsFirst, settings.InstructionsClaudeFirst, settings.InstructionsBoth} {
		if !strings.Contains(err.Error(), choice) {
			t.Errorf("the refusal %q does not name %s", err, choice)
		}
	}
}

func TestInstructionFilesPerFolder(t *testing.T) {
	agentsFirst, claudeFirst, both := settings.InstructionsAgentsFirst, settings.InstructionsClaudeFirst, settings.InstructionsBoth
	bothFiles := map[string]string{"work/AGENTS.md": "agents rules", "work/CLAUDE.md": "claude rules"}
	cases := []struct {
		name    string
		files   map[string]string
		choice  string
		sent    []string
		absent  []string
		skipped []string
	}{
		{"agents-first sends AGENTS.md only", bothFiles,
			agentsFirst, []string{"agents rules"}, []string{"claude rules"}, []string{"this project's CLAUDE.md"}},
		{"claude-first sends CLAUDE.md only", bothFiles,
			claudeFirst, []string{"claude rules"}, []string{"agents rules"}, []string{"this project's AGENTS.md"}},
		{"both sends both", bothFiles,
			both, []string{"agents rules", "claude rules"}, nil, nil},
		{"claude-first falls back to AGENTS.md", map[string]string{"work/AGENTS.md": "agents rules"},
			claudeFirst, []string{"agents rules"}, nil, nil},
		{"different folders are both read", map[string]string{".git/HEAD": "ref", "AGENTS.md": "agents rules", "work/CLAUDE.md": "claude rules"},
			agentsFirst, []string{"agents rules", "claude rules"}, nil, nil},
		{"a parent with no git is not read", map[string]string{"CLAUDE.md": "parent rules"},
			agentsFirst, nil, []string{"parent rules"}, nil},
		{"another harness's home is not read", map[string]string{"home/.claude/CLAUDE.md": "claude home", "home/.agents/AGENTS.md": "agents home"},
			agentsFirst, nil, []string{"claude home", "agents home"}, nil},
		{"the home gives only ~/.tofu/AGENTS.md", map[string]string{"home/.claude/CLAUDE.md": "claude home", "home/.tofu/AGENTS.md": "tofu home"},
			agentsFirst, []string{"tofu home"}, []string{"claude home"}, nil},
		{"an empty preferred file does not count", map[string]string{"work/AGENTS.md": " \n", "work/CLAUDE.md": "claude rules"},
			agentsFirst, []string{"claude rules"}, nil, nil},
		{"the ~/.tofu file ignores the setting", map[string]string{"home/.tofu/AGENTS.md": "personal rules", "work/AGENTS.md": "agents rules"},
			claudeFirst, []string{"personal rules", "agents rules"}, nil, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := t.TempDir()
			for name, body := range c.files {
				path := filepath.Join(root, name)
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			work := filepath.Join(root, "work")
			if err := os.MkdirAll(work, 0o755); err != nil {
				t.Fatal(err)
			}
			block, _, skipped := ProjectInstructionsInOrder(work, filepath.Join(root, "home"), 0, c.choice)
			if len(c.sent) == 0 && block != "" {
				t.Errorf("nothing should be sent, got:\n%s", block)
			}
			for _, want := range c.sent {
				if !strings.Contains(block, want) {
					t.Errorf("%q was not sent:\n%s", want, block)
				}
			}
			for _, unwanted := range c.absent {
				if strings.Contains(block, unwanted) {
					t.Errorf("%q was sent:\n%s", unwanted, block)
				}
			}
			for _, want := range c.skipped {
				if !slices.ContainsFunc(skipped, func(said string) bool { return strings.Contains(said, want) }) {
					t.Errorf("skipped %v does not name %q", skipped, want)
				}
			}
			if len(c.skipped) == 0 && len(skipped) > 0 {
				t.Errorf("nothing was skipped, got %v", skipped)
			}
		})
	}
}
