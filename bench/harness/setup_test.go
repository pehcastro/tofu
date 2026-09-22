package harness

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func twoSetups(t *testing.T) (stock, bare Plan) {
	t.Helper()
	root := repositoryRoot(t)
	stock, err := BuildPlan(root, ArmClaude, "hono", 1)
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	bare = stock
	if bare.Setup, err = SetupOf("no-rules", "", stock.Prompt); err != nil {
		t.Fatalf("SetupOf: %v", err)
	}
	return stock, bare
}

func TestTheSameTaskUnderTwoNamedSetupsIsToldApartOnTheRowsReadBack(t *testing.T) {
	transcript, err := os.ReadFile(filepath.Join("testdata", "claude-p1.json"))
	if err != nil {
		t.Fatalf("read the recorded claude transcript: %v", err)
	}
	stock, bare := twoSetups(t)

	var read []Row
	for i, plan := range []Plan{stock, bare} {
		row, _, err := ParseClaude(transcript, ClaudeMeta{
			Arm: plan.Arm, Task: plan.Task, Version: plan.Version, Run: i + 1, Setup: plan.Setup,
		})
		if err != nil {
			t.Fatalf("%s: ParseClaude: %v", plan.Setup.Name, err)
		}
		encoded, err := json.Marshal(row)
		if err != nil {
			t.Fatalf("%s: marshal: %v", plan.Setup.Name, err)
		}
		var back Row
		if err := json.Unmarshal(encoded, &back); err != nil {
			t.Fatalf("%s: unmarshal: %v", plan.Setup.Name, err)
		}
		read = append(read, back)
	}

	if read[0].Setup.Name != stockSetupName || read[1].Setup.Name != "no-rules" {
		t.Fatalf("the two rows name setups %q and %q, want %q and no-rules", read[0].Setup.Name, read[1].Setup.Name, stockSetupName)
	}
	if len(read[0].Setup.RuleFiles) == 0 {
		t.Fatal("the stock row recorded no rule files, so its name stands for nothing and the run cannot be reproduced")
	}
	if len(read[1].Setup.RuleFiles) != 0 {
		t.Fatalf("the no-rules row recorded %d rule files: %v", len(read[1].Setup.RuleFiles), read[1].Setup.RuleFiles)
	}
	if read[0].Setup.PromptBytes != len(stock.Prompt) || read[0].Setup.PromptSHA256 != read[1].Setup.PromptSHA256 {
		t.Fatalf("one task sent two different prompts: %d bytes %s against %d bytes %s",
			read[0].Setup.PromptBytes, read[0].Setup.PromptSHA256, read[1].Setup.PromptBytes, read[1].Setup.PromptSHA256)
	}
}

func TestSetupOfRefusesANameWithNothingBehindIt(t *testing.T) {
	if _, err := SetupOf("", "", "prompt"); err == nil {
		t.Fatal("SetupOf accepted an unnamed setup, which cannot be read back off a row")
	}
	if _, err := SetupOf("ghost", filepath.Join(t.TempDir(), "absent"), "prompt"); err == nil {
		t.Fatal("SetupOf accepted a rule directory that does not exist, so the name would stand for nothing")
	}
}

func TestReportGroupsBySetupAndQuotesTheWrittenRule(t *testing.T) {
	stock, bare := twoSetups(t)
	rows := []Row{
		rowUnder(stock.Setup, 1, gatesOK()),
		rowUnder(stock.Setup, 2, gatesOK()),
		rowUnder(stock.Setup, 3, gatesOK()),
		rowUnder(bare.Setup, 1, gatesOK()),
		rowUnder(bare.Setup, 2, gatesFailing("build")),
		rowUnder(bare.Setup, 3, gatesFailing("build")),
	}

	out := Render(rows)
	for _, want := range []string{
		"SETUPS hono-routes",
		"stock: always_passes, 3 of 3 runs passed.",
		"no-rules: usually_fails, 1 of 3 runs passed. 0 rule files",
		"WHAT A PASSING EVAL ENTITLES",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("the rendered report is missing %q:\n%s", want, out)
		}
	}
	if !strings.Contains(out, entitlement) {
		t.Fatalf("the report does not quote the written rule whole:\n%s", out)
	}
}

func TestARowWithNoSetupSaysSoRatherThanRenderingAsOne(t *testing.T) {
	out := Render([]Row{rowUnder(Setup{}, 1, gatesOK())})
	if !strings.Contains(out, "unrecorded: always_passes, 1 of 1 runs passed. no setup was recorded beside this row") {
		t.Fatalf("a row carrying no setup rendered as though it had one:\n%s", out)
	}
}
