package readworth

import (
	"testing"

	"tofu/internal/sift"
)

type tallyResult struct {
	correct    int
	total      int
	bytesTotal int
	bytesSaved int
}

func marksFor(rows []Row, mark func(Row) sift.Mark) []sift.Mark {
	marks := make([]sift.Mark, len(rows))
	for i, row := range rows {
		marks[i] = mark(row)
	}
	return marks
}

func tally(rows []Row, marks []sift.Mark) tallyResult {
	var result tallyResult
	for i, row := range rows {
		size := len(row.Paragraph)
		result.total++
		result.bytesTotal += size
		if marks[i].Keep == row.Keep {
			result.correct++
		}
		if !marks[i].Keep {
			result.bytesSaved += size
		}
	}
	return result
}

func (r tallyResult) accuracy() float64 {
	return 100 * float64(r.correct) / float64(r.total)
}

func (r tallyResult) savedPct() float64 {
	return 100 * float64(r.bytesSaved) / float64(r.bytesTotal)
}

var lengthFloors = []int{5, 8, 10, 12, 15, 20, 30}

func logLengthSweep(t *testing.T, rows []Row, prefix string) {
	t.Helper()
	for _, floor := range lengthFloors {
		result := tally(rows, marksFor(rows, func(row Row) sift.Mark { return ArmLength(row, floor) }))
		t.Logf("%slength floor %2d: accuracy %.1f%%, saved %.1f%%", prefix, floor, result.accuracy(), result.savedPct())
	}
}

func TestTheFreeArmsOverTheHandLabelledCorpus(t *testing.T) {
	rows, err := ReadCorpus()
	if err != nil {
		t.Fatal(err)
	}

	keepAll := tally(rows, marksFor(rows, ArmKeepEverything))
	t.Logf("keep everything: accuracy %.1f%%, saved %.1f%%", keepAll.accuracy(), keepAll.savedPct())

	signpost := tally(rows, marksFor(rows, ArmSignpost))
	t.Logf("signpost: accuracy %.1f%%, saved %.1f%%", signpost.accuracy(), signpost.savedPct())

	logLengthSweep(t, rows, "")
}
