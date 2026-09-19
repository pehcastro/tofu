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
	"strconv"
	"strings"
	"testing"

	"boji/internal/konst"
	"boji/internal/llm"
	"boji/internal/turn"
	"boji/internal/turn/tools"
)

const (
	standInExitEnvar    = "BOJI_STAND_IN_EXIT"
	standInMessageEnvar = "BOJI_STAND_IN_MESSAGE"
	realBojiEnvar       = "BOJI_STAND_IN_FORWARDS_TO"
	depthEnvar          = "BOJI_VERB_DEPTH"
	slashSlash          = "/" + "/"
)

func TestMain(m *testing.M) {
	if len(os.Args) > 1 && !strings.HasPrefix(os.Args[1], "-") {
		os.Exit(standInForBoji(os.Args[1:]))
	}
	os.Exit(m.Run())
}

func standInForBoji(args []string) int {
	if real := os.Getenv(realBojiEnvar); real != "" {
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
		Task:           "check this tree with boji's own verbs",
		ResultBytesCap: konst.TurnResultBytesCap,
		ArtifactDir:    t.TempDir(),
	})
	if err != nil {
		t.Fatalf("turn.Run: %v", err)
	}
	return row
}

func bojiName() string {
	if runtime.GOOS == "windows" {
		return "boji.exe"
	}
	return "boji"
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
	if err := os.WriteFile(filepath.Join(stale, bojiName()), body, 0o700); err != nil {
		t.Fatalf("planting a stale boji on PATH: %v", err)
	}
	t.Setenv("PATH", stale+string(os.PathListSeparator)+os.Getenv("PATH"))
	found, lookErr := exec.LookPath("boji")
	if lookErr != nil || filepath.Dir(found) != stale {
		t.Fatalf("the trap is not armed: LookPath gave %q, %v", found, lookErr)
	}

	result, err := verbTool(t, t.TempDir(), "boji_lint_comments").Run(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("running the verb tool: %v", err)
	}
	t.Logf("result: %s", strings.TrimSpace(result.Content))
	if !strings.Contains(result.Content, "stand-in "+filepath.Base(running)) {
		t.Fatalf("the running binary did not answer: %q", result.Content)
	}
	if strings.Contains(result.Content, "stand-in "+bojiName()) {
		t.Fatalf("the stale boji on PATH answered for the running one: %q", result.Content)
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
		{ID: "c1", Name: "boji_lint_comments", Arguments: json.RawMessage(`{"path":"."}`)},
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
	if called[0].Command != "boji lint comments ." {
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
	if _, err := verbTool(t, root, "boji_lint_comments").Run(context.Background(), json.RawMessage(`{}`)); err != nil {
		t.Fatalf("one below the bound has to run: %v", err)
	}

	t.Setenv(depthEnvar, strconv.Itoa(konst.VerbMaxDepth))
	_, err := verbTool(t, root, "boji_lint_comments").Run(context.Background(), json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("the bound let a nested run through")
	}
	t.Logf("refusal: %v", err)
	want := fmt.Sprintf("already nested %d deep and the bound is %d", konst.VerbMaxDepth, konst.VerbMaxDepth)
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("the refusal does not name the depth: %v", err)
	}
}

func TestATurnCallsLintCommentsThroughTheToolAndTheResultNamesARealComment(t *testing.T) {
	module, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatalf("finding the module root: %v", err)
	}
	real := filepath.Join(t.TempDir(), bojiName())
	build := exec.Command("go", "build", "-o", real, "./cmd/boji")
	build.Dir = module
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building boji: %v\n%s", err, out)
	}
	t.Setenv(realBojiEnvar, real)

	root := t.TempDir()
	comment := slashSlash + " the comment boji lint has to find"
	seed(t, root, "scratch/add.go", "package scratch\n\n"+comment+"\nfunc Add(a, b int) int { return a + b }\n")

	model := &recordingModel{calls: []llm.ToolCall{
		{ID: "c1", Name: "boji_lint_comments", Arguments: json.RawMessage(`{"path":"scratch"}`)},
	}}
	called := loggedRows(t, verbTurn(t, root, model))
	if len(called) != 1 {
		t.Fatalf("expected one tool call row, got %d", len(called))
	}
	if called[0].ExitCode == nil || *called[0].ExitCode != 1 {
		t.Fatalf("boji lint comments did not report a finding: %v", called[0].ExitCode)
	}
	results := model.toolResults()
	t.Logf("the model was sent: %s", strings.TrimSpace(results))
	if !strings.Contains(results, comment) {
		t.Fatalf("the result does not name the real comment: %q", results)
	}
}
