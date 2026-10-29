package ledger

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newestByEveryRow(t *testing.T, dir string, filter Filter) (Row, bool) {
	t.Helper()
	var newest Row
	var found bool
	if _, err := NewReader(dir).Each(filter, func(row Row) error {
		if !found || !row.At.Before(newest.At) {
			newest, found = row, true
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return newest, found
}

func TestNewestIsTheRowEveryRowWouldFindWhateverTheFileEndsIn(t *testing.T) {
	day := time.Date(2026, 10, 28, 9, 0, 0, 0, time.UTC)
	wide := make([]Answer, 3000)
	for i := range wide {
		wide[i] = Answer{Question: "q", Kind: AnswerChoice, Choice: "yes", Dist: []Slice{{Option: "yes", P: 1}}}
	}
	cases := []struct {
		name   string
		filter Filter
		mangle func(newest string) string
	}{
		{name: "no newline after the last row", mangle: func(newest string) string { return strings.TrimSuffix(newest, "\n") }},
		{name: "blank lines after the last row", mangle: func(newest string) string { return newest + "\n\n\n" }},
		{name: "the last row cut short by a crash", mangle: func(newest string) string { return newest + `{"id":"2026-10-29-cut","schema":1,"poi` }},
		{name: "a newer schema at the end", mangle: func(newest string) string {
			return newest + `{"id":"2026-10-29-next","schema":999,"point":"tool_gate","at":"2026-10-29T23:00:00Z"}` + "\n"
		}},
		{name: "windows line endings", mangle: func(newest string) string { return strings.ReplaceAll(newest, "\n", "\r\n") }},
		{name: "a row wider than one read is the newest", mangle: func(newest string) string {
			ended := strings.TrimSuffix(newest, "\n")
			return ended[:strings.LastIndex(ended, "\n")+1]
		}},
		{name: "the point only on an older day", filter: Filter{Point: "done_review"}},
		{name: "a point never written", filter: Filter{Point: "never"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			at := day
			writer := NewWriterWithClock(dir, func() time.Time { return at })
			write := func(point, build string, answers []Answer) Row {
				row, err := writer.Append(Row{Point: point, Questions: point, Version: 1, Build: build, Answers: answers, Reason: &Reason{Mode: ModeShadow}})
				if err != nil {
					t.Fatal(err)
				}
				at = at.Add(time.Minute)
				return row
			}
			write("done_review", "jev-old", nil)
			at = day.Add(24 * time.Hour)
			for i := range 200 {
				write("tool_gate", "jev-"+strings.Repeat("x", i%7), nil)
			}
			write("tool_gate", "jev-wide", wide)
			last := write("tool_gate", "jev-new", nil)
			if err := writer.Backfill(last.ID, Outcome{Kind: "confirmed"}); err != nil {
				t.Fatal(err)
			}
			if c.mangle != nil {
				path := filepath.Join(dir, last.Day()+logSuffix)
				raw, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(c.mangle(string(raw))), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			want, wantFound := newestByEveryRow(t, dir, c.filter)
			got, found, err := NewReader(dir).Newest(c.filter)
			if err != nil || found != wantFound || got.ID != want.ID || got.Build != want.Build || (got.Outcome == nil) != (want.Outcome == nil) {
				t.Fatalf("Newest gave %s %s found %v outcome %v, %v; every row gives %s %s found %v outcome %v", got.ID, got.Build, found, got.Outcome, err, want.ID, want.Build, wantFound, want.Outcome)
			}
		})
	}
	t.Run("no ledger yet", func(t *testing.T) {
		if row, found, err := NewReader(filepath.Join(t.TempDir(), "absent")).Newest(Filter{}); found || err != nil {
			t.Fatalf("an absent ledger gave %v found %v, %v", row.ID, found, err)
		}
	})
}
