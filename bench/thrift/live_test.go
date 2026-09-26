package thrift

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"tofu/bench/corpus"
	"tofu/bench/report"
	"tofu/internal/judge/jev"
	"tofu/internal/judge/ledger"
)

const liveCallCap = 400

const reportPath = "report-2026-09-23.md"

func TestTheJudgedThriftCutAgainstFixedTruncationAndTheThreeArmReport(t *testing.T) {
	if os.Getenv("TOFU_LIVE") != "1" {
		t.Skip("set TOFU_LIVE=1 to put the question to the real jev route and write today's report")
	}
	jev.AllowLiveCredential(t)
	client, err := NewJevClient(filepath.Join("..", "..", ".env"))
	if err != nil {
		t.Fatalf("no credential: %v", err)
	}
	jevQuestions, err := ThriftQuestions()
	if err != nil {
		t.Fatalf("ThriftQuestions: %v", err)
	}
	walked, err := corpus.WalkSessions(sessionsDir())
	if err != nil {
		t.Fatalf("WalkSessions(%q): %v", sessionsDir(), err)
	}
	if len(walked.Turns) == 0 {
		t.Fatal("no session was read: the path is wrong or the corpus is empty")
	}
	judged := RunJudged(context.Background(), client, jevQuestions, artifactsDir(), walked.Turns, liveCallCap)
	if len(judged.Rows) == 0 {
		t.Skip("every sampled read or search call was skipped, nothing to write")
	}

	result, err := Run(sessionsDir(), artifactsDir())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	body := "# the judged thrift cut against fixed truncation, and rtk plus thrift against rtk alone and neither\n\n" +
		"generated 2026-09-23, corpus .tofu/sessions and .tofu/artifacts, real jev calls over library/questions/thrift@1.yaml, tool bench/thrift, run by TOFU_LIVE=1 go test ./bench/thrift/ -run TestTheJudgedThriftCutAgainstFixedTruncationAndTheThreeArmReport -v -count=1.\n\n" +
		report.CostUnitLine([]ledger.Unit{ledger.UnitMoney}) + "\n\n" +
		"what this measures against is fixed truncation, internal/turn/artifact.go:60 truncateMiddle at konst.TurnResultBytesCap, the same cut a read, glob or search result gets today.\n\n" +
		"```\n" + Render(result) + RenderJudged(result, judged) + RenderThreeArms(result, judged) + "```\n"

	if err := report.Write(reportPath, []byte(body), 0o644, "report"); err != nil {
		t.Fatalf("report.Write: %v", err)
	}
	t.Logf("wrote %s, %d rows judged, %d live calls, $%.6f spent", reportPath, len(judged.Rows), judged.CallsMade, judged.CostUSD)
}
