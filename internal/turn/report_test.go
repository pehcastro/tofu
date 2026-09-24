package turn

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"tofu/internal/llm"
	"tofu/internal/secret"
	"tofu/internal/subagent"
)

func childReport(t *testing.T, root string, decisions []llm.Decision) ChildReport {
	t.Helper()
	_, spawn := parentTurn(t, root, decisions)
	spawnDirect(t, spawn, "write the greeting under mine/", "mine/**")
	reports := spawn.Reports()
	if len(reports) != 1 {
		t.Fatalf("the parent holds %d reports, want one", len(reports))
	}
	return reports[0]
}

func TestAChildThatFinishedAndNoticedSomethingIsDoneWithConcerns(t *testing.T) {
	clean := childReport(t, t.TempDir(), []llm.Decision{
		writeCall("call-1", "mine/hello.txt", "written by the child"),
		claimDecision("I wrote the greeting"),
	})
	noticed := childReport(t, t.TempDir(), []llm.Decision{
		toolCallDecision(llm.ToolCall{ID: "call-1", Name: "read", Arguments: json.RawMessage(`{"path":"mine/ghost.txt"}`)}),
		claimDecision("I wrote the greeting"),
	})

	if clean.Completion != subagent.Done {
		t.Fatalf("a child that did the work and hit nothing reads %s, want done: %+v", clean.Completion, clean.Findings)
	}
	if noticed.Completion != subagent.DoneWithConcerns {
		t.Fatalf("a child that finished and was refused a write reads %s, want done_with_concerns", noticed.Completion)
	}
	if clean.State != noticed.State || clean.Outcome != noticed.Outcome {
		t.Fatalf("the two children differ in state or outcome, so the completion is not what carries the difference: %s/%s and %s/%s",
			clean.State, clean.Outcome, noticed.State, noticed.Outcome)
	}
	if len(noticed.Findings) != 1 || noticed.Findings[0].Bucket != subagent.ActOn {
		t.Fatalf("the refused write is not one finding to act on: %+v", noticed.Findings)
	}
	if reason := noticed.Findings[0].Reason; strings.Contains(reason, "\n") || !strings.Contains(reason, "ghost.txt") {
		t.Fatalf("the finding reason is not one line naming what happened: %q", reason)
	}
	t.Logf("clean: %s, noticed: %s %s", clean.Completion, noticed.Completion, noticed.Findings[0].Reason)
}

func TestTheParentTellsTheTwoApartFromTheTypedValueAndNotTheProse(t *testing.T) {
	report := childReport(t, t.TempDir(), []llm.Decision{
		toolCallDecision(llm.ToolCall{ID: "call-1", Name: "read", Arguments: json.RawMessage(`{"path":"mine/ghost.txt"}`)}),
		claimDecision("all done, everything went fine"),
	})

	if report.Completion == subagent.Done {
		t.Fatal("the child's prose said it went fine and the completion agreed with the prose")
	}
	read := ChildReport{}
	written, err := json.Marshal(report)
	if err != nil || json.Unmarshal(written, &read) != nil {
		t.Fatalf("the handback does not cross JSON: %v", err)
	}
	if read.Completion != subagent.DoneWithConcerns {
		t.Fatalf("read back, the completion is %s", read.Completion)
	}
	if !strings.Contains(string(written), `"completion":"done_with_concerns"`) {
		t.Fatalf("a parent has to parse prose to find the completion: %s", written)
	}
}

func TestTheLearningStepRunsOnEveryHandbackAndSaysSoWhenItFoundNothing(t *testing.T) {
	nothing := childReport(t, t.TempDir(), []llm.Decision{
		writeCall("call-1", "mine/hello.txt", "written by the child"),
		claimDecision("I wrote the greeting"),
	})
	warned := claimDecision("I wrote the greeting")
	warned.Warnings = []string{"the wire answered on attempt 2"}
	something := childReport(t, t.TempDir(), []llm.Decision{
		writeCall("call-1", "mine/hello.txt", "written by the child"),
		warned,
	})

	if nothing.Learned == nil || len(nothing.Learned) != 0 {
		t.Fatalf("a child that learned nothing carries %v, and an absent field cannot be told from a step that never ran", nothing.Learned)
	}
	written, err := json.Marshal(nothing)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(written), `"learned":[]`) {
		t.Fatalf("the empty learning is missing from the handback rather than stated in it: %s", written)
	}
	if !strings.Contains(nothing.Text(), "learned nothing:") {
		t.Fatalf("the handback is silent about the learning step: %q", nothing.Text())
	}
	if len(something.Learned) != 1 || something.Learned[0] != "the wire answered on attempt 2" {
		t.Fatalf("what the run raised did not reach the handback: %v", something.Learned)
	}
	t.Logf("empty: %q", nothing.Text())
}

