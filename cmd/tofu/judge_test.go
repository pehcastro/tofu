package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"tofu/internal/judge/gate"
	"tofu/internal/judge/jev"
	"tofu/internal/judge/ledger"
	"tofu/internal/judge/question"
	"tofu/internal/sys"
)

const denyReply = `{"model":"typesafe/jev-1.13-20260917","provider":"TypeSafe","id":"gen-stub-deny",` +
	`"answers":{` +
	`"risk":{"type":"score","score":3,"probabilities":{"0":0,"1":0,"2":0,"3":1},"confidence":0.9},` +
	`"approval":{"type":"noul","noul":0.5},` +
	`"user_requested":{"type":"noul","noul":0.5},` +
	`"from_untrusted":{"type":"noul","noul":0.9}` +
	`},"usage":{"input_tokens":10,"output_tokens":2,"cost":0.00002}}`

func readShippedFile(t *testing.T, elem ...string) string {
	t.Helper()
	abs, err := filepath.Abs(filepath.Join(append([]string{"..", ".."}, elem...)...))
	if err != nil {
		t.Fatalf("resolving %v: %v", elem, err)
	}
	body, err := os.ReadFile(abs)
	if err != nil {
		t.Fatalf("reading %v: %v", elem, err)
	}
	return string(body)
}

func writeJudgeRuleFixture(t *testing.T, ruleBody, questionsBody, mode string) {
	t.Helper()
	ruleBody = strings.Replace(ruleBody, "mode: shadow", "mode: "+mode, 1)
	ruleDir := filepath.Join("library", "general", "rules")
	if err := os.MkdirAll(ruleDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(ruleDir, "tool_gate@1.yaml"), []byte(ruleBody), 0o644); err != nil {
		t.Fatalf("writing the rule fixture: %v", err)
	}
	questionsDir := filepath.Join("library", "questions")
	if err := os.MkdirAll(questionsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(questionsDir, "tool_gate@1.yaml"), []byte(questionsBody), 0o644); err != nil {
		t.Fatalf("writing the questions fixture: %v", err)
	}
}

func TestJudgeExitCodeFollowsResolvedModeNotVerdictAlone(t *testing.T) {
	dir := t.TempDir()
	ruleBody := readShippedFile(t, "library", "general", "rules", "tool_gate@1.yaml")
	questionsBody := readShippedFile(t, "library", "questions", "tool_gate@1.yaml")
	set, err := resolveLibrary("tool_gate@1", "")
	if err != nil {
		t.Fatalf("resolveLibrary: %v", err)
	}
	t.Chdir(dir)
	writeJudgeRuleFixture(t, ruleBody, questionsBody, "enforced")
	pol, err := resolveRule("tool_gate@1", set)
	if err != nil {
		t.Fatalf("resolveRule: %v", err)
	}
	set.Rule = &pol

	for _, tc := range []struct {
		mode gate.Mode
		want int
	}{
		{gate.ModeEnforced, exitVerdict},
		{gate.ModeShadow, exitOK},
	} {
		set.Mode = tc.mode
		client, err := jev.NewClient(jev.Config{Wire: &stubWire{reply: denyReply}})
		if err != nil {
			t.Fatalf("client: %v", err)
		}
		req := jev.Request{State: map[string]string{"command": "rm -rf /"}, Questions: set.Questions}
		outcome, err := runJudge(context.Background(), client, req, set, true)
		if err != nil {
			t.Fatalf("runJudge (mode %s): %v", tc.mode, err)
		}
		if outcome.verdict != ledger.VerdictDeny {
			t.Fatalf("mode %s: verdict = %q, want deny", tc.mode, outcome.verdict)
		}
		if got := judgeExitCode(outcome); got != tc.want {
			t.Fatalf("mode %s: exit = %d, want %d", tc.mode, got, tc.want)
		}
	}
}

