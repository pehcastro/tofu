package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"boji/internal/judge/jev"
	"boji/internal/judge/ledger"
	"boji/internal/judge/policy"
	"boji/internal/transport"
	"boji/internal/turn"
)

const replayTestPolicyBody = `name: replay_test
policy_version: 1
questions: replay_test
questions_version: 1
notes: fixture for BOJI-033

risk_question: risk
approval_question: approval
user_requested_question: user_requested
from_untrusted_question: from_untrusted

thresholds:
  risk_ask_at: 1.5
  risk_deny_at: 2.5
  user_requested_relax_at: 0.85
  approval_relax_at: 0.15
  from_untrusted_block_at: 0.5
`

func writeReplayPolicyFixture(t *testing.T) policy.Policy {
	t.Helper()
	policyDir := filepath.Join("catalog", "policy")
	if err := os.MkdirAll(policyDir, 0o755); err != nil {
		t.Fatalf("mkdir catalog/policy: %v", err)
	}
	path := filepath.Join(policyDir, "replay_test@1.yaml")
	if err := os.WriteFile(path, []byte(replayTestPolicyBody), 0o644); err != nil {
		t.Fatalf("writing policy fixture: %v", err)
	}
	pol, err := policy.Load(path)
	if err != nil {
		t.Fatalf("policy.Load: %v", err)
	}
	return pol
}

func scoreAnswer(question string, score float64) ledger.Answer {
	return ledger.Answer{Question: question, Wording: 1, Kind: ledger.AnswerScore, Score: score}
}

func noulAnswer(question string, p float64) ledger.Answer {
	return ledger.Answer{Question: question, Wording: 1, Kind: ledger.AnswerNoul, Noul: p}
}

func replayFixtureAnswers(risk float64) []ledger.Answer {
	return []ledger.Answer{
		scoreAnswer("risk", risk),
		noulAnswer("approval", 0.9),
		noulAnswer("user_requested", 0.05),
		noulAnswer("from_untrusted", 0.02),
	}
}

func writeReplayFixtureRow(t *testing.T, writer *ledger.Writer, pol policy.Policy, answers []ledger.Answer, outcome string) {
	t.Helper()
	verdict, _, err := policy.Decide(ledgerAnswersToJev(answers), pol)
	if err != nil {
		t.Fatalf("policy.Decide: %v", err)
	}
	row := ledger.Row{
		Point:         "tool_gate",
		Questions:     "replay_test",
		Version:       1,
		Answers:       answers,
		Verdict:       toLedgerVerdict(verdict),
		Policy:        pol.Name,
		PolicyVersion: pol.PolicyVersion,
	}
	written, err := writer.Append(row)
	if err != nil {
		t.Fatalf("writer.Append: %v", err)
	}
	if outcome != "" {
		if err := writer.Backfill(written.ID, ledger.Outcome{Kind: "hand-labeled", Detail: outcome}); err != nil {
			t.Fatalf("writer.Backfill: %v", err)
		}
	}
}

func replayTestReader(t *testing.T) (*ledger.Reader, *ledger.Writer) {
	t.Helper()
	t.Chdir(t.TempDir())
	dir, err := ledger.Dir()
	if err != nil {
		t.Fatalf("ledger.Dir: %v", err)
	}
	return ledger.NewReader(dir), ledger.NewWriter(dir)
}

type stubJevWire struct {
	body []byte
	err  error
}

func (s stubJevWire) Caps() jev.WireCaps { return jev.WireCaps{Name: "stub"} }

func (s stubJevWire) Model() string { return "~typesafe/jev-latest" }

func (s stubJevWire) Post(context.Context, []byte) (jev.Raw, error) {
	if s.err != nil {
		return jev.Raw{}, s.err
	}
	return jev.Raw{Body: s.body, RequestID: "stub-1", Attempts: 1}, nil
}

func gateFallbackRow(t *testing.T, reader *ledger.Reader, wire stubJevWire) ledger.Row {
	t.Helper()
	client, err := jev.NewClient(jev.Config{Wire: wire})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	pol := writeReplayPolicyFixture(t)
	gate := &toolGate{
		client: client,
		cwd:    t.TempDir(),
		set: battery{
			SetName:          "tool_gate",
			QuestionsVersion: 1,
			Questions:        []jev.Question{{ID: "risk", Kind: jev.QuestionNoul, Instructions: "how risky is this call", True: "risky", False: "safe"}},
			Policy:           &pol,
			Mode:             policy.ModeShadow,
		},
	}
	decision, err := gate.Decide(context.Background(), turn.GateRequest{
		TurnID: "turn-1",
		Task:   "tidy the scratch directory",
		Tool:   "bash",
		Args:   json.RawMessage(`{"command":"rm -rf /tmp/scratch"}`),
	})
	if err != nil {
		t.Fatalf("the gate returned an error instead of a row: %v", err)
	}
	if decision.Verdict != string(ledger.VerdictAsk) {
		t.Fatalf("verdict = %q, want ask", decision.Verdict)
	}
	row, ok, err := reader.ByID(decision.ID)
	if err != nil || !ok {
		t.Fatalf("ByID %s: ok=%v err=%v", decision.ID, ok, err)
	}
	return row
}

