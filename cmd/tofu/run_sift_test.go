package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"

	"tofu/internal/judge/ledger"
	"tofu/internal/judge/method"
	"tofu/internal/llm"
	"tofu/internal/sift"
	"tofu/internal/sys"
	"tofu/internal/turn"
	shipped "tofu/library"
)

const recordedJudgedReplyCostUSD = 0.0000306

const siftMedianSessionCeilingUSD = 0.000614

func recordedJudgedNouls() []float64 { return []float64{0.91, 0.62, 0.44, 0.18, 0.07, 0.76} }

var chunkPosition = regexp.MustCompile(`chunk (\d+) of`)

func judgedReply(index int, noul float64) string {
	return fmt.Sprintf(`{"model":"typesafe/jev-1.13-20260917","provider":"TypeSafe","id":"gen-recorded-%04d",`+
		`"answers":{"still_needed":{"type":"noul","noul":%g}},`+
		`"usage":{"input_tokens":1042,"output_tokens":12,"cost":%g}}`,
		index+1, noul, recordedJudgedReplyCostUSD)
}

func stubRecordedJudgedReplies(t *testing.T) {
	t.Helper()
	bodies := make([]string, 0, len(recordedJudgedNouls()))
	for i, noul := range recordedJudgedNouls() {
		bodies = append(bodies, judgedReply(i, noul))
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("reading the request the scorer sent: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, bodies[chunkAsked(t, string(asked))%len(bodies)])
	}))
	t.Cleanup(server.Close)
	t.Setenv(judgeEndpointEnvar, server.URL)
	t.Setenv("OPENROUTER_KEY", "stub-key-not-a-real-credential")
}

func chunkAsked(t *testing.T, request string) int {
	t.Helper()
	found := chunkPosition.FindStringSubmatch(request)
	if found == nil {
		t.Errorf("the request names no chunk position, so no recorded reply fits it: %.200s", request)
		return 0
	}
	at, err := strconv.Atoi(found[1])
	if err != nil {
		t.Errorf("the chunk position %q is not a number", found[1])
	}
	return at
}

func assumesTheRuleIsOutOfShadow(t *testing.T) {
	t.Helper()
	rule, err := sift.LoadShellRule(shipped.Files(), shellSiftPoint)
	if err != nil {
		t.Fatalf("sift.LoadShellRule: %v", err)
	}
	if rule.Mode == sift.ModeShadow {
		t.Skipf("skipped, and counted: %s declares mode %s against the %s the table names, so the run attaches no sieve and cuts nothing",
			rule.File, rule.Mode, method.Judged)
	}
}

func siftableOutput() string {
	var out strings.Builder
	for chunk := range 6 {
		for line := range 8 {
			fmt.Fprintf(&out, "chunk %d line %d: %s\n", chunk, line, strings.Repeat("filler ", 9))
		}
		out.WriteString("\n")
	}
	return out.String()
}

func siftedShell() sift.Shell {
	return sift.Shell{Command: "go test ./internal/turn/...", Stdout: siftableOutput()}
}

func TestTheRunAttachesAScorerBuiltOverTheJevClient(t *testing.T) {
	assumesTheRuleIsOutOfShadow(t)
	chdirTemp(t)
	stubRecordedJudgedReplies(t)

	sifter, scorer, err := buildShellSift(siftFollowsTheTable)
	if err != nil {
		t.Fatalf("buildShellSift: %v", err)
	}
	if sifter == nil || scorer == nil {
		t.Fatalf("the table names %s for %s and the run built sifter %v scorer %v",
			method.Judged, sift.ShellSchema, sifter, scorer)
	}
	if sifter.Scores != turn.ShellScores(scorer) {
		t.Fatalf("the sieve scores through %#v, not the scorer the run built", sifter.Scores)
	}
	if scorer.client == nil || scorer.set.SetName != sift.ShellSchema {
		t.Fatalf("the scorer carries client %v and the question set %q", scorer.client, scorer.set.SetName)
	}
	opts := armOpts(t)
	config, _ := mustConfig(t, opts, nil, runtime{spend: turn.SpendSubscription, sift: sifter, scorer: scorer})
	if config.Sift != sifter {
		t.Fatalf("turn.Config carries %v, not the sieve the run built", config.Sift)
	}
	if scorer.turnID == "" {
		t.Fatal("the scorer writes ledger rows against no turn")
	}
	t.Logf("config.Sift keeps at %.2f, scores through the jev client, and rows say turn %s", config.Sift.KeepAt, scorer.turnID)
}

