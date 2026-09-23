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

const overCapLiveCallCap = 250

const overCapReportPath = "report-2026-09-23-overcap.md"

func TestTheNineOverCapArtifactsJudgedAgainstFixedTruncation(t *testing.T) {
	if os.Getenv("TOFU_LIVE") != "1" {
		t.Skip("set TOFU_LIVE=1 to put the question to the real jev route and write today's over-cap report")
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
	walked, err := corpus.WalkSessions(sessionsDir)
	if err != nil {
		t.Fatalf("WalkSessions(%q): %v", sessionsDir, err)
	}
	if len(walked.Turns) == 0 {
		t.Fatal("no session was read: the path is wrong or the corpus is empty")
	}
	targets, idSkips := OverCapReadAndSearchTargets(artifactsDir, walked.Turns)
	if len(targets)+len(idSkips) == 0 {
		t.Fatal("no over-cap read or search artifact was found in the corpus")
	}
	judged := RunJudgedOverCap(context.Background(), client, jevQuestions, targets, overCapLiveCallCap)

	body := "# the 9 over-cap read and search artifacts, judged against fixed truncation\n\n" +
		"generated 2026-09-23, corpus .tofu/sessions and .tofu/artifacts, real jev calls over library/questions/thrift@1.yaml, tool bench/thrift, run by TOFU_LIVE=1 go test ./bench/thrift/ -run TestTheNineOverCapArtifactsJudgedAgainstFixedTruncation -v -count=1.\n\n" +
		report.CostUnitLine([]ledger.Unit{ledger.UnitMoney}) + "\n\n" +
		"this follows report-2026-09-23.md section 5, which sampled 8 results all under the truncation cap. This report reaches the other 9, the ones over it, ordered smallest paragraph count first so the call cap covers as many distinct artifacts as it can.\n\n" +
		"```\n" + RenderOverCap(targets, idSkips, judged) + "```\n"

	if err := report.Write(overCapReportPath, []byte(body), 0o644, "report"); err != nil {
		t.Fatalf("report.Write: %v", err)
	}
	t.Logf("wrote %s, %d of %d over-cap artifacts judged, %d live calls, $%.6f spent", overCapReportPath, len(judged.Rows), len(targets), judged.CallsMade, judged.CostUSD)
}
