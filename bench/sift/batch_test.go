package sift

import (
	"context"
	"os"
	"testing"

	"tofu/bench/api"
	"tofu/internal/judge/jev"
	"tofu/internal/sift"
)

const (
	batchFile = "testdata/batch-2026-10-16.jsonl"
	batchRuns = 2
)

func TestEveryBatchFitsTheCeilingsBeforeAnythingIsSent(t *testing.T) {
	needed, ok := shippedQuestions(t).Question(sift.NeededQuestion)
	if !ok {
		t.Fatalf("the shipped set carries no %q", sift.NeededQuestion)
	}
	largestRequest, largestQuestion, questions := 0, 0, 0
	for i, row := range corpusRows(t) {
		planted, err := Plant(row, i)
		if err != nil {
			t.Fatalf("Plant: %v", err)
		}
		for _, arm := range []Arm{ArmBatched, ArmBatchedRubric} {
			batch, err := BuildBatch(arm, needed.ToJev(), "~typesafe/jev-latest", planted.Shell, planted.Units, row.Task)
			if err != nil {
				t.Fatalf("row %d %s: %v", i, arm, err)
			}
			largestRequest = max(largestRequest, batch.RequestTokens)
			largestQuestion = max(largestQuestion, batch.QuestionTokens)
			questions = max(questions, len(batch.Indexes))
		}
	}
	t.Logf("largest batched request about %d tokens, largest single question about %d, most questions in one request %d", largestRequest, largestQuestion, questions)
}

func TestEveryChunkInOneRequestAgainstOneRequestPerChunk(t *testing.T) {
	asker := liveClientOn(t, jev.TypeSafeVariable, api.NewTypeSafeWire)
	asker.Ledger = nil
	rows := corpusRows(t)
	var asked []Asked
	for run := 1; run <= batchRuns; run++ {
		for i, row := range rows {
			planted, err := Plant(row, i)
			if err != nil {
				t.Fatalf("Plant: %v", err)
			}
			for turn := range JevArms {
				arm := JevArms[(i+run+turn)%len(JevArms)]
				a, err := AskArm(context.Background(), asker, arm, planted, row.Task)
				if err != nil {
					t.Fatalf("row %d %s: %v", i, arm, err)
				}
				if a.Errors > 0 {
					t.Fatalf("run %d row %d %s: %d of %d requests failed %s", run, i, arm, a.Errors, a.Requests, a.Failure)
				}
				a.Run, a.Row = run, i
				asked = append(asked, a)
			}
		}
	}
	table, err := BatchTable(rows, asked, shippedRule(t).KeepAt)
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteAsked(batchFile, asked); err != nil {
		t.Fatalf("WriteAsked: %v", err)
	}
	t.Log("\n" + table)
}

func TestTheBatchTableFromTheRecordedRun(t *testing.T) {
	if _, err := os.Stat(batchFile); err != nil {
		t.Skipf("%s is not recorded yet: run TestEveryChunkInOneRequestAgainstOneRequestPerChunk with TOFU_LIVE=1", batchFile)
	}
	asked, err := ReadAsked(batchFile)
	if err != nil {
		t.Fatal(err)
	}
	table, err := BatchTable(corpusRows(t), asked, shippedRule(t).KeepAt)
	if err != nil {
		t.Fatal(err)
	}
	t.Log("\n" + table)
}