type bashingModel struct {
	command string
	asked   int
}

func (m *bashingModel) Ask(context.Context, llm.Request) (llm.Decision, error) {
	m.asked++
	if m.asked == 1 {
		return llm.Decision{Build: "stub-model", Outcome: llm.OutcomeToolCalls, ToolCalls: []llm.ToolCall{
			{ID: "call-1", Name: "bash", Arguments: json.RawMessage(`{"command":` + strconv.Quote(m.command) + `}`)},
		}}, nil
	}
	return llm.Decision{Build: "stub-model", Outcome: llm.OutcomeMessage, Content: "read it"}, nil
}

func TestABashResultReachesTheModelSmallerThanTheOutputItRan(t *testing.T) {
	assumesTheRuleIsOutOfShadow(t)
	dir := chdirTemp(t)
	stubRecordedJudgedReplies(t)

	path := filepath.Join(dir, "recorded-output.txt")
	if err := os.WriteFile(path, []byte(siftableOutput()), 0o600); err != nil {
		t.Fatal(err)
	}

	opts := armOpts(t)
	opts.dir, opts.task = dir, "find the line that says what failed"
	built, _, err := buildRunTools(dir, opts.toolSet)
	if err != nil {
		t.Fatalf("buildRunTools: %v", err)
	}
	sifter, scorer, err := buildShellSift(siftFollowsTheTable)
	if err != nil {
		t.Fatalf("buildShellSift: %v", err)
	}
	model := &bashingModel{command: "cat " + filepath.ToSlash(path)}
	config, _ := mustConfig(t, opts, built, runtime{model: model, spend: turn.SpendSubscription, sift: sifter, scorer: scorer})

	row, err := turn.Run(context.Background(), config)
	if err != nil {
		t.Fatalf("turn.Run: %v", err)
	}
	var messages []string
	for _, message := range row.Conversation {
		if message.Role == llm.RoleTool {
			messages = append(messages, message.Content)
		}
	}
	if len(messages) != 1 || len(row.Steps) == 0 || len(row.Steps[0].ToolCalls) != 1 {
		t.Fatalf("the turn ran %d steps and answered %d tool messages, want one bash call", len(row.Steps), len(messages))
	}
	call := row.Steps[0].ToolCalls[0]
	if call.Error != "" {
		t.Fatalf("the bash call failed: %s", call.Error)
	}
	calls, cost := scorer.spend()
	if len(messages[0]) >= call.ResultBytes {
		t.Fatalf("the command returned %d bytes and %d reached the model after %d answered calls: nothing was cut, and the message ends %q",
			call.ResultBytes, len(messages[0]), calls, lastLine(messages[0]))
	}
	if call.SiftSavedBytes == 0 {
		t.Fatal("the row says no byte was saved")
	}
	t.Logf("%d bytes ran, %d reached the model, %d saved, %d jev calls at $%.6f",
		call.ResultBytes, len(messages[0]), call.SiftSavedBytes, calls, cost)
	t.Logf("last line: %s", lastLine(messages[0]))
}

func lastLine(message string) string {
	trimmed := strings.TrimRight(message, "\n")
	return trimmed[strings.LastIndex(trimmed, "\n")+1:]
}

func TestTheThreeStatesOfTheShellSiftCountedInBytes(t *testing.T) {
	assumesTheRuleIsOutOfShadow(t)
	chdirTemp(t)
	stubRecordedJudgedReplies(t)
	shell := siftedShell()
	whole := len(shell.Stdout)

	judged, _, err := buildShellSift(siftJudged)
	if err != nil {
		t.Fatalf("buildShellSift judged: %v", err)
	}
	free, freeScorer, err := buildShellSift(siftFree)
	if err != nil {
		t.Fatalf("buildShellSift free: %v", err)
	}
	if freeScorer != nil {
		t.Fatal("the free arm built a jev client")
	}
	unattached, err := turn.NewShellSift(nil)
	if err != nil {
		t.Fatalf("turn.NewShellSift(nil): %v", err)
	}

	cutJudged, err := judged.Cut(t.Context(), shell, "find the line that says what failed")
	if err != nil {
		t.Fatalf("the judged arm: %v", err)
	}
	cutFree, err := free.Cut(t.Context(), shell, "find the line that says what failed")
	if err != nil {
		t.Fatalf("the free arm: %v", err)
	}
	cutNothing, err := unattached.Cut(t.Context(), shell, "find the line that says what failed")
	if err == nil {
		t.Fatal("a sieve with no arm cut the output and reported no error")
	}

	if len(cutJudged.Text) >= whole || cutJudged.Method != method.Judged {
		t.Fatalf("judged: %d of %d bytes through %s", len(cutJudged.Text), whole, cutJudged.Method)
	}
	if len(cutFree.Text) >= whole || cutFree.Method != method.Cheap {
		t.Fatalf("free: %d of %d bytes through %s", len(cutFree.Text), whole, cutFree.Method)
	}
	if len(cutNothing.Text) != whole {
		t.Fatalf("with no arm attached %d of %d bytes reached the model", len(cutNothing.Text), whole)
	}
	t.Logf("%d bytes ran: judged sends %d, free sends %d, nothing attached sends %d (%v)",
		whole, len(cutJudged.Text), len(cutFree.Text), len(cutNothing.Text), err)
}

