package turn

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"

	"tofu/internal/llm"
	"tofu/internal/subagent"
)

func messageDoing(id, to, do string, owns ...string) llm.Decision {
	args, err := json.Marshal(map[string]any{"to": to, "do": do, "owns": owns})
	if err != nil {
		panic(err)
	}
	return toolCallDecision(llm.ToolCall{ID: id, Name: "message", Arguments: args})
}

func writeCall(id, path string) llm.Decision {
	args, err := json.Marshal(map[string]string{"path": path, "content": "x"})
	if err != nil {
		panic(err)
	}
	return toolCallDecision(llm.ToolCall{ID: id, Name: "write", Arguments: args})
}

func writingCrew(t *testing.T, model *crew) (Config, string) {
	t.Helper()
	root := t.TempDir()
	write, err := NewWriteTool(root)
	if err != nil {
		t.Fatal(err)
	}
	base := Config{Model: model, Spend: SpendAPIKey, Tools: NewRegistry(write), Caps: Caps{MaxSteps: 20}, ResultBytesCap: 4096,
		ArtifactDir: filepath.Join(root, "artifacts"), NewID: func() string { return "turn-lead" }}
	spawn := NewSpawnTool("turn-lead", base, &subagent.Roster{})
	lead := base
	lead.Task, lead.Tools, lead.Inbox = "hand the work to sub-agents", NewRegistry(write, spawn), spawn.Inbox
	return lead, root
}

func TestTheLeadGrantsARefusedPathAndTheWriteRuns(t *testing.T) {
	const writer, holder = "write other/x.txt", "hold theirs"
	model := newCrew(map[string][]llm.Decision{
		leadKey: {
			toolCallDecision(spawnCall("call-a", writer, "mine/**").ToolCalls[0], spawnCall("call-b", holder, "theirs/**").ToolCalls[0]),
			messageDoing("grant-held", "sub-1", "grant", "theirs/**"),
			messageDoing("grant-unknown", "sub-9", "grant", "other/**"),
			messageDoing("grant-empty", "sub-1", "grant"),
			messageDoing("grant", "sub-1", "grant", "other/**"),
			claimDecision("granted other/** to sub-1"),
			called("subagents", map[string]any{}),
			claimDecision("sub-1 wrote it"),
			claimDecision("sub-2 is done"),
		},
		writer: {
			writeCall("first", "other/x.txt"), claimDecision("other/x.txt is outside my paths"),
			writeCall("again", "other/x.txt"), claimDecision("wrote other/x.txt"),
		},
		holder: {claimDecision("held theirs")},
	})
	release := model.hold(holder, 1)
	defer release()
	lead, root := writingCrew(t, model)
	led := startLead(t.Context(), lead, nil)
	waitFor(t, "the lead's listing after sub-1's second report", func() bool { return model.answered(leadKey) >= 8 })
	release()
	led.wait(t)

	if written, err := os.ReadFile(filepath.Join(root, "other", "x.txt")); err != nil || string(written) != "x" {
		t.Errorf("after the grant other/x.txt holds %q: %v", written, err)
	}
	for call, want := range map[string]string{
		"grant-held":    "sub-2",
		"grant-unknown": "no sub-agent is named",
		"grant-empty":   "owns",
		"grant":         "other/**",
		"subagents":     "grant other/**",
	} {
		if got := answerTo(t, model, call); !strings.Contains(got, want) {
			t.Errorf("the lead's %s call came back without %q:\n%s", call, want, got)
		}
	}
	if listing := answerTo(t, model, "subagents"); !strings.Contains(listing, "mine/** other/**") {
		t.Errorf("subagents does not show sub-1's owns after the grant:\n%s", listing)
	}
	if first := lastUser(model.requests(leadKey)[1]); !strings.Contains(first, "do grant") {
		t.Errorf("sub-1's report on the refused write does not point at do grant:\n%s", first)
	}
}

