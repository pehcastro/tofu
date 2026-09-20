package tools_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"

	"tofu/internal/konst"
	"tofu/internal/llm"
	"tofu/internal/turn"
	"tofu/internal/turn/tools"
)

const (
	standInExitEnvar    = "TOFU_STAND_IN_EXIT"
	standInMessageEnvar = "TOFU_STAND_IN_MESSAGE"
	realTofuEnvar       = "TOFU_STAND_IN_FORWARDS_TO"
	depthEnvar          = "TOFU_VERB_DEPTH"
	slashSlash          = "/" + "/"
	rulesInTheBinary    = "the binary"
	rulesInTheProject   = "the project"
)

func TestMain(m *testing.M) {
	if len(os.Args) > 1 && !strings.HasPrefix(os.Args[1], "-") {
		os.Exit(standInForTofu(os.Args[1:]))
	}
	os.Exit(m.Run())
}

func standInForTofu(args []string) int {
	if real := os.Getenv(realTofuEnvar); real != "" {
		forwarded := exec.Command(real, args...)
		forwarded.Stdin, forwarded.Stdout, forwarded.Stderr = os.Stdin, os.Stdout, os.Stderr
		_ = forwarded.Run()
		if forwarded.ProcessState == nil {
			return 3
		}
		return forwarded.ProcessState.ExitCode()
	}
	body, _ := io.ReadAll(os.Stdin)
	running, _ := os.Executable()
	fmt.Printf("stand-in %s ran %q depth %s stdin %q\n",
		filepath.Base(running), strings.Join(args, " "), os.Getenv(depthEnvar), body)
	fmt.Fprint(os.Stderr, os.Getenv(standInMessageEnvar))
	code, _ := strconv.Atoi(os.Getenv(standInExitEnvar))
	return code
}

type recordingModel struct {
	calls []llm.ToolCall
	step  int
	seen  []llm.Message
}

func (m *recordingModel) Ask(_ context.Context, request llm.Request) (llm.Decision, error) {
	m.seen = request.Messages
	if m.step >= len(m.calls) {
		return llm.Decision{Build: "recording", Outcome: llm.OutcomeMessage, Content: "done"}, nil
	}
	call := m.calls[m.step]
	m.step++
	return llm.Decision{Build: "recording", Outcome: llm.OutcomeToolCalls, ToolCalls: []llm.ToolCall{call}}, nil
}

func (m *recordingModel) toolResults() string {
	var results []string
	for _, message := range m.seen {
		if message.Role == llm.RoleTool {
			results = append(results, message.Content)
		}
	}
	return strings.Join(results, "\n")
}

func verbTool(t *testing.T, root, name string) turn.Tool {
	t.Helper()
	verbs, err := tools.NewVerbs(root)
	if err != nil {
		t.Fatalf("building the verb tools: %v", err)
	}
	for _, verb := range verbs {
		if verb.Name() == name {
			return verb
		}
	}
	t.Fatalf("no verb tool is named %s", name)
	return nil
}

func verbTurn(t *testing.T, root string, model *recordingModel) turn.Row {
	t.Helper()
	verbs, err := tools.NewVerbs(root)
	if err != nil {
		t.Fatalf("building the verb tools: %v", err)
	}
	row, err := turn.Run(context.Background(), turn.Config{
		Model:          model,
		Spend:          turn.SpendSubscription,
		Tools:          turn.NewRegistry(verbs...),
		Task:           "check this tree with tofu's own verbs",
		ResultBytesCap: konst.TurnResultBytesCap,
		ArtifactDir:    t.TempDir(),
	})
	if err != nil {
		t.Fatalf("turn.Run: %v", err)
	}
	return row
}

func tofuName() string {
	if runtime.GOOS == "windows" {
		return "tofu.exe"
	}
	return "tofu"
}

func TestTheVerbToolRunsTheBinaryThatIsRunningAndNotTheOneOnPATH(t *testing.T) {
	running, err := os.Executable()
	if err != nil {
		t.Fatalf("finding the running test binary: %v", err)
	}
	body, err := os.ReadFile(running)
	if err != nil {
		t.Fatalf("reading the running test binary: %v", err)
	}
	stale := t.TempDir()
	if err := os.WriteFile(filepath.Join(stale, tofuName()), body, 0o700); err != nil {
		t.Fatalf("planting a stale tofu on PATH: %v", err)
	}
	t.Setenv("PATH", stale+string(os.PathListSeparator)+os.Getenv("PATH"))
	found, lookErr := exec.LookPath("tofu")
	if lookErr != nil || filepath.Dir(found) != stale {
		t.Fatalf("the trap is not armed: LookPath gave %q, %v", found, lookErr)
	}

	result, err := verbTool(t, t.TempDir(), "tofu_lint_comments").Run(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("running the verb tool: %v", err)
	}
	t.Logf("result: %s", strings.TrimSpace(result.Content))
	if !strings.Contains(result.Content, "stand-in "+filepath.Base(running)) {
		t.Fatalf("the running binary did not answer: %q", result.Content)
	}
	if strings.Contains(result.Content, "stand-in "+tofuName()) {
		t.Fatalf("the stale tofu on PATH answered for the running one: %q", result.Content)
	}
	if !strings.Contains(result.Content, "depth 1") {
		t.Fatalf("the child was not told its nesting depth: %q", result.Content)
	}
}