func TestTheHelpTextSaysWhatTheJudgedShellSiftCosts(t *testing.T) {
	table, err := method.Load(shipped.Files())
	if err != nil {
		t.Fatalf("method.Load: %v", err)
	}
	chosen, err := table.Of(sift.ShellSchema)
	if err != nil {
		t.Fatalf("%v", err)
	}
	if chosen.Cost == "" {
		t.Fatalf("%s states no cost for %s", table.File, sift.ShellSchema)
	}
	printed := runUsage()
	if !strings.Contains(printed, chosen.Cost) {
		t.Fatalf("tofu run --help says\n%s\nand %s states %q", printed, table.File, chosen.Cost)
	}
	if !strings.Contains(printed, "--sift") {
		t.Fatal("the price is printed and nothing says which flag controls it")
	}
	t.Logf("tofu run --help carries %q from %s", chosen.Cost, table.File)
}

func TestAnArmThatIsNeitherFreeNorJudgedIsRefused(t *testing.T) {
	if _, err := parseRunArgs([]string{"--dir", t.TempDir(), "--sift", "maybe", "a task"}); err == nil {
		t.Fatal("--sift maybe parsed")
	}
	for _, arm := range siftArms() {
		opts, err := parseRunArgs([]string{"--dir", t.TempDir(), "--sift", arm, "a task"})
		if err != nil || opts.siftArm != arm {
			t.Fatalf("--sift %s parsed as %q: %v", arm, opts.siftArm, err)
		}
	}
}

type recordedShell struct {
	session string
	task    string
	shell   sift.Shell
}

func recordedShellResults(t *testing.T, sessions string) []recordedShell {
	t.Helper()
	entries, err := os.ReadDir(sessions)
	if err != nil {
		t.Skipf("skipped, and counted: no %s on this machine: %v", sessions, err)
	}
	var out []recordedShell
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		var header struct {
			ID   string `json:"id"`
			Task string `json:"task"`
		}
		raw, readErr := os.ReadFile(filepath.Join(sessions, entry.Name(), "header.json"))
		if readErr != nil {
			continue
		}
		if err := json.Unmarshal(raw, &header); err != nil {
			t.Fatalf("%s: %v", entry.Name(), err)
		}
		out = append(out, bashResultsIn(t, filepath.Join(sessions, entry.Name(), "body.jsonl"), header.ID, header.Task)...)
	}
	return out
}

