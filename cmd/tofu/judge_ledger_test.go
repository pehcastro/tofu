package main

import (
	"context"
	"fmt"
	"testing"

	"tofu/internal/judge/gate"
	"tofu/internal/judge/jev"
	"tofu/internal/judge/ledger"
	"tofu/internal/judge/question"
	"tofu/internal/sys"
)

func panicOf(call func()) (message string) {
	defer func() {
		if raised := recover(); raised != nil {
			message = fmt.Sprint(raised)
		}
	}()
	call()
	return ""
}

func TestToLedgerVerdictNamesEveryGateVerdict(t *testing.T) {
	want := map[gate.Verdict]ledger.Verdict{
		gate.VerdictAllow: ledger.VerdictAllow,
		gate.VerdictAsk:   ledger.VerdictAsk,
		gate.VerdictDeny:  ledger.VerdictDeny,
	}
	for _, v := range gate.AllVerdicts() {
		expected, named := want[v]
		if !named {
			t.Fatalf("%s carries no expected ledger verdict, so a new gate verdict can reach toLedgerVerdict untested", v)
		}
		if got := toLedgerVerdict(v); got != expected {
			t.Errorf("toLedgerVerdict(%s) = %s, want %s", v, got, expected)
		}
	}
}

func TestToGateVerdictNamesEveryLedgerVerdict(t *testing.T) {
	want := map[ledger.Verdict]gate.Verdict{
		ledger.VerdictAllow: gate.VerdictAllow,
		ledger.VerdictAsk:   gate.VerdictAsk,
		ledger.VerdictDeny:  gate.VerdictDeny,
	}
	for _, v := range ledger.AllVerdicts() {
		if v == ledger.VerdictUnset {
			if raised := panicOf(func() { toGateVerdict(v) }); raised == "" {
				t.Errorf("toGateVerdict(unset) no longer panics: the impossible state is now reachable")
			}
			continue
		}
		expected, named := want[v]
		if !named {
			t.Fatalf("%s carries no expected gate verdict, so a new ledger verdict can reach toGateVerdict untested", v)
		}
		if got := toGateVerdict(v); got != expected {
			t.Errorf("toGateVerdict(%s) = %s, want %s", v, got, expected)
		}
	}
}

type stubWire struct {
	calls int
	reply string
}

func (s *stubWire) Caps() jev.WireCaps {
	return jev.WireCaps{MaxRequestBytes: 90000, MaxChoiceOptions: 255, MaxScoreLevels: 10, ReturnsConfidence: true}
}

func (s *stubWire) Model() string { return "~typesafe/jev-latest" }

func (s *stubWire) Post(_ context.Context, _ []byte) (jev.Raw, error) {
	s.calls++
	return jev.Raw{Body: []byte(s.reply), RequestID: "req-stub", Attempts: 1}, nil
}

func TestAReplayedRowAgreesOnBuildAndDisagreesOnTheMarker(t *testing.T) {
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

	if _, err := runJudge(context.Background(), client, req, set, false); err != nil {
		t.Fatalf("first runJudge: %v", err)
	}
	if _, err := runJudge(context.Background(), client, req, set, false); err != nil {
		t.Fatalf("second runJudge: %v", err)
	}
	if wire.calls != 1 {
		t.Fatalf("expected one wire call across both runs, got %d", wire.calls)
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
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
	original, replay := rows[0], rows[1]

	if original.ReplayOf != "" {
		t.Fatalf("the original row should carry no replay marker, got %q", original.ReplayOf)
	}
	if replay.ReplayOf != original.ID {
		t.Fatalf("replay_of = %q, want the original row's id %q", replay.ReplayOf, original.ID)
	}
	if replay.Build != original.Build {
		t.Fatalf("build disagrees: original %q, replay %q", original.Build, replay.Build)
	}
	if replay.RequestID != original.RequestID {
		t.Fatalf("request id disagrees: original %q, replay %q", original.RequestID, replay.RequestID)
	}
	if replay.Version != original.Version {
		t.Fatalf("wording version disagrees: original %d, replay %d", original.Version, replay.Version)
	}
	if original.Cost == 0 {
		t.Fatal("the original row should carry the real cost")
	}
	if replay.Cost != 0 {
		t.Fatalf("a replay costs nothing, got %v", replay.Cost)
	}
	if replay.LatencyMS != 0 {
		t.Fatalf("a replay takes no time, got %d ms", replay.LatencyMS)
	}
}