func TestEscalationCarriesWhatEachOfTheThreeAttemptsWas(t *testing.T) {
	agent := subagent.SubAgent{ID: "turn-parent-c1", Mission: "write the greeting", Owns: []string{"mine/**"}}
	attempts := []Row{
		{ID: "turn-parent-c1", Outcome: OutcomeStepCap, Steps: []StepRow{{ToolCalls: []ToolCallRow{{Tool: "read"}, {Tool: "write"}}}}},
		{ID: "turn-parent-c1-r", Outcome: OutcomeLoopGuard, Steps: []StepRow{{ToolCalls: []ToolCallRow{{Tool: "bash"}}}}},
		{ID: "turn-parent-c1-r-r", Outcome: OutcomeStopped},
	}

	written, err := json.Marshal(reportOf(agent, attempts, subagent.InReview))
	if err != nil {
		t.Fatal(err)
	}
	var read ChildReport
	if err := json.Unmarshal(written, &read); err != nil {
		t.Fatalf("reading the handback back: %v", err)
	}

	want := []subagent.Attempt{
		{ID: "turn-parent-c1", Tried: "read, write", Outcome: "step_cap"},
		{ID: "turn-parent-c1-r", Tried: "bash", Outcome: "loop_guard"},
		{ID: "turn-parent-c1-r-r", Tried: "nothing ran", Outcome: "stopped"},
	}
	if len(read.Attempts) != len(want) {
		t.Fatalf("the handback carries %d attempts, and a count is not what was tried: %+v", len(read.Attempts), read.Attempts)
	}
	for i, attempt := range read.Attempts {
		if attempt != want[i] {
			t.Fatalf("attempt %d reads %+v, want %+v", i+1, attempt, want[i])
		}
	}
	text := read.Text()
	for _, line := range []string{"escalating after 3 attempts", "turn-parent-c1-r, tried bash, ended loop_guard"} {
		if !strings.Contains(text, line) {
			t.Fatalf("the handback text does not say %q:\n%s", line, text)
		}
	}
	t.Logf("handback:\n%s", text)
}

func TestAFailureTheChildRecoveredFromIsDismissedAndNotADefect(t *testing.T) {
	exit := 1
	recovered := findings(Row{Outcome: OutcomeStopped, Steps: []StepRow{{ToolCalls: []ToolCallRow{
		{Tool: "bash", Command: "go build ./...", ExitCode: &exit},
		{Tool: "bash", Command: "go build ./internal/subagent/..."},
	}}}}, subagent.InReview)
	stuck := findings(Row{Outcome: OutcomeStopped, Steps: []StepRow{{ToolCalls: []ToolCallRow{
		{Tool: "bash", Command: "go build ./...", ExitCode: &exit},
		{Tool: "read", Error: "no file is at that path"},
	}}}}, subagent.InReview)

	if len(recovered) != 1 || recovered[0].Bucket != subagent.Dismissed {
		t.Fatalf("a command that failed and then ran is not dismissed: %+v", recovered)
	}
	if completionOf(subagent.InReview, recovered) != subagent.Done {
		t.Fatalf("a dismissed finding raised a concern, so every nit would read as a defect")
	}
	if len(stuck) != 2 || stuck[0].Bucket != subagent.ActOn || stuck[1].Bucket != subagent.ActOn {
		t.Fatalf("two failures nothing recovered from are not both to act on: %+v", stuck)
	}
	if completionOf(subagent.InReview, stuck) != subagent.DoneWithConcerns {
		t.Fatal("a failure nothing recovered from did not reach the parent")
	}
	t.Logf("dismissed: %q", recovered[0].Reason)
}

func TestAFinishedChildsCallTextIsTheToolsOwnLabelSoABashArgumentIsKeptWholeAndNeverReachesTheProse(t *testing.T) {
	planted := "sk-ant-oat" + strings.Repeat("0", 16)
	command := `curl -H "Authorization: Bearer ` + planted + `" https://api.example.com`
	if len(secret.CredentialsIn(command)) != 1 {
		t.Fatal("the planted argument is not credential shaped, so this test proves nothing about a credential")
	}

	report := reportOf(
		subagent.SubAgent{ID: "turn-parent-c1", Mission: "call the api", Owns: []string{"mine/**"}},
		[]Row{{ID: "turn-parent-c1", Outcome: OutcomeStopped, Steps: []StepRow{{ToolCalls: []ToolCallRow{
			{Tool: "bash", Command: command},
		}}}}},
		subagent.InReview,
	)

	shapes := make([]string, len(report.Ran))
	for index, one := range report.Ran {
		shapes[index] = one.Tool + " " + strconv.Itoa(len(one.Command)) + " bytes carrying " +
			strings.Join(secret.CredentialsIn(one.Command), ",")
	}
	if len(report.Ran) != 1 || report.Ran[0].Command != command {
		t.Fatalf("a finished bash call does not carry the tool's own label whole: %v", shapes)
	}
	if strings.Contains(report.Text(), planted) {
		t.Fatal("the prose handed to the parent repeats a command line, and it carries counts only")
	}
	t.Logf("the view reads %v, the prose reads %q", shapes, strings.SplitN(report.Text(), "\n", 2)[0])
}