func bashResultsIn(t *testing.T, body, session, task string) []recordedShell {
	t.Helper()
	file, err := os.Open(body)
	if err != nil {
		return nil
	}
	defer func() { _ = file.Close() }()

	calls := map[string]string{}
	var out []recordedShell
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for scanner.Scan() {
		var event struct {
			Kind string          `json:"kind"`
			Body turn.MessageRow `json:"body"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil || event.Kind != "message" {
			continue
		}
		for _, call := range event.Body.ToolCalls {
			calls[call.ID] = call.Name
		}
		if event.Body.Role != "tool" || calls[event.Body.ToolCallID] != "bash" {
			continue
		}
		out = append(out, recordedShell{session: session, task: task, shell: sift.Shell{Command: "bash", Stdout: event.Body.Content}})
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("%s: %v", body, err)
	}
	return out
}

func TestWhatTheJudgedSiftSavesAndCostsOverEveryRecordedSession(t *testing.T) {
	sessions, err := filepath.Abs(recordedSessionsDir)
	if err != nil {
		t.Fatal(err)
	}
	results := recordedShellResults(t, sessions)
	if len(results) == 0 {
		t.Skip("skipped, and counted: no recorded session carries a bash result")
	}
	assumesTheRuleIsOutOfShadow(t)
	chdirTemp(t)
	stubRecordedJudgedReplies(t)

	sifter, scorer, err := buildShellSift(siftFollowsTheTable)
	if err != nil {
		t.Fatalf("buildShellSift: %v", err)
	}
	type spent struct {
		raw, sent, calls int
	}
	perSession := map[string]spent{}
	var whole spent
	asked := 0
	for _, one := range results {
		scorer.turnID = one.session
		cut, cutErr := sifter.Cut(t.Context(), one.shell, one.task)
		if cutErr != nil {
			t.Fatalf("%s: %v", one.session, cutErr)
		}
		calls, _ := scorer.spend()
		session := perSession[one.session]
		perSession[one.session] = spent{
			raw:   session.raw + len(one.shell.Stdout),
			sent:  session.sent + len(cut.Text),
			calls: session.calls + calls - asked,
		}
		asked = calls
		whole.raw += len(one.shell.Stdout)
		whole.sent += len(cut.Text)
	}
	whole.calls = asked

	prices := make([]float64, 0, len(perSession))
	shares := make([]float64, 0, len(perSession))
	silent := 0
	for _, session := range perSession {
		prices = append(prices, float64(session.calls)*recordedJudgedReplyCostUSD)
		if session.calls == 0 {
			silent++
		}
		if session.raw > 0 {
			shares = append(shares, float64(session.raw-session.sent)/float64(session.raw)*100)
		}
	}
	sort.Float64s(prices)
	sort.Float64s(shares)
	median := prices[len(prices)/2]

	t.Logf("%d bash results over %d recorded sessions: %d bytes became %d, %d saved, %.1f percent, median session %.1f percent, best %.1f",
		len(results), len(perSession), whole.raw, whole.sent, whole.raw-whole.sent,
		float64(whole.raw-whole.sent)/float64(whole.raw)*100, shares[len(shares)/2], slices.Max(shares))
	t.Logf("%d jev calls at the $%g a call the paid run recorded: $%.6f over the whole corpus, median session $%.6f, worst $%.6f",
		whole.calls, recordedJudgedReplyCostUSD, float64(whole.calls)*recordedJudgedReplyCostUSD, median, prices[len(prices)-1])
	t.Logf("%d of the %d sessions ask nothing, because a bash result of one or two chunks is all held and carries no candidate", silent, len(perSession))
	t.Logf("the nouls are the six hand written replies of bench/sift/testdata/judged-replies.jsonl, picked by the chunk position the state names, so the bytes are a shape and only the call count and the price are measured")

	if median > siftMedianSessionCeilingUSD {
		t.Fatalf("the median session costs $%.6f and the ceiling this ticket carries is $%.6f", median, siftMedianSessionCeilingUSD)
	}
	if whole.sent >= whole.raw {
		t.Fatalf("%d bytes ran and %d reached the model", whole.raw, whole.sent)
	}
}

func TestEveryShellSiftDecisionIsWrittenToTheLedger(t *testing.T) {
	assumesTheRuleIsOutOfShadow(t)
	chdirTemp(t)
	stubRecordedJudgedReplies(t)
	sifter, scorer, err := buildShellSift(siftFollowsTheTable)
	if err != nil {
		t.Fatalf("buildShellSift: %v", err)
	}
	scorer.turnID = "turn-sift-ledger"
	if _, err := sifter.Cut(t.Context(), siftedShell(), "find the line that says what failed"); err != nil {
		t.Fatalf("Cut: %v", err)
	}
	dir, err := sys.LogDir()
	if err != nil {
		t.Fatalf("sys.LogDir: %v", err)
	}
	rows := 0
	if _, err := ledger.NewReader(dir).Each(ledger.Filter{Point: sift.ShellSchema}, func(row ledger.Row) error {
		if row.TurnID != "turn-sift-ledger" {
			t.Fatalf("a row says turn %q", row.TurnID)
		}
		rows++
		return nil
	}); err != nil {
		t.Fatalf("reading the ledger back: %v", err)
	}
	calls, _ := scorer.spend()
	if rows != calls || rows == 0 {
		t.Fatalf("%d jev calls left %d ledger rows", calls, rows)
	}
	t.Logf("%d decisions, %d rows", calls, rows)
}