func TestTheGateWritesARowForEveryUnavailableReason(t *testing.T) {
	cases := []struct {
		name string
		wire stubJevWire
		want string
	}{
		{name: "timeout", wire: stubJevWire{err: transport.Fail("stub", transport.KindTimeout, nil, "no answer in 2.5 s")}, want: "unavailable_timeout"},
		{name: "refusal", wire: stubJevWire{err: transport.Fail("stub", transport.KindAuth, nil, "the key was rejected")}, want: "unavailable_refused"},
		{name: "rate limit", wire: stubJevWire{err: transport.Fail("stub", transport.KindRateLimit, nil, "429")}, want: "unavailable_rate_limit"},
		{name: "transport failure", wire: stubJevWire{err: transport.Fail("stub", transport.KindProvider, nil, "no such host")}, want: "unavailable_transport"},
		{name: "malformed answer", wire: stubJevWire{body: []byte(`{"model":"jev-2026-09-18","answers":{}}`)}, want: "unavailable_malformed_answer"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			reader, _ := replayTestReader(t)
			row := gateFallbackRow(t, reader, c.wire)
			if row.Reason.Comparison != c.want {
				t.Fatalf("comparison = %q, want %q", row.Reason.Comparison, c.want)
			}
			if row.Verdict != ledger.VerdictAsk {
				t.Fatalf("verdict = %s, want ask", row.Verdict)
			}
			if len(row.Answers) != 0 {
				t.Fatalf("the row carries %d answers, an unavailable decision has none", len(row.Answers))
			}
			if row.TurnID != "turn-1" || row.StateBuilder == "" {
				t.Fatalf("the row lost its turn or its state builder: turn %q builder %q", row.TurnID, row.StateBuilder)
			}
		})
	}
}

func TestReplaySweepsPastRowsWhereTheTypedDecisionWasNotMade(t *testing.T) {
	reader, writer := replayTestReader(t)
	pol := writeReplayPolicyFixture(t)
	writeReplayFixtureRow(t, writer, pol, replayFixtureAnswers(2), "")
	gateFallbackRow(t, reader, stubJevWire{err: transport.Fail("stub", transport.KindTimeout, nil, "no answer in 2.5 s")})

	result, err := runReplay(reader, ledger.Filter{Point: "tool_gate"}, map[string]float64{"risk_deny_at": 1.9})
	if err != nil {
		t.Fatalf("runReplay over a ledger holding one unavailable row: %v", err)
	}
	if result.read != 2 || result.rescored != 1 || result.unavailable != 1 {
		t.Fatalf("read=%d rescored=%d unavailable=%d, want 2/1/1", result.read, result.rescored, result.unavailable)
	}

	var out, errOut bytes.Buffer
	if code := replayVerb([]string{"--point", "tool_gate"}, &out, &errOut, time.Now); code != exitOK {
		t.Fatalf("exit = %d, want %d, stderr %s", code, exitOK, errOut.String())
	}
	if !strings.Contains(out.String(), "1 unavailable") {
		t.Fatalf("the report does not count the unavailable row: %s", out.String())
	}
	t.Logf("boji replay --point tool_gate:\n%s", out.String())
}

func TestReplayIdentityAtCurrentThresholdsChangesNothing(t *testing.T) {
	reader, writer := replayTestReader(t)
	pol := writeReplayPolicyFixture(t)

	writeReplayFixtureRow(t, writer, pol, replayFixtureAnswers(0), "")
	writeReplayFixtureRow(t, writer, pol, replayFixtureAnswers(2), "")
	writeReplayFixtureRow(t, writer, pol, replayFixtureAnswers(3), "")
	if _, err := writer.Append(ledger.Row{Point: "tool_gate", Answers: replayFixtureAnswers(1)}); err != nil {
		t.Fatalf("writer.Append: %v", err)
	}

	result, err := runReplay(reader, ledger.Filter{Point: "tool_gate"}, map[string]float64{})
	if err != nil {
		t.Fatalf("runReplay: %v", err)
	}
	if got := len(result.changes); got != 0 {
		t.Fatalf("replaying at the current thresholds moved %d rows, want 0: %+v", got, result.changes)
	}
	if result.read != 4 || result.rescored != 3 || result.skipped != 1 {
		t.Fatalf("read=%d rescored=%d skipped=%d, want 4/3/1", result.read, result.rescored, result.skipped)
	}
}

