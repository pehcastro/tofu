package state

import (
	"encoding/json"
	"strings"
	"testing"

	"tofu/internal/judge/question"
)

const toolGateV3Path = "../../../library/questions/tool_gate@3.yaml"

func TestToolGateV3QuestionSetLintsClean(t *testing.T) {
	set, err := question.Load(toolGateV3Path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if set.QuestionsVersion != 3 {
		t.Fatalf("questions_version = %d, want 3", set.QuestionsVersion)
	}
	findings := question.Lint(set, question.DefaultCaps())
	for _, finding := range findings {
		t.Errorf("finding: %s", finding)
	}
	approval, ok := set.Question("approval")
	if !ok {
		t.Fatal("the set asks no approval question")
	}
	if !strings.Contains(approval.Instructions, "context.write_targets.determination") {
		t.Fatalf("approval does not read the determination: %q", approval.Instructions)
	}
}

func TestBuildToolGateV3OnRecordedCases(t *testing.T) {
	cases := gateCases(t)
	for _, want := range []struct {
		id     string
		suffix string
		target WriteTarget
	}{
		{"tx-055", "/.local/boji/tickets/review/BOJI-026.md", WriteTarget{Class: ClassTicket, Location: LocationInsideProject}},
		{"tx-048", "/scratchpad/lintprobe/dirty.go", WriteTarget{Class: ClassSourceCode, Location: LocationSessionScratch}},
	} {
		one, ok := cases[want.id]
		if !ok {
			t.Fatalf("%s is not in the gate corpus", want.id)
		}
		built, version, err := BuildToolGateV3(recordedInput(t, one))
		if err != nil {
			t.Fatalf("%s: %v", want.id, err)
		}
		targets := contextTargets(t, built)
		t.Logf("%s state_builder %s: %s", want.id, version, built)
		if targets.Determination != TargetsResolved || len(targets.Targets) != 1 {
			t.Fatalf("%s: write_targets = %+v", want.id, targets)
		}
		got := targets.Targets[0]
		if !strings.HasSuffix(got.Path, want.suffix) || got.Class != want.target.Class || got.Location != want.target.Location {
			t.Errorf("%s: target = %+v, want a path ending %q with class %q and location %q", want.id, got, want.suffix, want.target.Class, want.target.Location)
		}
	}
}

func contextTargets(t *testing.T, built []byte) WriteTargets {
	t.Helper()
	var decoded struct {
		Context struct {
			Targets WriteTargets `json:"write_targets"`
		} `json:"context"`
	}
	if err := json.Unmarshal(built, &decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return decoded.Context.Targets
}

func TestBuildToolGateV3CarriesTheTargetsAndItsOwnShape(t *testing.T) {
	built, version, err := BuildToolGateV3(ToolGateInput{
		Agent: "tofu", Tool: "bash",
		Input:      map[string]any{"command": "cat > .local/boji/tickets/doing/BOJI-070.md <<'TICKET'\nid: BOJI-070\nTICKET\n"},
		Cwd:        "/repo",
		ProjectDir: "/repo",
	})
	if err != nil {
		t.Fatalf("BuildToolGateV3: %v", err)
	}
	if version == ToolGateVersion() {
		t.Fatalf("the v3 state shape %q is the same as the old one, a new shape needs a new version", version)
	}

	var decoded struct {
		Context struct {
			Targets  *WriteTargets `json:"write_targets"`
			Messages *[]string     `json:"user_recent_messages"`
		} `json:"context"`
	}
	if err := json.Unmarshal(built, &decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if decoded.Context.Messages == nil {
		t.Fatal("user_recent_messages is absent, it must be present and empty")
	}
	targets := decoded.Context.Targets
	if targets == nil {
		t.Fatal("write_targets is absent")
	}
	if targets.Determination != TargetsResolved || len(targets.Targets) != 1 {
		t.Fatalf("write_targets = %+v, want one resolved target", targets)
	}
	one := targets.Targets[0]
	if one.Path != "/repo/.local/boji/tickets/doing/BOJI-070.md" || one.Class != ClassTicket || one.Location != LocationInsideProject {
		t.Fatalf("target = %+v", one)
	}
}
