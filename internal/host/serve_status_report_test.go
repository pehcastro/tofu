package host

import (
	"context"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"tofu/internal/shell"
	"tofu/internal/status"
	roster "tofu/internal/subagent"
)

func TestServeStatusNamesTheLeadNestedAgentsAndAShellsOwnReport(t *testing.T) {
	release := make(chan struct{})
	play := func(_ context.Context, _ Pick, _ string, live Live) {
		live.Emit(Event{Kind: EventSubAgent, SubAgents: []SubAgentRow{
			{Name: "go-dev-1", State: roster.Working, Doing: "writes the parser"},
			{Name: "research-1", Parent: "go-dev-1", State: roster.WaitingAnswer},
		}})
		<-release
		live.Emit(Event{Kind: EventDone, Status: StatusFinished, Text: "finished in"})
	}
	registry := shell.OpenAt(filepath.Join(t.TempDir(), "shells"))
	c, _, _ := serving(t, play, ServeConfig{Shells: registry})
	latest := map[string]status.Record{}
	statusOf := func(id string, wanted func(status.Record) bool) func(wireLine) bool {
		return func(line wireLine) bool {
			if line.Method == "shell.output" && strings.Contains(string(line.Params), "7501") {
				t.Errorf("shell.output carried the report itself: %s", line.Params)
			}
			if line.Method != statusMethod {
				return false
			}
			var record status.Record
			_ = json.Unmarshal(line.Params, &record)
			latest[record.ID] = record
			return record.ID == id && wanted(record)
		}
	}
	in := func(state status.State) func(status.Record) bool {
		return func(record status.Record) bool { return record.State == state }
	}

	c.ask("1", "initialize", `{"client":"desk"}`)
	c.ask("2", "session.open", `{}`)
	var opened SessionOpenResult
	c.answer("2", &opened)
	var before StatusList
	c.ask("2b", "status.list", `{}`)
	c.answer("2b", &before)
	if len(before.Records) != 1 || before.Records[0].ID != statusApp || before.Records[0].State != status.Idle {
		t.Errorf("status.list before any turn = %+v, want the lead alone, idle", before.Records)
	}
	c.ask("3", "turn.send", `{"session":"`+opened.Session+`","text":"go"}`)
	c.until(statusOf(statusApp, in(status.Working)), "the lead working")
	c.until(statusOf("agents/go-dev-1", in(status.Working)), "agents/go-dev-1 working")
	nested := c.until(statusOf("agents/go-dev-1/agents/research-1", in(status.Blocked)), "the nested sub-agent blocked")
	var asking status.Record
	_ = json.Unmarshal(nested.Params, &asking)
	if asking.Kind != status.Question || asking.App != statusApp {
		t.Errorf("the nested sub-agent reads %+v, want blocked:question with app tofu", asking)
	}

	choice, err := shell.Resolve("")
	if err != nil {
		t.Fatal(err)
	}
	command := `printf 'up\033]7501;state=working:progress=40\aon\n'; sleep 2`
	cmd := exec.Command(choice.Path, "-c", command)
	cmd.Dir = t.TempDir()
	kept, err := registry.YieldReady(context.Background(), cmd, command, "lead", shell.Wait{Within: 200 * time.Millisecond, Kept: shell.KeptBackground})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = registry.Kill(kept.Shell.Name) })
	id := status.Path("shells", kept.Shell.Name)
	reported := c.until(statusOf(id, func(record status.Record) bool { return record.Progress != nil }), id+" at the program's progress")
	var program status.Record
	_ = json.Unmarshal(reported.Params, &program)
	if program.State != status.Working || *program.Progress != 40 || program.Title != kept.Shell.Name {
		t.Errorf("the shell's own report reads %+v, want working at 40 titled %s", program, kept.Shell.Name)
	}
	c.until(statusOf(id, in(status.Done)), id+" done once it exits 0, its working report dropped")

	var listed StatusList
	c.ask("4", "status.list", `{}`)
	c.answer("4", &listed)
	ids := map[string]status.State{}
	for _, record := range listed.Records {
		ids[record.ID] = record.State
	}
	if ids[statusApp] != status.Working || ids["agents/go-dev-1"] != status.Working || ids[id] != status.Done {
		t.Errorf("status.list = %v, want the lead and go-dev-1 working and %s done", ids, id)
	}
	close(release)
	c.until(statusOf(statusApp, in(status.Done)), "the lead done when its turn ends")
}

func TestStatusAgentIDFollowsParentsAndSurvivesALoop(t *testing.T) {
	rows := []SubAgentRow{{Name: "a"}, {Name: "b", Parent: "a"}, {Name: "c", Parent: "b"}, {Name: "x", Parent: "y"}, {Name: "y", Parent: "x"}, {Name: "lost", Parent: "gone"}}
	for name, want := range map[string]string{
		"a":    "agents/a",
		"c":    "agents/a/agents/b/agents/c",
		"lost": "agents/gone/agents/lost",
	} {
		if got := agentStatusID(rows, name); got != want {
			t.Errorf("agentStatusID(%s) = %q, want %q", name, got, want)
		}
	}
	if looped := agentStatusID(rows, "x"); !strings.HasSuffix(looped, "agents/x") {
		t.Errorf("a parent loop gave %q", looped)
	}
}