func TestJudgeRowRecordsResolvedModeNotDeclaredMode(t *testing.T) {
	dir := t.TempDir()
	ruleBody := readShippedFile(t, "library", "general", "rules", "tool_gate@1.yaml")
	questionsBody := readShippedFile(t, "library", "questions", "tool_gate@1.yaml")
	set, err := resolveLibrary("tool_gate@1", "")
	if err != nil {
		t.Fatalf("resolveLibrary: %v", err)
	}
	t.Chdir(dir)
	writeJudgeRuleFixture(t, ruleBody, questionsBody, "enforced")
	pol, err := resolveRule("tool_gate@1", set)
	if err != nil {
		t.Fatalf("resolveRule: %v", err)
	}
	if pol.Mode != gate.ModeEnforced {
		t.Fatalf("fixture setup: pol.Mode = %v, want enforced", pol.Mode)
	}
	res, err := resolveRuleMode(pol)
	if err != nil {
		t.Fatalf("resolveRuleMode: %v", err)
	}
	if res.Mode != gate.ModeShadow {
		t.Fatalf("resolution mode = %v, want shadow: no lock file exists for this rule", res.Mode)
	}
	set.Rule = &pol
	set.Mode = res.Mode

	client, err := jev.NewClient(jev.Config{Wire: &stubWire{reply: denyReply}})
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	req := jev.Request{State: map[string]string{"command": "rm -rf /"}, Questions: set.Questions}
	if _, err := runJudge(context.Background(), client, req, set, true); err != nil {
		t.Fatalf("runJudge: %v", err)
	}

	ledgerDir, err := sys.LogDir()
	if err != nil {
		t.Fatalf("sys.LogDir: %v", err)
	}
	var rows []ledger.Row
	if _, err := ledger.NewReader(ledgerDir).Each(ledger.Filter{}, func(row ledger.Row) error {
		rows = append(rows, row)
		return nil
	}); err != nil {
		t.Fatalf("reading the ledger: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %+v", rows)
	}
	if rows[0].Mode() != ledger.ModeShadow {
		t.Fatalf("row mode = %v, want shadow: the file declares enforced but Resolve fell back to shadow, and the row must record what Resolve returned, not what the file declared", rows[0].Mode())
	}
}

func TestJudgeRowCarriesTheSentenceResolveReturned(t *testing.T) {
	dir := t.TempDir()
	ruleBody := readShippedFile(t, "library", "general", "rules", "tool_gate@1.yaml")
	questionsBody := readShippedFile(t, "library", "questions", "tool_gate@1.yaml")
	set, err := resolveLibrary("tool_gate@1", "")
	if err != nil {
		t.Fatalf("resolveLibrary: %v", err)
	}
	t.Chdir(dir)
	writeJudgeRuleFixture(t, ruleBody, questionsBody, "enforced")
	pol, err := resolveRule("tool_gate@1", set)
	if err != nil {
		t.Fatalf("resolveRule: %v", err)
	}
	res, err := resolveRuleMode(pol)
	if err != nil {
		t.Fatalf("resolveRuleMode: %v", err)
	}
	if res.Reason == "" {
		t.Fatal("fixture setup: Resolve returned no sentence to compare against")
	}
	set.Rule = &pol
	set.Mode = res.Mode
	set.ModeReason = res.Reason

	client, err := jev.NewClient(jev.Config{Wire: &stubWire{reply: denyReply}})
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	req := jev.Request{State: map[string]string{"command": "rm -rf /"}, Questions: set.Questions}
	if _, err := runJudge(context.Background(), client, req, set, true); err != nil {
		t.Fatalf("runJudge: %v", err)
	}

	ledgerDir, err := sys.LogDir()
	if err != nil {
		t.Fatalf("sys.LogDir: %v", err)
	}
	var rows []ledger.Row
	if _, err := ledger.NewReader(ledgerDir).Each(ledger.Filter{}, func(row ledger.Row) error {
		rows = append(rows, row)
		return nil
	}); err != nil {
		t.Fatalf("reading the ledger: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %+v", rows)
	}
	if rows[0].Reason == nil || rows[0].Reason.ModeReason == nil {
		t.Fatalf("row carries no mode reason: %+v", rows[0].Reason)
	}
	if *rows[0].Reason.ModeReason != res.Reason {
		t.Fatalf("row sentence = %q, want the sentence Resolve returned directly: %q", *rows[0].Reason.ModeReason, res.Reason)
	}
}

func TestJudgeRowCarriesAnEmptyTurnIDRatherThanAMissingField(t *testing.T) {
	t.Chdir(t.TempDir())
	wire := &stubWire{reply: `{"model":"typesafe/jev-1.13-20260917","answers":{"approval":{"type":"noul","noul":0.11}},"usage":{"input_tokens":10,"output_tokens":2,"cost":0.00002},"id":"gen-stub-1","provider":"TypeSafe"}`}
	client, err := jev.NewClient(jev.Config{Wire: wire})
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	set := battery{SetName: "stub_battery", QuestionsVersion: 1, Kinds: map[string]question.Kind{"approval": question.KindNoul}}
	req := jev.Request{
		State:     map[string]string{"command": "ls -la"},
		Questions: []jev.Question{{ID: "approval", Kind: jev.QuestionNoul, Instructions: "?", True: "t", False: "f"}},
	}
	if _, err := runJudge(context.Background(), client, req, set, true); err != nil {
		t.Fatalf("runJudge: %v", err)
	}

	dir, err := sys.LogDir()
	if err != nil {
		t.Fatalf("sys.LogDir: %v", err)
	}
	var rows []ledger.Row
	if _, err := ledger.NewReader(dir).Each(ledger.Filter{}, func(row ledger.Row) error {
		rows = append(rows, row)
		return nil
	}); err != nil {
		t.Fatalf("reading the ledger: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %+v", rows)
	}
	if rows[0].TurnID != "" {
		t.Fatalf("row.TurnID = %q, want empty: this row was not made inside a turn", rows[0].TurnID)
	}
}

