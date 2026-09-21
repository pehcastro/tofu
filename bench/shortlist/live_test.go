package shortlist

import (
	"context"
	"os"
	"testing"
	"time"

	"tofu/internal/judge/jev"
)

const repoRoot = "../.."

func liveClient(t *testing.T) *jev.Client {
	t.Helper()
	if os.Getenv("TOFU_LIVE") != "1" {
		t.Skip("set TOFU_LIVE=1 to put the question to the real jev route")
	}
	jev.AllowLiveCredential(t)
	key, err := jev.Key(repoRoot + "/.env")
	if err != nil {
		t.Fatalf("no credential: %v", err)
	}
	wire, err := NewWire(key)
	if err != nil {
		t.Fatalf("NewWire: %v", err)
	}
	client, err := jev.NewClient(jev.Config{Wire: wire})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return client
}

func corpusRows(t *testing.T) []Row {
	t.Helper()
	rows, err := ReadCorpus()
	if err != nil {
		t.Fatalf("ReadCorpus: %v", err)
	}
	if len(rows) == 0 {
		t.Fatal("the corpus is empty")
	}
	return rows
}

type armTally struct {
	name  string
	hit1  int
	hit5  int
	total int
}

func (a *armTally) add(ranked []Ranked, label []string) {
	a.total++
	if HitAt(ranked, label, 1) {
		a.hit1++
	}
	if HitAt(ranked, label, 5) {
		a.hit5++
	}
}

func (a armTally) log(t *testing.T) {
	t.Logf("%-10s hit@1 %d/%d (%.1f%%) hit@5 %d/%d (%.1f%%)",
		a.name, a.hit1, a.total, 100*float64(a.hit1)/float64(a.total),
		a.hit5, a.total, 100*float64(a.hit5)/float64(a.total))
}

func TestTheFourArmsOverTheIdenticalCorpus(t *testing.T) {
	client := liveClient(t)
	rows := corpusRows(t)

	bm25, grep, shortlistAlone, judged := &armTally{name: "bm25"}, &armTally{name: "grep"}, &armTally{name: "shortlist"}, &armTally{name: "judged"}
	var totalCost float64
	var latencies []time.Duration

	for _, row := range rows {
		bm25Ranked := BM25Rank(row.Task, row.Files)
		grepRanked := GrepRank(row.Task, row.Files)
		bm25.add(bm25Ranked, row.Label)
		grep.add(grepRanked, row.Label)

		short := Shortlist(row.Files, bm25Ranked, ShortlistSize)
		unreranked := make([]Ranked, len(short))
		for i, f := range short {
			unreranked[i] = Ranked{Path: f.Path, Score: float64(len(short) - i)}
		}
		shortlistAlone.add(unreranked, row.Label)

		answer, err := AskJudged(context.Background(), client, row.Task, short)
		if err != nil {
			t.Fatalf("AskJudged(%s): %v", row.TurnID, err)
		}
		judged.add(answer.Ranked, row.Label)
		totalCost += answer.Cost
		latencies = append(latencies, answer.Latency)
		t.Logf("%-32s bm25 top1 %-16s judged top1 %-16s (choice %s) label %v latency %s cost $%.6f build %s",
			row.TurnID, bm25Ranked[0].Path, answer.Ranked[0].Path, answer.Choice, row.Label, answer.Latency.Round(time.Millisecond), answer.Cost, answer.Build)
	}

	bm25.log(t)
	grep.log(t)
	shortlistAlone.log(t)
	judged.log(t)

	var sum time.Duration
	for _, l := range latencies {
		sum += l
	}
	if len(latencies) > 0 {
		t.Logf("judged arm: %d questions, $%.6f total, $%.6f per question, mean latency %s",
			len(latencies), totalCost, totalCost/float64(len(latencies)), (sum / time.Duration(len(latencies))).Round(time.Millisecond))
	}
}

func TestTheSameQuestionJudgedTwice(t *testing.T) {
	client := liveClient(t)
	rows := corpusRows(t)
	row := rows[0]

	bm25Ranked := BM25Rank(row.Task, row.Files)
	short := Shortlist(row.Files, bm25Ranked, ShortlistSize)

	first, err := AskJudged(context.Background(), client, row.Task, short)
	if err != nil {
		t.Fatalf("first AskJudged: %v", err)
	}
	second, err := AskJudged(context.Background(), client, row.Task, short)
	if err != nil {
		t.Fatalf("second AskJudged: %v", err)
	}

	spread := 0.0
	for i := range first.Ranked {
		diff := first.Ranked[i].Score - second.Ranked[i].Score
		if diff < 0 {
			diff = -diff
		}
		if diff > spread {
			spread = diff
		}
	}
	t.Logf("%s asked twice: first top1 %s (%.2f) second top1 %s (%.2f), largest noul spread %.3f",
		row.TurnID, first.Ranked[0].Path, first.Ranked[0].Score, second.Ranked[0].Path, second.Ranked[0].Score, spread)
}