func TestAChildCutOnASilentStepReportsTheLastStepThatSpoke(t *testing.T) {
	agent := subagent.SubAgent{ID: "turn-parent-c1", Mission: "read the note"}
	spokeThenCut := reportOf(agent, []Row{{ID: "turn-parent-c1", Outcome: OutcomeStepCap, Steps: []StepRow{
		{AssistantText: "the note holds one line and it names SPARROW-7731"},
		{ToolCalls: []ToolCallRow{{Tool: "read"}}},
		{ToolCalls: []ToolCallRow{{Tool: "read"}}},
	}}}, subagent.Parked)
	neverSpoke := reportOf(agent, []Row{{ID: "turn-parent-c1", Outcome: OutcomeStepCap, Steps: []StepRow{
		{ToolCalls: []ToolCallRow{{Tool: "read"}}},
	}}}, subagent.Parked)

	if !strings.Contains(spokeThenCut.Prose, "SPARROW-7731") {
		t.Fatalf("a child cut on a silent step lost what it said on step one: %q", spokeThenCut.Prose)
	}
	if spokeThenCut.Steps != 3 || spokeThenCut.ProseStep != 1 {
		t.Fatalf("the report says step %d of %d, and a silent step that ran a tool is not a step never reached",
			spokeThenCut.ProseStep, spokeThenCut.Steps)
	}
	if !strings.Contains(spokeThenCut.Text(), "it last spoke at step 1 of 3") {
		t.Fatalf("the handback quotes the child without saying when it spoke:\n%s", spokeThenCut.Text())
	}
	if neverSpoke.Prose != "" || neverSpoke.ProseStep != 0 {
		t.Fatalf("a child that never spoke reports %q from step %d", neverSpoke.Prose, neverSpoke.ProseStep)
	}
	t.Logf("handback:\n%s", spokeThenCut.Text())
}

func TestEveryOutcomeAndEveryStateIsHandledAndAnUnknownOnePanicsByName(t *testing.T) {
	for _, outcome := range AllOutcomes() {
		finding, carries := outcomeFinding(outcome, subagent.InReview)
		parked, carriedWhileParked := outcomeFinding(outcome, subagent.Parked)
		if !carriedWhileParked || strings.Contains(parked.Reason, "error") || !strings.Contains(parked.Reason, "unfinished") {
			t.Fatalf("outcome %s under a parked child reads %q, and a parked child was stopped rather than broken", outcome, parked.Reason)
		}
		if carries && (finding.Reason == "" || strings.Contains(finding.Reason, "\n")) {
			t.Fatalf("outcome %s gives a finding with no one line reason: %+v", outcome, finding)
		}
	}
	for _, state := range []subagent.State{subagent.Working, subagent.WaitingAnswer, subagent.InReview, subagent.Parked, subagent.Errored, subagent.Finished} {
		if completionOf(state, nil).String() == "" {
			t.Fatalf("state %s has no completion", state)
		}
	}
	if completionOf(subagent.Errored, nil) != subagent.Blocked || completionOf(subagent.Parked, nil) != subagent.Blocked {
		t.Fatal("a child that errored or was parked does not read as blocked")
	}
	if completionOf(subagent.WaitingAnswer, nil) != subagent.NeedsContext {
		t.Fatal("a child waiting on an answer does not read as needs_context")
	}
	for _, c := range []struct {
		what  string
		read  func() string
		wants string
	}{
		{"state", func() string { return completionOf(subagent.State(11), nil).String() }, "unknown sub-agent state 11"},
		{"outcome", func() string { finding, _ := outcomeFinding(Outcome(12), subagent.InReview); return finding.Reason }, "unknown child outcome 12"},
	} {
		func() {
			defer func() {
				raised, isText := recover().(string)
				if !isText || !strings.Contains(raised, c.wants) {
					t.Fatalf("an unknown %s raised %v and has to name the value: %q", c.what, raised, c.wants)
				}
			}()
			_ = c.read()
		}()
	}
}