func TestJudgeRejectsMalformedJSON(t *testing.T) {
	var out, errOut bytes.Buffer
	code := judgeVerb(nil, strings.NewReader("not json"), &out, &errOut)
	if code != exitUsage {
		t.Fatalf("exit = %d, want %d", code, exitUsage)
	}
	if out.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", out.String())
	}
	if errOut.Len() == 0 {
		t.Fatal("stderr is empty, want a message")
	}
}

func TestJudgeRejectsMissingState(t *testing.T) {
	var out, errOut bytes.Buffer
	code := judgeVerb(nil, strings.NewReader(`{"library":"tool_gate@1"}`), &out, &errOut)
	if code != exitUsage {
		t.Fatalf("exit = %d, want %d", code, exitUsage)
	}
	if out.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", out.String())
	}
}

func TestJudgeDryRunNeedsNoKey(t *testing.T) {
	t.Setenv("OPENROUTER_KEY", "")
	t.Chdir(t.TempDir())
	body := `{"state":{"tool":"bash","input":{"command":"ls -la"}},"library":"tool_gate@1"}`
	var out, errOut bytes.Buffer
	code := judgeVerb([]string{"--dry-run"}, strings.NewReader(body), &out, &errOut)
	if code != exitOK {
		t.Fatalf("exit = %d, want %d, stderr %s", code, exitOK, errOut.String())
	}
	if !strings.Contains(out.String(), `"risk"`) {
		t.Fatalf("dry run body missing a question, got %s", out.String())
	}
	if !strings.Contains(out.String(), `"model":"~typesafe/jev-latest"`) {
		t.Fatalf("dry run body missing the alias, got %s", out.String())
	}
}

func TestJudgeExitsTwoWhenTheKeyIsMissing(t *testing.T) {
	t.Setenv("OPENROUTER_KEY", "")
	t.Chdir(t.TempDir())
	body := `{"state":{"tool":"bash","input":{"command":"ls -la"}},"library":"tool_gate@1"}`
	var out, errOut bytes.Buffer
	code := judgeVerb(nil, strings.NewReader(body), &out, &errOut)
	if code != exitUsage {
		t.Fatalf("exit = %d, want %d", code, exitUsage)
	}
	if out.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", out.String())
	}
	if !strings.Contains(errOut.String(), "OPENROUTER_KEY") {
		t.Fatalf("stderr does not name the variable: %s", errOut.String())
	}
}

func TestJudgeLintFindsNothingOnTheShippedSet(t *testing.T) {
	var out, errOut bytes.Buffer
	fixture := filepath.Join("..", "..", "library", "questions", "tool_gate@1.yaml")
	code := judgeVerb([]string{"--lint", fixture}, nil, &out, &errOut)
	if code != exitOK {
		t.Fatalf("exit = %d, want %d, stdout %s stderr %s", code, exitOK, out.String(), errOut.String())
	}
	if out.Len() != 0 {
		t.Fatalf("expected no findings, got %s", out.String())
	}
}

func TestJudgeLintReportsAFindingWithExitVerdict(t *testing.T) {
	dir := t.TempDir()
	broken := filepath.Join(dir, "broken@1.yaml")
	body := "name: broken\ndomain: general\nquestions_version: 1\nstate:\n  - tool\nquestions:\n  pick:\n    type: choice\n    instructions: choose one\n    options:\n      - a\n      - b\n"
	if err := os.WriteFile(broken, []byte(body), 0o644); err != nil {
		t.Fatalf("writing the fixture: %v", err)
	}
	var out, errOut bytes.Buffer
	code := judgeVerb([]string{"--lint", broken}, nil, &out, &errOut)
	if code != exitVerdict {
		t.Fatalf("exit = %d, want %d", code, exitVerdict)
	}
	if !strings.Contains(out.String(), string(question.RuleNoEscape)) {
		t.Fatalf("expected the no-escape finding, got %s", out.String())
	}
}

func chdirRepoRoot(t *testing.T) {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	t.Chdir(root)
}

