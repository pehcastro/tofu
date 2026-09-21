package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"tofu/interface/tui/markdown"
	"tofu/interface/tui/session"
	"tofu/internal/judge/jev"
	"tofu/internal/judge/ledger"
	"tofu/internal/turn"
)

const (
	recordedResponse = "../../internal/judge/jev/testdata/adapter-cassette-response.json"
	recordedLog      = "../../.tofu/log"
	recordedAskRow   = "2026-09-20-0d8f0bece3a1a2a00a28e585e3a47795"
)

func drawnFrame(t *testing.T, decision session.Decision) string {
	t.Helper()
	view := session.New(time.Now, new(markdown.Renderer).Lines)
	view.SetSize(120, 20)
	view.Append(session.Entry{Kind: session.Tool, Head: decision.Tool, Body: "git push --force origin main"})
	view.Decide(decision)
	return view.View()
}

func TestTheLegendOfARecordedResponseReachesTheDrawnFrame(t *testing.T) {
	raw, err := os.ReadFile(filepath.FromSlash(recordedResponse))
	if err != nil {
		t.Fatal(err)
	}
	response, err := jev.Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	frame := drawnFrame(t, gateDecision("bash", turn.GateDecision{
		ID:      "2026-09-21-cassette",
		Verdict: ledger.VerdictAsk,
		Answers: toLedgerAnswers(6, response.Answers),
		Reason: &ledger.Reason{
			Question:   "rating",
			Comparison: "rating_ask_at",
			Threshold:  1.5,
			Value:      response.Answers["rating"].Score,
		},
	}))
	const want = "rating is the reviewer offers unreserved praise and an emphatic recommendation, " +
		"so the call is asked about"
	if !strings.Contains(frame, want) {
		t.Fatalf("the word the wire sent does not reach the frame, want %q\n%s", want, frame)
	}
}

func TestARecordedRowWithNoLegendKeepsItsNumbers(t *testing.T) {
	if _, err := os.Stat(filepath.FromSlash(recordedLog)); err != nil {
		t.Skipf("no recorded ledger in this tree: %v", err)
	}
	row, found, err := ledger.NewReader(filepath.FromSlash(recordedLog)).ByID(recordedAskRow)
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatalf("row %s is not in %s", recordedAskRow, recordedLog)
	}
	for _, answer := range row.Answers {
		if len(answer.Legend) != 0 {
			t.Fatalf("a row recorded before the field carries a legend: %+v", answer)
		}
	}
	frame := drawnFrame(t, gateDecision("bash", turn.GateDecision{
		ID:      row.ID,
		Verdict: row.Verdict,
		Answers: row.Answers,
		Reason:  row.Reason,
	}))
	const want = "risk 2.05 is over risk_ask_at 1.50"
	if !strings.Contains(frame, want) {
		t.Fatalf("the recorded row no longer draws %q\n%s", want, frame)
	}
}
