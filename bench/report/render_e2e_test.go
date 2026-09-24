package report

import (
	"strings"
	"testing"

	"tofu/bench/api"
)

func sweepSection(t *testing.T, points []api.OptionSweepPoint) string {
	t.Helper()
	b := &strings.Builder{}
	renderSweepSection(b, api.Result{OptionSweep: points})
	return b.String()
}

func tableRows(section string) []string {
	var rows []string
	for _, line := range strings.Split(section, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "|") {
			rows = append(rows, trimmed)
		}
	}
	return rows
}

func cellsOf(row string) []string {
	var cells []string
	current := &strings.Builder{}
	escaped := false
	for _, r := range row {
		switch {
		case escaped:
			current.WriteRune(r)
			escaped = false
		case r == '\\':
			escaped = true
		case r == '|':
			cells = append(cells, strings.TrimSpace(current.String()))
			current.Reset()
		default:
			current.WriteRune(r)
		}
	}
	cells = append(cells, strings.TrimSpace(current.String()))
	return cells[1 : len(cells)-1]
}

func resultCellOfTheLastRow(t *testing.T, section string) string {
	t.Helper()
	rows := tableRows(section)
	for column, name := range cellsOf(rows[0]) {
		if name == "Result" {
			return cellsOf(rows[len(rows)-1])[column]
		}
	}
	t.Fatalf("the sweep header has no Result column: %s", rows[0])
	return ""
}

func TestAFailedSweepRowHasTheSameCellCountAsTheHeader(t *testing.T) {
	section := sweepSection(t, []api.OptionSweepPoint{
		{Options: 8, Succeeded: true, Correct: true, LatencyMS: 300, BilledInput: 360, Confidence: 1, Cost: 0.000015},
		{Options: 256, ServerError: "too many choices"},
	})
	t.Logf("\n%s", section)

	rows := tableRows(section)
	header := cellsOf(rows[0])
	for i, row := range rows {
		got := cellsOf(row)
		if len(got) != len(header) {
			t.Errorf("row %d has %d cells against the header's %d: %s", i, len(got), len(header), row)
		}
	}
}

func TestAFailedSweepRowNamesTheServerMessageWhereAReaderLooksForIt(t *testing.T) {
	got := resultCellOfTheLastRow(t, sweepSection(t, []api.OptionSweepPoint{
		{Options: 256, ServerError: "too many choices"},
	}))
	if got != "failed: too many choices" {
		t.Errorf("the Result cell reads %q, so the reason for the failure is not in the column that means something", got)
	}
}

func TestAServerMessageWithAPipeOrANewlineStaysInsideOneCell(t *testing.T) {
	section := sweepSection(t, []api.OptionSweepPoint{
		{Options: 256, ServerError: "choices | exceeded\nretry with fewer"},
	})
	t.Logf("\n%s", section)

	var stray []string
	for _, line := range strings.Split(section, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" && trimmed != "## Option sweep" && !strings.HasPrefix(trimmed, "|") {
			stray = append(stray, trimmed)
		}
	}
	if len(stray) > 0 {
		t.Errorf("the newline in the message put %q outside the table, which ends the table there", stray)
	}

	rows := tableRows(section)
	header := cellsOf(rows[0])
	last := cellsOf(rows[len(rows)-1])
	if len(last) != len(header) {
		t.Errorf("the failed row has %d cells against the header's %d, so the pipe in the message opened a column: %s", len(last), len(header), rows[len(rows)-1])
	}
	if got := resultCellOfTheLastRow(t, section); got != "failed: choices | exceeded retry with fewer" {
		t.Errorf("the Result cell reads %q, so the message did not survive being made safe for a table", got)
	}
}

func TestAFailedSweepRowWithNoServerMessageStillSaysWhyItIsThere(t *testing.T) {
	got := resultCellOfTheLastRow(t, sweepSection(t, []api.OptionSweepPoint{
		{Options: 256},
	}))
	if got != "failed: no server message" {
		t.Errorf("the Result cell of a failure with an empty message reads %q, which leaves a reader with no reason at all", got)
	}
}

func TestASucceededSweepRowOfAllZeroesIsStillAWholeRow(t *testing.T) {
	section := sweepSection(t, []api.OptionSweepPoint{
		{Options: 0, Succeeded: true},
	})
	rows := tableRows(section)
	header := cellsOf(rows[0])
	got := cellsOf(rows[len(rows)-1])
	if len(got) != len(header) {
		t.Errorf("an all-zero success row has %d cells against the header's %d: %s", len(got), len(header), rows[len(rows)-1])
	}
	if strings.Join(got, "|") != "0|ok|0 ms|0|false|0.00|$0.000000" {
		t.Errorf("an all-zero success row reads %q, and a zero that prints as nothing is a hole a reader cannot tell from a missing measurement", strings.Join(got, "|"))
	}
}