func TestJudgeRuleBareNameIsRefused(t *testing.T) {
	t.Setenv("OPENROUTER_KEY", "")
	chdirRepoRoot(t)
	body := `{"state":{"tool":"bash","input":{"command":"ls -la"}},"library":"tool_gate@1","rule":"tool_gate"}`
	var out, errOut bytes.Buffer
	code := judgeVerb(nil, strings.NewReader(body), &out, &errOut)
	if code != exitUsage {
		t.Fatalf("exit = %d, want %d, stdout %s", code, exitUsage, out.String())
	}
	if !strings.Contains(errOut.String(), "names no version") {
		t.Fatalf("error does not explain the missing version: %s", errOut.String())
	}
}

func TestJudgeRuleVersionMismatchIsRefusedBeforeAnyCall(t *testing.T) {
	t.Setenv("OPENROUTER_KEY", "")
	chdirRepoRoot(t)
	body := `{"state":{"tool":"bash","input":{"command":"ls -la"}},"library":"tool_gate@2","rule":"tool_gate@1"}`
	var out, errOut bytes.Buffer
	code := judgeVerb(nil, strings.NewReader(body), &out, &errOut)
	if code != exitUsage {
		t.Fatalf("exit = %d, want %d, stdout %s", code, exitUsage, out.String())
	}
	if out.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", out.String())
	}
	if strings.Contains(errOut.String(), "OPENROUTER_KEY") {
		t.Fatalf("failed on the missing key, not the rule mismatch, so the refusal did not happen before the call: %s", errOut.String())
	}
	if !strings.Contains(errOut.String(), "tool_gate@1") || !strings.Contains(errOut.String(), "tool_gate@2") {
		t.Fatalf("error does not name both versions: %s", errOut.String())
	}
}

func TestNoRuleNamedGetsNoVerdict(t *testing.T) {
	t.Chdir(t.TempDir())
	wire := &stubWire{reply: `{"model":"typesafe/jev-1.13-20260917","answers":{"approval":{"type":"noul","noul":0.11}},"usage":{"input_tokens":10,"output_tokens":2,"cost":0.00002},"id":"gen-stub-1","provider":"TypeSafe"}`}
	client, err := jev.NewClient(jev.Config{Wire: wire})
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	set := battery{SetName: "stub_battery", QuestionsVersion: 1, Kinds: map[string]question.Kind{"approval": question.KindNoul}}
	req := jev.Request{
		State:     map[string]string{"command": "ls -la"},
		Questions: []jev.Question{{ID: "approval", Kind: jev.QuestionNoul, Instructions: "?", True: "t", False: "f"}},
	}
	outcome, err := runJudge(context.Background(), client, req, set, false)
	if err != nil {
		t.Fatalf("runJudge: %v", err)
	}
	if outcome.verdict != ledger.VerdictUnset {
		t.Fatalf("verdict = %q, want unset since no rule was named", outcome.verdict)
	}
	dir, err := sys.LogDir()
	if err != nil {
		t.Fatalf("sys.LogDir: %v", err)
	}
	var rows []ledger.Row
	if _, err := ledger.NewReader(dir).Each(ledger.Filter{}, func(row ledger.Row) error {
		rows = append(rows, row)
		return nil
	}); err != nil {
		t.Fatalf("reading the ledger: %v", err)
	}
	if len(rows) != 1 || rows[0].Verdict != ledger.VerdictUnset {
		t.Fatalf("rows = %+v", rows)
	}
}

func TestLibraryResolveRefusesABareNameWithTwoVersions(t *testing.T) {
	var out, errOut bytes.Buffer
	code := libraryVerb([]string{"resolve", "tool_gate"}, &out, &errOut)
	if code != exitUsage {
		t.Fatalf("exit = %d, want %d, stdout %s", code, exitUsage, out.String())
	}
	if !strings.Contains(errOut.String(), "tool_gate@1") || !strings.Contains(errOut.String(), "tool_gate@2") {
		t.Fatalf("error does not name both versions: %s", errOut.String())
	}
}

func TestLibraryResolvePrintsEveryFieldAndItsOrigin(t *testing.T) {
	var out, errOut bytes.Buffer
	code := libraryVerb([]string{"resolve", "tool_gate@1"}, &out, &errOut)
	if code != exitOK {
		t.Fatalf("exit = %d, want %d, stderr %s", code, exitOK, errOut.String())
	}
	if !strings.Contains(out.String(), "tool_gate@1.yaml") {
		t.Fatalf("expected the origin file in the output, got %s", out.String())
	}
	if !strings.Contains(out.String(), "questions.risk.instructions") {
		t.Fatalf("expected a nested field path, got %s", out.String())
	}
}
