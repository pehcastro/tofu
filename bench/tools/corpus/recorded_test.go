package corpus

import (
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"

	sessions "tofu/bench/corpus"
	"tofu/internal/sys"
)

type recordedArgs struct {
	Pattern string `json:"pattern"`
	Path    string `json:"path"`
	Glob    string `json:"glob"`
}

func TestExtractRecordedTextSearchCalls(t *testing.T) {
	if os.Getenv("TOFU_TOOLS_EXTRACT") != "1" {
		t.Skip("set TOFU_TOOLS_EXTRACT=1 to print the search calls .tofu/sessions carries, and the grep calls the sessions recorded before 2026-09-21")
	}
	walked, err := sessions.WalkSessions(sys.RecordedStateDir("sessions"))
	if err != nil {
		t.Fatalf("WalkSessions: %v", err)
	}
	t.Logf("turns %d skipped %d", len(walked.Turns), len(walked.Skipped))
	for _, s := range walked.Skipped {
		t.Logf("skip: %s %s", s.Path, s.Reason)
	}
	for _, turn := range walked.Turns {
		for _, step := range turn.Steps {
			for _, call := range step.ToolCalls {
				if call.Tool != "grep" && call.Tool != "search" {
					continue
				}
				var a recordedArgs
				if err := json.Unmarshal(call.Args, &a); err != nil {
					continue
				}
				root := a.Path
				if root == "" {
					root = "."
				}
				out, _ := exec.Command("git", "-C", "../../..", "grep", "-nE", a.Pattern, "--", root).CombinedOutput()
				lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
				if len(lines) == 1 && lines[0] == "" {
					lines = nil
				}
				t.Logf("turn=%s tool=%s pattern=%q path=%q glob=%q live_matches=%d",
					turn.ID, call.Tool, a.Pattern, a.Path, a.Glob, len(lines))
				for i, l := range lines {
					if i >= 6 {
						t.Logf("   ... %d more", len(lines)-6)
						break
					}
					t.Logf("   %s", l)
				}
			}
		}
	}
}
