package turn

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestInstructionFilesPerFolder(t *testing.T) {
	agentsFirst := []string{"AGENTS.md", "CLAUDE.md"}
	claudeFirst := []string{"CLAUDE.md", "AGENTS.md"}
	cases := []struct {
		name    string
		files   map[string]string
		sources []string
		sent    []string
		absent  []string
		skipped []string
	}{
		{"both in one folder sends AGENTS.md", map[string]string{"work/AGENTS.md": "agents rules", "work/CLAUDE.md": "claude rules"},
			agentsFirst, []string{"agents rules"}, []string{"claude rules"}, []string{"this project's CLAUDE.md"}},
		{"the order setting is read", map[string]string{"work/AGENTS.md": "agents rules", "work/CLAUDE.md": "claude rules"},
			claudeFirst, []string{"claude rules"}, []string{"agents rules"}, []string{"this project's AGENTS.md"}},
		{"different folders are both read", map[string]string{"AGENTS.md": "agents rules", "work/CLAUDE.md": "claude rules"},
			agentsFirst, []string{"agents rules", "claude rules"}, nil, nil},
		{"an empty preferred file does not count", map[string]string{"work/AGENTS.md": " \n", "work/CLAUDE.md": "claude rules"},
			agentsFirst, []string{"claude rules"}, nil, nil},
		{"a source left out is never read", map[string]string{"work/AGENTS.md": "agents rules", "work/CLAUDE.md": "claude rules"},
			[]string{"CLAUDE.md"}, []string{"claude rules"}, []string{"agents rules"}, nil},
		{"the personal file ignores the setting", map[string]string{"home/.claude/CLAUDE.md": "personal rules", "work/AGENTS.md": "agents rules"},
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
			block, _, skipped := ProjectInstructionsInOrder(work, filepath.Join(root, "home"), 0, c.sources)
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
