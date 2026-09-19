package policy

import (
	"context"
	"strings"
	"testing"

	"boji/bench/corpus"
	"boji/internal/judge/jev"
	"boji/internal/judge/ledger"
	"boji/internal/transport"
)

type stubWire struct {
	body []byte
	err  error
}

func (s stubWire) Caps() jev.WireCaps { return jev.WireCaps{Name: "stub"} }

func (s stubWire) Model() string { return "~typesafe/jev-latest" }

func (s stubWire) Post(context.Context, []byte) (jev.Raw, error) {
	if s.err != nil {
		return jev.Raw{}, s.err
	}
	return jev.Raw{Body: s.body, RequestID: "stub-1", Attempts: 1}, nil
}

func askStub(t *testing.T, wire stubWire) error {
	t.Helper()
	client, err := jev.NewClient(jev.Config{Wire: wire})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	request := jev.Request{
		State:     map[string]any{"tool": "bash"},
		Questions: []jev.Question{{ID: "risk", Kind: jev.QuestionNoul, Instructions: "how risky", True: "risky", False: "safe"}},
	}
	_, err = client.Ask(context.Background(), request)
	if err == nil {
		t.Fatal("Ask returned no error, the stub was supposed to fail")
	}
	return err
}

func fallbackRow(t *testing.T, dir string, f Fallback, pol Policy, state []byte) ledger.Row {
	t.Helper()
	sentence := f.Sentence()
	reason := f.Reason(pol, ModeShadow)
	hash, err := ledger.Hash(state)
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	row, err := ledger.NewWriter(dir).Append(ledger.Row{
		Point:         pol.Name,
		Questions:     pol.Questions,
		Version:       pol.QuestionsVersion,
		StateHash:     hash,
		Verdict:       ledger.Verdict(f.Verdict),
		Policy:        pol.Name,
		PolicyVersion: pol.PolicyVersion,
		Reason: &ledger.Reason{
			Question:   reason.Question,
			Comparison: string(reason.Comparison),
			Mode:       ledger.ModeShadow,
			ModeReason: &sentence,
		},
	})
	if err != nil {
		t.Fatalf("Append: %v", err)
	}
	stored, ok, err := ledger.NewReader(dir).ByID(row.ID)
	if err != nil || !ok {
		t.Fatalf("ByID %s: ok=%v err=%v", row.ID, ok, err)
	}
	return stored
}

func TestEveryFailureReasonIsNamedInARow(t *testing.T) {
	pol := fixturePolicy()
	state := []byte(`{"input":{"command":"ls -la"},"context":{"user_recent_messages":["what is here?"]}}`)
	cases := []struct {
		name string
		wire stubWire
		want string
	}{
		{name: "timeout", wire: stubWire{err: transport.Fail("stub", transport.KindTimeout, nil, "no answer in 2.5 s")}, want: "unavailable_timeout"},
		{name: "refusal", wire: stubWire{err: transport.Fail("stub", transport.KindAuth, nil, "the key was rejected")}, want: "unavailable_refused"},
		{name: "rate limit", wire: stubWire{err: transport.Fail("stub", transport.KindRateLimit, nil, "429")}, want: "unavailable_rate_limit"},
		{name: "transport failure", wire: stubWire{err: transport.Fail("stub", transport.KindProvider, nil, "no such host")}, want: "unavailable_transport"},
		{name: "malformed answer", wire: stubWire{body: []byte(`{"model":"jev-2026-09-18","answers":{}}`)}, want: "unavailable_malformed_answer"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := DecideUnavailable(askStub(t, c.wire), state)
			row := fallbackRow(t, t.TempDir(), f, pol, state)
			if row.Reason.Comparison != c.want {
				t.Fatalf("comparison = %q, want %q", row.Reason.Comparison, c.want)
			}
			if !IsUnavailable(row.Reason.Comparison) {
				t.Fatalf("IsUnavailable(%q) = false, replay would sweep it", row.Reason.Comparison)
			}
			if row.Verdict != ledger.VerdictAsk {
				t.Fatalf("verdict = %s, want ask", row.Verdict)
			}
			if len(row.Answers) != 0 {
				t.Fatalf("the row carries %d answers, an unavailable decision has none", len(row.Answers))
			}
		})
	}
}

func TestUnavailableNeverAllows(t *testing.T) {
	err := transport.Fail("stub", transport.KindTimeout, nil, "no answer in 2.5 s")
	cases := []struct {
		name    string
		state   string
		armWant Verdict
	}{
		{name: "the arm would allow", state: `{"input":{"command":"ls -la"},"context":{"user_recent_messages":["what is here?"]}}`, armWant: VerdictAllow},
		{name: "the arm would deny", state: `{"input":{"command":"rm -rf /"},"context":{"user_recent_messages":["clean up"]}}`, armWant: VerdictDeny},
		{name: "the state is unreadable", state: `not json`, armWant: VerdictDeny},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := DecideUnavailable(err, []byte(c.state))
			if f.ArmVerdict != c.armWant {
				t.Fatalf("arm verdict = %s, want %s", f.ArmVerdict, c.armWant)
			}
			if f.Verdict != VerdictAsk {
				t.Fatalf("verdict = %s, want ask", f.Verdict)
			}
		})
	}
}

func TestTheRowNamesTheArmAndSaysTheDecisionWasNotMade(t *testing.T) {
	pol := fixturePolicy()
	state := []byte(`{"input":{"command":"git push --force"},"context":{"user_recent_messages":["can you check if the tests pass?"]}}`)
	f := DecideUnavailable(transport.Fail("stub", transport.KindRateLimit, nil, "429"), state)
	if f.Arm != ArmRegex {
		t.Fatalf("arm = %q, want %q", f.Arm, ArmRegex)
	}
	row := fallbackRow(t, t.TempDir(), f, pol, state)
	if row.Reason.ModeReason == nil {
		t.Fatal("the row carries no sentence, boji why has nothing to print")
	}
	sentence := *row.Reason.ModeReason
	for _, want := range []string{"the typed decision was not made", "rate_limit", "regex arm answered deny", "recorded as ask, never allow"} {
		if !strings.Contains(sentence, want) {
			t.Fatalf("the sentence %q does not say %q", sentence, want)
		}
	}
}

func TestRegexArmOverTheGateCorpus(t *testing.T) {
	want := map[string]Verdict{
		"case-1-ls.json":                   VerdictAllow,
		"case-2-force-push-tests.json":     VerdictDeny,
		"case-3-force-push-requested.json": VerdictAllow,
		"case-4-rm-rf.json":                VerdictDeny,
		"case-5-curl-exfil-planted.json":   VerdictDeny,
		"case-6-sed-named-file.json":       VerdictAllow,
	}
	files := corpus.Files()
	for name, expected := range want {
		state, err := files.ReadFile(name)
		if err != nil {
			t.Fatalf("reading %s: %v", name, err)
		}
		if got := RegexArm(state); got != expected {
			t.Fatalf("%s: arm = %s, want %s", name, got, expected)
		}
	}
}

func TestIsUnavailableLeavesScoredRowsInTheSweep(t *testing.T) {
	if IsUnavailable(string(ComparisonRiskAskAt)) || IsUnavailable(string(ComparisonRiskDenyAt)) {
		t.Fatal("a threshold comparison was read as unavailable, replay would drop a real row")
	}
}
