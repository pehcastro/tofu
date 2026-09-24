package main

import (
	"context"
	"testing"

	"tofu/internal/judge/gate"
	"tofu/internal/judge/jev"
	"tofu/internal/judge/ledger"
	"tofu/internal/judge/question"
	"tofu/internal/sys"
)

func TestTheLedgerReasonCarriesTheModeTheGateRanUnder(t *testing.T) {
	for _, m := range gate.AllModes() {
		if got := toLedgerReason(gate.Reason{Mode: m}).Mode; got != m.Ledger() {
			t.Errorf("toLedgerReason(%s).Mode = %s, want %s", m, got, m.Ledger())
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
