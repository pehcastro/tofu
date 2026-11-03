package turn

import (
	"strings"
	"testing"
)

func TestCompareHeadlinesNamesWhatMoved(t *testing.T) {
	file := Headline{
		EntryCount: 110,
		Parsed:     102,
		Skipped:    8,
		AsRecorded: ReplayArm{Steps: 1007, Calls: 1385, Refusals: 9, BytesReturned: 54267064, WallClockMS: 11435106},
		Layered:    ReplayArm{Steps: 1005, Calls: 1383, CacheHits: 0, Retries: 2, Repaired: 2, Refusals: 9, Undecided: 6, BytesReturned: 54260599, AnswersChanged: 2, WallClockMS: 11423108},
	}
	disk := file
	disk.EntryCount = 111
	disk.Parsed = 103
	disk.AsRecorded.Steps = 1018

	err := CompareHeadlines(file, disk)
	if err == nil {
		t.Fatal("a corpus that grew by one entry and a hundred more parsed turns should not compare equal")
	}
	msg := err.Error()
	for _, want := range []string{
		"entries read: the report says 110, disk computes 111",
		"entries parsed as a recorded turn: the report says 102, disk computes 103",
		"as recorded steps: the report says 1007, disk computes 1018",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("mismatch message %q does not name %q", msg, want)
		}
	}
	t.Logf("real mismatch produced by CompareHeadlines:\n%s", msg)
}

func TestCompareHeadlinesAgreesOnAMatch(t *testing.T) {
	h := Headline{
		EntryCount: 110,
		Parsed:     102,
		Skipped:    8,
		AsRecorded: ReplayArm{Steps: 1007, Calls: 1385, Refusals: 9, BytesReturned: 54267064, WallClockMS: 11435106},
		Layered:    ReplayArm{Steps: 1005, Calls: 1383, Retries: 2, Repaired: 2, Refusals: 9, Undecided: 6, BytesReturned: 54260599, AnswersChanged: 2, WallClockMS: 11423108},
	}
	if err := CompareHeadlines(h, h); err != nil {
		t.Fatalf("identical headlines must not report a mismatch: %v", err)
	}
}

func TestParseHeadlineReadsTheCommittedReport(t *testing.T) {
	body := `# bench turn: replay, 2026-09-21

110 entries read under ` + "`../../.tofu/sessions`" + `: 102 parsed as a recorded turn, 8 skipped.

| as recorded | 1007 | 1385 | - | - | - | 9 | - | 54267064 | 0 | 11435106 |
| with the call memo and the repair rule | 1005 | 1383 | 0 | 2 | 2 | 9 | 6 | 54260599 | 2 | 11423108 |
`
	got, err := ParseHeadline(body)
	if err != nil {
		t.Fatalf("ParseHeadline: %v", err)
	}
	want := Headline{
		EntryCount: 110,
		Parsed:     102,
		Skipped:    8,
		AsRecorded: ReplayArm{Steps: 1007, Calls: 1385, Refusals: 9, BytesReturned: 54267064, WallClockMS: 11435106},
		Layered:    ReplayArm{Steps: 1005, Calls: 1383, Retries: 2, Repaired: 2, Refusals: 9, Undecided: 6, BytesReturned: 54260599, AnswersChanged: 2, WallClockMS: 11423108},
	}
	if err := CompareHeadlines(want, got); err != nil {
		t.Fatalf("ParseHeadline did not round-trip the fixture: %v", err)
	}
}