func TestTheLeadRevokesAPathAndALaterWriteThereIsRefused(t *testing.T) {
	const holder, taker = "hold a and b", "take b"
	model := newCrew(map[string][]llm.Decision{
		leadKey: {
			spawnCall("call-a", holder, "a/**", "b/**"),
			messageDoing("revoke", "sub-1", "revoke", "b/**"),
			messageDoing("revoke-unheld", "sub-1", "revoke", "c/**"),
			messageTo("write-b", "sub-1", "write b/y.txt"),
			spawnCall("call-b", taker, "b/**"),
			claimDecision("a report"), claimDecision("another report"), claimDecision("a third report"),
		},
		holder: {
			claimDecision("holding a and b"),
			writeCall("late", "b/y.txt"), claimDecision("b/y.txt was refused"),
		},
		taker: {claimDecision("took b")},
	})
	lead, root := writingCrew(t, model)
	led := startLead(t.Context(), lead, nil)
	led.wait(t)

	if _, err := os.Stat(filepath.Join(root, "b", "y.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("sub-1 wrote b/y.txt after b/** was revoked: %v", err)
	}
	for call, want := range map[string]string{"revoke": "a/**", "revoke-unheld": "does not hold", "call-b": "holding b/**"} {
		if got := answerTo(t, model, call); !strings.Contains(got, want) {
			t.Errorf("the lead's %s call came back without %q:\n%s", call, want, got)
		}
	}
}

type scratchCrew struct {
	mu       sync.Mutex
	lead     []llm.Decision
	leadSaw  []string
	subSteps int
	command  func(scratch string, step int) llm.Decision
}

var scratchNamed = regexp.MustCompile(`scratch folder is (\S+?),`)

func (m *scratchCrew) Ask(ctx context.Context, request llm.Request) (llm.Decision, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if SubAgentAsking(ctx) == "" {
		m.leadSaw = append(m.leadSaw, request.Messages[len(request.Messages)-1].Content)
		if len(m.lead) == 0 {
			return llm.Decision{}, errors.New("the lead has no decision left")
		}
		next := m.lead[0]
		m.lead = m.lead[1:]
		return next, nil
	}
	scratch := ""
	for _, message := range request.Messages {
		if found := scratchNamed.FindStringSubmatch(message.Content); found != nil {
			scratch = found[1]
		}
	}
	m.subSteps++
	return m.command(scratch, m.subSteps), nil
}

func TestASubAgentWritesAndDeletesInItsScratchWithNoAsk(t *testing.T) {
	root := t.TempDir()
	write, err := NewWriteTool(root)
	if err != nil {
		t.Fatal(err)
	}
	bash, err := NewBashTool(root)
	if err != nil || strings.Contains(bash.choice.Label, "powershell") {
		t.Skip("no bash resolved on this machine")
	}
	var seen string
	model := &scratchCrew{
		lead: []llm.Decision{spawnCall("call-spawn", "probe and clean up"), claimDecision("sub-1 cleaned up")},
		command: func(scratch string, step int) llm.Decision {
			seen, scratch = scratch, filepath.ToSlash(scratch)
			switch step {
			case 1:
				return writeCall("log", scratch+"/run.log")
			case 2:
				return called("bash", map[string]any{"command": "rm " + scratch + "/run.log"})
			}
			return claimDecision("wrote and removed " + scratch + "/run.log")
		},
	}
	asked := 0
	base := Config{Model: model, Spend: SpendAPIKey, Tools: NewRegistry(write, bash), Caps: Caps{MaxSteps: 20}, ResultBytesCap: 4096, Project: root,
		ArtifactDir: filepath.Join(root, "artifacts"), NewID: func() string { return "turn-lead" },
		Gate: asksForSubAgents{}, GateMode: GateEnforce,
		Person: func(context.Context, GateRequest, GateDecision) (PersonAnswer, error) {
			asked++
			return PersonDenied, nil
		}}
	spawn := NewSpawnTool("turn-lead", base, &subagent.Roster{})
	spawn.Project = root
	lead := base
	lead.Task, lead.Tools, lead.Inbox = "hand the probe to a sub-agent", NewRegistry(write, bash, spawn), spawn.Inbox
	startLead(t.Context(), lead, nil).wait(t)

	if seen == "" {
		t.Fatal("the sub-agent was never told of a scratch folder")
	}
	for _, said := range model.leadSaw {
		if strings.Contains(said, "asks to run") {
			t.Errorf("a scratch call reached the lead:\n%s", said)
		}
	}
	if asked != 0 {
		t.Errorf("the person was asked %d times", asked)
	}
	if _, err := os.Stat(seen); err != nil {
		t.Errorf("the scratch folder %s is not there: %v", seen, err)
	}
	if _, err := os.Stat(filepath.Join(seen, "run.log")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("run.log is still in the scratch: %v", err)
	}
	if _, inside := within(root, seen); inside {
		t.Errorf("the scratch folder %s is inside the project %s", seen, root)
	}
	ran := 0
	for _, row := range spawn.SubAgentRows() {
		for _, step := range row.Steps {
			for _, call := range step.ToolCalls {
				if call.Refused || call.Error != "" {
					t.Errorf("%s %s was refused or failed: %s", call.Tool, call.Command, call.Error)
				}
				if call.GateReason != nil && call.GateReason.AllowedBy == allowedByOwnScratch {
					ran++
				}
			}
		}
	}
	if ran != 2 {
		t.Errorf("%d calls record the scratch as what allowed them, want the write and the rm", ran)
	}
}