func TestAVerbNonzeroExitReachesTheModelAsAFailedResultCarryingItsOwnMessage(t *testing.T) {
	message := "store.go:7:1: " + slashSlash + " the comment the verb found"
	t.Setenv(standInExitEnvar, "1")
	t.Setenv(standInMessageEnvar, message+"\n")

	model := &recordingModel{calls: []llm.ToolCall{
		{ID: "c1", Name: "tofu_lint_comments", Arguments: json.RawMessage(`{"path":"."}`)},
	}}
	row := verbTurn(t, t.TempDir(), model)
	called := loggedRows(t, row)
	if len(called) != 1 {
		t.Fatalf("expected one tool call row, got %d", len(called))
	}
	if called[0].Error != "" {
		t.Fatalf("a verb verdict became a harness error: %q", called[0].Error)
	}
	if called[0].ExitCode == nil || *called[0].ExitCode != 1 {
		t.Fatalf("the exit code did not reach the row: %v", called[0].ExitCode)
	}
	if called[0].Command != "tofu lint comments ." {
		t.Fatalf("the row does not name the verb that ran: %q", called[0].Command)
	}
	results := model.toolResults()
	t.Logf("the model was sent: %s", strings.TrimSpace(results))
	if !strings.Contains(results, message) {
		t.Fatalf("the verb's own message did not reach the model: %q", results)
	}
}

func TestTheRecursionBoundRefusesANestedRunAtTheLimit(t *testing.T) {
	root := t.TempDir()
	t.Setenv(depthEnvar, strconv.Itoa(konst.VerbMaxDepth-1))
	if _, err := verbTool(t, root, "tofu_lint_comments").Run(context.Background(), json.RawMessage(`{}`)); err != nil {
		t.Fatalf("one below the bound has to run: %v", err)
	}

	t.Setenv(depthEnvar, strconv.Itoa(konst.VerbMaxDepth))
	_, err := verbTool(t, root, "tofu_lint_comments").Run(context.Background(), json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("the bound let a nested run through")
	}
	t.Logf("refusal: %v", err)
	want := fmt.Sprintf("already nested %d deep and the bound is %d", konst.VerbMaxDepth, konst.VerbMaxDepth)
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("the refusal does not name the depth: %v", err)
	}
}

func standInForwardsToARealTofu(t *testing.T) {
	t.Helper()
	module, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatalf("finding the module root: %v", err)
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	real := filepath.Join(t.TempDir(), tofuName())
	build := exec.Command("go", "build", "-o", real, "./cmd/tofu")
	build.Dir = module
	if out, err := build.CombinedOutput(); err != nil {
		t.Skipf("this test runs the real tofu and the tree does not build it, which is not a finding about this package: %v\n%s", err, out)
	}
	t.Setenv(realTofuEnvar, real)
}

func TestATurnCallsRulesCheckThroughTheToolInATreeThatIsNotThisRepository(t *testing.T) {
	standInForwardsToARealTofu(t)
	root := t.TempDir()
	seed(t, root, "scratch/notes.md", "the line that breaks the rule"+string(rune(0x2014))+"on purpose\n")

	model := &recordingModel{calls: []llm.ToolCall{
		{ID: "c1", Name: "tofu_rules_check", Arguments: json.RawMessage(`{"path":"scratch"}`)},
	}}
	called := loggedRows(t, verbTurn(t, root, model))
	if len(called) != 1 {
		t.Fatalf("expected one tool call row, got %d", len(called))
	}
	if called[0].ExitCode == nil || *called[0].ExitCode != 0 {
		t.Fatalf("tofu rules check did not reach a verdict: %v", called[0].ExitCode)
	}
	results := model.toolResults()
	t.Logf("the model was sent: %s", strings.TrimSpace(results))
	if !strings.Contains(results, rulesInTheBinary) {
		t.Fatalf("the result does not name the rule set that ran: %q", results)
	}
	if strings.Contains(results, rulesInTheProject) {
		t.Fatalf("a tree with no catalog was told the project's rules ran: %q", results)
	}
	if !strings.Contains(results, "em_dash") {
		t.Fatalf("the shipped em dash rule did not fire: %q", results)
	}
}

