package state

import (
	"strings"
	"testing"
)

func TestTargetsOfRecordedCases(t *testing.T) {
	cases := gateCases(t)
	for _, want := range []struct {
		id            string
		determination Determination
		suffix        string
		class         TargetClass
		location      TargetLocation
	}{
		{"tx-055", TargetsResolved, "/tickets/review/BOJI-026.md", ClassTicket, LocationInsideProject},
		{"tx-121", TargetUnknown, "/tickets/doing/BOJI-070.md", ClassTicket, LocationInsideProject},
		{"tx-018", TargetsResolved, "/.claude/agents/references/working-a-ticket.md", ClassDocument, LocationInsideProject},
		{"tx-019", TargetsResolved, "/.claude/settings.json", ClassConfig, LocationInsideProject},
		{"tx-040", TargetUnknown, "/scratchpad/rv.go", ClassSourceCode, LocationSessionScratch},
		{"tx-048", TargetsResolved, "/scratchpad/lintprobe/dirty.go", ClassSourceCode, LocationSessionScratch},
		{"tx-127", TargetUnknown, "/scratchpad/g.py", ClassSourceCode, LocationSessionScratch},
		{"tx-140", TargetsResolved, "/scratchpad/prov.py", ClassSourceCode, LocationSessionScratch},
		{"tx-027", TargetUnknown, "/tmp/b1.json", ClassConfig, LocationOutsideProject},
		{"tx-028", TargetsResolved, "/tickets/review", ClassTicket, LocationInsideProject},
	} {
		t.Run(want.id, func(t *testing.T) {
			one, ok := cases[want.id]
			if !ok {
				t.Fatalf("%s is not in the gate corpus", want.id)
			}
			got := TargetsOf(recordedInput(t, one))
			if got.Determination != want.determination {
				t.Fatalf("determination = %q, want %q, targets %+v", got.Determination, want.determination, got.Targets)
			}
			for _, target := range got.Targets {
				if !strings.HasSuffix(target.Path, want.suffix) {
					continue
				}
				if target.Class != want.class || target.Location != want.location {
					t.Fatalf("%s: class %q location %q, want %q and %q", target.Path, target.Class, target.Location, want.class, want.location)
				}
				return
			}
			t.Fatalf("no target ends in %q, got %+v", want.suffix, got.Targets)
		})
	}
}

func TestTargetsOfSaysUnknownWhenTheRecordedCommandWasCutShort(t *testing.T) {
	cases := gateCases(t)
	for _, id := range []string{"tx-082", "tx-122", "tx-128", "tx-004"} {
		one, ok := cases[id]
		if !ok {
			t.Fatalf("%s is not in the gate corpus", id)
		}
		got := TargetsOf(recordedInput(t, one))
		if got.Determination != TargetUnknown {
			t.Errorf("%s: determination = %q, want %q", id, got.Determination, TargetUnknown)
		}
	}
}

func TestTargetsOfSaysUnknownRatherThanNothing(t *testing.T) {
	for _, in := range []ToolGateInput{
		{Tool: "bash", Input: map[string]any{"command": `cat > "$UNSET_VAR/notes.md"`}, Cwd: "/repo", ProjectDir: "/repo"},
		{Tool: "bash", Input: map[string]any{"command": `python - <<'PY'` + "\nio.open(target,'w').write(x)\nPY\n"}, Cwd: "/repo", ProjectDir: "/repo"},
		{Tool: "write", Input: map[string]any{"bytes": 12}, Cwd: "/repo", ProjectDir: "/repo"},
		{Tool: "some_tool_the_harness_does_not_know", Input: map[string]any{}, Cwd: "/repo", ProjectDir: "/repo"},
	} {
		got := TargetsOf(in)
		if got.Determination != TargetUnknown {
			t.Errorf("%s %v: determination = %q, want %q", in.Tool, in.Input, got.Determination, TargetUnknown)
		}
		if got.Targets == nil {
			t.Errorf("%s %v: targets is null, it must be present and empty", in.Tool, in.Input)
		}
	}
}

func TestTargetsOfReadsNoWrite(t *testing.T) {
	got := TargetsOf(ToolGateInput{Tool: "bash", Input: map[string]any{"command": "go test ./internal/judge/... -count=1"}, Cwd: "/repo", ProjectDir: "/repo"})
	if got.Determination != NoWriteFound {
		t.Fatalf("determination = %q, want %q, targets %+v", got.Determination, NoWriteFound, got.Targets)
	}
}

func TestTargetsOfKeepsHeredocContentOutOfTheScan(t *testing.T) {
	command := "cd /repo && cat > notes.md <<'TEXT'\nrm -rf /etc/passwd\nsed -i s/a/b/ internal/turn/loop.go\nTEXT\n"
	got := TargetsOf(ToolGateInput{Tool: "bash", Input: map[string]any{"command": command}, Cwd: "/repo", ProjectDir: "/repo"})
	if got.Determination != TargetsResolved || len(got.Targets) != 1 || got.Targets[0].Path != "/repo/notes.md" {
		t.Fatalf("got %+v, want the redirect target alone", got)
	}
}
