package sift

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"tofu/internal/judge/jev"
	"tofu/internal/judge/jev/wire/openrouter"
	"tofu/internal/judge/ledger"
	"tofu/internal/judge/question"
	"tofu/internal/konst"
	"tofu/internal/sift"
	libraryquestions "tofu/library/questions"
)

const replyFile = "testdata/judged-replies.jsonl"

type replayWire struct {
	replies [][]byte
	served  atomic.Int64
}

func (w *replayWire) Model() string { return openrouter.Alias }

func (w *replayWire) Caps() jev.WireCaps {
	return jev.WireCaps{
		Name:              "recorded",
		MaxStateTokens:    konst.JudgeStateTokenCeiling,
		MaxRequestTokens:  konst.JudgeRequestTokenCeiling,
		MaxRequestBytes:   jev.EstimateBytes(konst.JudgeStateTokenCeiling),
		CriteriaKinds:     []jev.CriteriaKind{jev.CriteriaString, jev.CriteriaObject, jev.CriteriaNull},
		MaxChoiceOptions:  konst.ChoiceCeiling,
		MaxScoreLevels:    konst.JudgeScoreLevelCeiling,
		ReturnsConfidence: true,
	}
}

func (w *replayWire) Post(_ context.Context, _ []byte) (jev.Raw, error) {
	at := w.served.Add(1) - 1
	return jev.Raw{
		Body:      w.replies[int(at)%len(w.replies)],
		RequestID: fmt.Sprintf("recorded-%d", at),
		Attempts:  1,
		Latency:   time.Millisecond,
	}, nil
}

func recordedClient(t *testing.T) *jev.Client {
	t.Helper()
	file, err := os.Open(replyFile)
	if err != nil {
		t.Fatalf("open %s: %v", replyFile, err)
	}
	defer func() { _ = file.Close() }()
	wire := &replayWire{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		if strings.TrimSpace(scanner.Text()) == "" {
			continue
		}
		wire.replies = append(wire.replies, append([]byte(nil), scanner.Bytes()...))
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("read %s: %v", replyFile, err)
	}
	if len(wire.replies) == 0 {
		t.Fatalf("%s carries no recorded reply", replyFile)
	}
	client, err := jev.NewClient(jev.Config{Wire: wire})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return client
}

func shippedQuestions(t *testing.T) question.Set {
	t.Helper()
	layers := []question.Layer{{Name: "library", Origin: "library/questions", FS: libraryquestions.Files()}}
	set, _, err := question.Resolve(Point, layers)
	if err != nil {
		t.Fatalf("resolve %s: %v", Point, err)
	}
	return set
}

func TestTheJudgedArmWritesARowPerDecision(t *testing.T) {
	dir := t.TempDir()
	asker := Asker{Client: recordedClient(t), Set: shippedQuestions(t), Ledger: ledger.NewWriter(dir)}

	decisions := 0
	for i, row := range corpusRows(t) {
		planted, err := Plant(row, i)
		if err != nil {
			t.Fatalf("Plant: %v", err)
		}
		asked := asker.Ask(context.Background(), planted.Shell, planted.Units, row.Task)
		if asked.Errors > 0 {
			t.Fatalf("%s: %d of the recorded replies did not decode", row.Session, asked.Errors)
		}
		if len(asked.Unrecorded) > 0 {
			t.Fatalf("%s: a decision was not written: %v", row.Session, asked.Unrecorded[0])
		}
		decisions += len(asked.Scores)
	}

	stats, err := ledger.Summary(dir)
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	if stats.Rows != decisions {
		t.Fatalf("the judged arm made %d decisions and left %d rows under %s", decisions, stats.Rows, dir)
	}

	elided, largestState := 0, 0
	reader := ledger.NewReader(dir)
	report, err := reader.Each(ledger.Filter{Origin: ledger.OriginBench}, func(row ledger.Row) error {
		if row.Bench != BenchName || row.Point != Point || row.Build != recordedBuild {
			return fmt.Errorf("row %s reads as bench=%q point=%q build=%q", row.ID, row.Bench, row.Point, row.Build)
		}
		if len(row.Answers) != 1 || row.Answers[0].Question != sift.NeededQuestion || row.Answers[0].Kind != ledger.AnswerNoul {
			return fmt.Errorf("row %s carries answers %+v", row.ID, row.Answers)
		}
		state, err := reader.State(row)
		if err != nil {
			return err
		}
		if !strings.Contains(string(state), `"chunk"`) {
			return fmt.Errorf("row %s reads its state back as %d bytes carrying no chunk", row.ID, len(state))
		}
		hash, err := ledger.Hash(state)
		if err != nil {
			return err
		}
		if hash != row.StateHash {
			return fmt.Errorf("row %s names state %s and the state it points at hashes to %s", row.ID, row.StateHash, hash)
		}
		if row.StateElision == nil {
			if len(state) > largestState {
				largestState = len(state)
			}
			return nil
		}
		elided++
		if len(state) != row.StateElision.Bytes {
			return fmt.Errorf("row %s elided %d bytes and %d read back", row.ID, row.StateElision.Bytes, len(state))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("reading the bench ledger back: %v", err)
	}
	if report.Matched != decisions {
		t.Fatalf("%d rows read back as bench rows, %d decisions were made", report.Matched, decisions)
	}
	t.Logf("%d decisions, %d rows under %s, every state read back through the reader, %d elided, largest inline state %d bytes",
		decisions, report.Matched, dir, elided, largestState)
}