func TestATurnCallsJudgeThroughTheToolAndTheMissingKeyIsAStatedRequirement(t *testing.T) {
	standInForwardsToARealTofu(t)
	t.Setenv("OPENROUTER_KEY", "")
	root := t.TempDir()

	model := &recordingModel{calls: []llm.ToolCall{
		{ID: "c1", Name: "tofu_judge", Arguments: json.RawMessage(`{"state":"the tests pass and the task is done","battery":"stop_check@1"}`)},
	}}
	called := loggedRows(t, verbTurn(t, root, model))
	if len(called) != 1 {
		t.Fatalf("expected one tool call row, got %d", len(called))
	}
	if called[0].ExitCode == nil || *called[0].ExitCode != 2 {
		t.Fatalf("tofu judge without a key did not exit 2: %v", called[0].ExitCode)
	}
	results := model.toolResults()
	t.Logf("the model was sent: %s", strings.TrimSpace(results))
	if !strings.Contains(results, "OPENROUTER_KEY") {
		t.Fatalf("the result does not name what the verb needs: %q", results)
	}
	if strings.Contains(results, "unmarshal") {
		t.Fatalf("the request shape is still wrong: %q", results)
	}
}

func TestTheJudgeVerbSendsTheRequestShapeTheVerbReads(t *testing.T) {
	result, err := verbTool(t, t.TempDir(), "tofu_judge").Run(context.Background(),
		json.RawMessage(`{"state":"49 pass 0 fail","battery":"stop_check@1"}`))
	if err != nil {
		t.Fatalf("running the judge verb: %v", err)
	}
	t.Logf("result: %s", strings.TrimSpace(result.Content))
	if !strings.Contains(result.Content, `stdin "{\"state\":\"49 pass 0 fail\",\"catalog\":\"stop_check@1\"}"`) {
		t.Fatalf("the verb did not send the shape tofu judge reads: %q", result.Content)
	}
}

func TestTheJudgeVerbRefusesTheCallTheModelActuallyMade(t *testing.T) {
	recorded := json.RawMessage(`{"body":"{\"state\":\"Task API changed in place: done renamed to completed\",\"questions\":[\"Does the work satisfy every requirement of the task?\",\"Was the existing code changed in place rather than rewritten?\"]}"}`)
	_, err := verbTool(t, t.TempDir(), "tofu_judge").Run(context.Background(), recorded)
	if err == nil {
		t.Fatal("the recorded call was accepted, so the model learns nothing")
	}
	t.Logf("refusal: %v", err)
	for _, want := range []string{"body", "state", "battery"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the refusal does not name %q, so it does not name the shape it wants: %v", want, err)
		}
	}
}

func TestEveryVerbToolSaysWhatItNeedsToRun(t *testing.T) {
	verbs, err := tools.NewVerbs(t.TempDir())
	if err != nil {
		t.Fatalf("building the verb tools: %v", err)
	}
	if len(verbs) != 5 {
		t.Fatalf("expected five verb tools, got %d", len(verbs))
	}
	for _, verb := range verbs {
		description := verb.Definition().Description
		t.Logf("%s: %s", verb.Name(), description)
		if !strings.Contains(description, "it needs") {
			t.Fatalf("%s does not say what it needs to run: %q", verb.Name(), description)
		}
	}
}

func TestATurnCallsLintCommentsThroughTheToolAndTheResultNamesARealComment(t *testing.T) {
	standInForwardsToARealTofu(t)

	root := t.TempDir()
	comment := slashSlash + " the comment tofu lint has to find"
	seed(t, root, "scratch/add.go", "package scratch\n\n"+comment+"\nfunc Add(a, b int) int { return a + b }\n")

	model := &recordingModel{calls: []llm.ToolCall{
		{ID: "c1", Name: "tofu_lint_comments", Arguments: json.RawMessage(`{"path":"scratch"}`)},
	}}
	called := loggedRows(t, verbTurn(t, root, model))
	if len(called) != 1 {
		t.Fatalf("expected one tool call row, got %d", len(called))
	}
	if called[0].ExitCode == nil || *called[0].ExitCode != 1 {
		t.Fatalf("tofu lint comments did not report a finding: %v", called[0].ExitCode)
	}
	results := model.toolResults()
	t.Logf("the model was sent: %s", strings.TrimSpace(results))
	if !strings.Contains(results, comment) {
		t.Fatalf("the result does not name the real comment: %q", results)
	}
}

func TestEveryVerbGivesTheModelATofuName(t *testing.T) {
	verbs, err := tools.NewVerbs(t.TempDir())
	if err != nil {
		t.Fatalf("building the verb tools: %v", err)
	}
	given := make([]string, len(verbs))
	for i, verb := range verbs {
		given[i] = verb.Definition().Name
	}
	t.Logf("the model is given: %v", given)

	wanted := []string{"tofu_lint_comments", "tofu_rules_check", "tofu_judge", "tofu_why", "tofu_replay"}
	if len(given) != len(wanted) {
		t.Fatalf("the model is given %d verb tools, want exactly %d: %v", len(given), len(wanted), given)
	}
	for _, want := range wanted {
		t.Run(want, func(t *testing.T) {
			if !slices.Contains(given, want) {
				t.Fatalf("no verb gives the model the name %s, it gives %v", want, given)
			}
		})
	}
}