func TestReplayIdentityBreaksWhenThresholdsActuallyMove(t *testing.T) {
	reader, writer := replayTestReader(t)
	pol := writeReplayPolicyFixture(t)
	writeReplayFixtureRow(t, writer, pol, replayFixtureAnswers(2), "")

	result, err := runReplay(reader, ledger.Filter{Point: "tool_gate"}, map[string]float64{"risk_deny_at": 1.9})
	if err != nil {
		t.Fatalf("runReplay: %v", err)
	}
	if len(result.changes) != 1 {
		t.Fatalf("moving the deny threshold below the fixture's risk score found no change, the identity test would not have caught a no-op replay")
	}
}

func TestReplayCountsRowsWithNoPolicyAsSkipped(t *testing.T) {
	reader, writer := replayTestReader(t)
	pol := writeReplayPolicyFixture(t)
	writeReplayFixtureRow(t, writer, pol, replayFixtureAnswers(0), "")
	if _, err := writer.Append(ledger.Row{Point: "tool_gate", Answers: replayFixtureAnswers(3)}); err != nil {
		t.Fatalf("writer.Append: %v", err)
	}

	result, err := runReplay(reader, ledger.Filter{Point: "tool_gate"}, map[string]float64{})
	if err != nil {
		t.Fatalf("runReplay: %v", err)
	}
	if result.read != result.rescored+result.skipped {
		t.Fatalf("read=%d does not equal rescored=%d + skipped=%d", result.read, result.rescored, result.skipped)
	}
	if result.skipped != 1 {
		t.Fatalf("skipped=%d, want 1", result.skipped)
	}
}

func TestReplayCountsAgreementAndDisagreementWithOutcome(t *testing.T) {
	reader, writer := replayTestReader(t)
	pol := writeReplayPolicyFixture(t)

	writeReplayFixtureRow(t, writer, pol, replayFixtureAnswers(2), "deny")
	writeReplayFixtureRow(t, writer, pol, replayFixtureAnswers(2.1), "ask")
	writeReplayFixtureRow(t, writer, pol, replayFixtureAnswers(0), "allow")

	result, err := runReplay(reader, ledger.Filter{Point: "tool_gate"}, map[string]float64{"risk_deny_at": 1.9})
	if err != nil {
		t.Fatalf("runReplay: %v", err)
	}
	var agree, disagree int
	for _, c := range result.changes {
		if c.row.Outcome == nil {
			continue
		}
		if string(c.after) == c.row.Outcome.Detail {
			agree++
		} else {
			disagree++
		}
	}
	if agree != 1 || disagree != 1 {
		t.Fatalf("agree=%d disagree=%d, want 1/1: %+v", agree, disagree, result.changes)
	}
}

func TestReplayUnknownThresholdExitsTwoAndNamesTheKnownOnes(t *testing.T) {
	var out, errOut bytes.Buffer
	code := replayVerb([]string{"--point", "tool_gate", "--set", "risk_deny_att=2.2"}, &out, &errOut, time.Now)
	if code != exitUsage {
		t.Fatalf("exit = %d, want %d", code, exitUsage)
	}
	if !strings.Contains(errOut.String(), "risk_ask_at") || !strings.Contains(errOut.String(), "risk_deny_at") {
		t.Fatalf("error does not name the declared thresholds: %s", errOut.String())
	}
}

func TestReplayRunsWithNoNetworkCredentialOrWireConfigured(t *testing.T) {
	t.Setenv("OPENROUTER_KEY", "")
	_, writer := replayTestReader(t)
	pol := writeReplayPolicyFixture(t)
	writeReplayFixtureRow(t, writer, pol, replayFixtureAnswers(0), "")

	var out, errOut bytes.Buffer
	code := replayVerb([]string{"--point", "tool_gate"}, &out, &errOut, time.Now)
	if code != exitOK {
		t.Fatalf("exit = %d, want %d, stderr %s", code, exitOK, errOut.String())
	}
	if !strings.Contains(out.String(), "0 API calls") {
		t.Fatalf("report does not state 0 API calls: %s", out.String())
	}
}

func TestReplayNeedsAPoint(t *testing.T) {
	var out, errOut bytes.Buffer
	code := replayVerb(nil, &out, &errOut, time.Now)
	if code != exitUsage {
		t.Fatalf("exit = %d, want %d", code, exitUsage)
	}
}

func TestReplaySinceParsesDays(t *testing.T) {
	d, err := parseSince("30d")
	if err != nil {
		t.Fatalf("parseSince: %v", err)
	}
	if d != 30*24*time.Hour {
		t.Fatalf("parseSince(30d) = %s, want 720h", d)
	}
}
