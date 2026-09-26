package picker

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"tofu/internal/llm/quota"
	"tofu/internal/sys"
)

const prefixPath = "../forkcache/testdata/forks.jsonl"

const fullAgain = 1

var fixtureStart = time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)

func account(row int64, used float64, resets time.Time) quota.Candidate {
	return quota.Candidate{
		ID:       row,
		Provider: quota.ClaudeSub,
		Report: quota.Report{
			Provider: quota.ClaudeSub,
			Windows:  []quota.Window{{ID: "5h", Used: quota.Used{Fraction: used, Reported: true}, ResetsAt: resets}},
		},
	}
}

func snapshot(step int, candidates ...quota.Candidate) Snapshot {
	at := fixtureStart.Add(time.Duration(step) * time.Hour)
	return Snapshot{Provider: quota.ClaudeSub, At: at, Candidates: candidates}
}

func fixtureSnapshots() []Snapshot {
	later := fixtureStart.Add(24 * time.Hour)
	steady := []Snapshot{
		snapshot(0, account(1, 0.10, later), account(2, 0.50, later)),
		snapshot(1, account(1, 0.10, later), account(2, 0.50, later)),
		snapshot(2, account(1, 0.10, later), account(2, 0.50, later)),
	}
	return append(steady,
		snapshot(3, account(1, 1, later), account(2, 0.50, later)),
		snapshot(4, account(1, 0.95, later), account(2, 0.50, later)),
		snapshot(5, account(1, 1, later), account(2, 1, later)),
	)
}

func TestTheArmArithmeticOnASmallFixture(t *testing.T) {
	want := map[Arm]Score{
		FirstUsable: {Arm: FirstUsable, Moves: 2, Walls: 1},
		RoundRobin:  {Arm: RoundRobin, Moves: 4, Walls: 1},
		Headroom:    {Arm: Headroom, Moves: 1, Walls: 1},
	}
	for _, arm := range Arms {
		got := replay(arm, fixtureSnapshots(), nil)
		if got.Moves != want[arm].Moves || got.Walls != want[arm].Walls {
			t.Errorf("%s: %d moves and %d walls, want %d and %d",
				arm, got.Moves, got.Walls, want[arm].Moves, want[arm].Walls)
		}
	}
}

func TestAWindowPastItsResetCountsAsFullAgain(t *testing.T) {
	past := fixtureStart.Add(-time.Hour)
	future := fixtureStart.Add(time.Hour)
	spentButReset := account(1, 0.95, past)
	if left := quota.Left(spentButReset.Report, fixtureStart); left.Fraction != fullAgain {
		t.Fatalf("a window 95 percent used whose reset has passed has headroom %v, want %v", left.Fraction, fullAgain)
	}
	reset := snapshot(0, spentButReset, account(2, 0.40, future))
	if chosen, _ := choose(Headroom, reset, 0); chosen != 1 {
		t.Fatalf("headroom chose row %d, want the account whose window has reset", chosen)
	}
	live := snapshot(0, account(1, 0.95, future), account(2, 0.40, future))
	if chosen, _ := choose(Headroom, live, 0); chosen != 2 {
		t.Fatalf("headroom chose row %d, want the account with room left", chosen)
	}
}

func TestFreshPrefixComesFromTheForkcacheRows(t *testing.T) {
	prefixes, err := FreshPrefixIn(prefixPath)
	if err != nil {
		t.Fatalf("FreshPrefixIn(%q): %v", prefixPath, err)
	}
	if len(prefixes) != 2 {
		t.Fatalf("%d anthropic fork rows, want the 2 TOFU-373 measured", len(prefixes))
	}
	for _, prefix := range prefixes {
		if prefix.Tokens <= 0 {
			t.Fatalf("%s reports %d tokens of fresh prefix", prefix.Child, prefix.Tokens)
		}
	}
}

func TestAReadingMissingAFieldIsSkippedAndNamed(t *testing.T) {
	dir := t.TempDir()
	lines := strings.Join([]string{
		`{"provider":"claude-sub","account":1,"at":"2026-09-22T12:00:00Z","windows":[{"id":"5h","used_fraction":0.1,"resets_at":"2026-09-22T17:00:00Z"}]}`,
		`{"account":2,"at":"2026-09-22T12:00:00Z","windows":[{"id":"5h","used_fraction":0.1,"resets_at":"2026-09-22T17:00:00Z"}]}`,
		`{"provider":"claude-sub","at":"2026-09-22T12:00:00Z","windows":[]}`,
		`{"provider":"claude-sub","account":3,"windows":[]}`,
		`{"provider":"claude-sub","account":4,"at":"2026-09-22T12:00:00Z","windows":[{"id":"5h","resets_at":"2026-09-22T17:00:00Z"}]}`,
		`{"at":"2026-09-22T12:00:00Z","kind":"not a reading at all"}`,
	}, "\n")
	if err := os.WriteFile(filepath.Join(dir, "readings.jsonl"), []byte(lines), 0o600); err != nil {
		t.Fatalf("writing the fixture: %v", err)
	}
	corpus, err := Gather(dir)
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}
	if len(corpus.Readings) != 1 || corpus.Accounts != 1 || corpus.Rows != 6 {
		t.Fatalf("%d readings over %d accounts from %d rows, want 1, 1 and 6",
			len(corpus.Readings), corpus.Accounts, corpus.Rows)
	}
	fields := []string{}
	for _, skip := range corpus.Skips {
		fields = append(fields, skip.Reason)
	}
	want := []string{"provider", "account", "at", "used_fraction"}
	if !slices.Equal(fields, want) {
		t.Fatalf("skipped for %v, want %v", fields, want)
	}
}

func TestEveryRecordedRowIsEitherAReadingOrANamedSkip(t *testing.T) {
	corpus, err := Gather(sys.RecordedStateDir("log"), sys.RecordedStateDir("sessions"))
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}
	if corpus.Rows == 0 {
		t.Fatal("no row was read: the corpus path is wrong or the corpus is empty")
	}
	for _, skip := range corpus.Skips {
		if skip.Reason == "" {
			t.Fatalf("%s was skipped with no field named", skip.Path)
		}
	}
}

func TestTheReportOverTheRecordedCorpus(t *testing.T) {
	result, err := Run(prefixPath, sys.RecordedStateDir("log"), sys.RecordedStateDir("sessions"))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	rendered := Render(result)
	if result.Separable {
		t.Fatal("the recorded corpus now separates the arms: rewrite the report, it says it cannot")
	}
	for _, want := range []string{"cannot separate", "what it would take", "no number is offered", "fresh prefix"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("the report carries no %q line", want)
		}
	}
	t.Log("\n" + rendered)
}

func TestTheInstrumentSeparatesTheArmsWhenTwoAccountsAreRecorded(t *testing.T) {
	prefixes, err := FreshPrefixIn(prefixPath)
	if err != nil {
		t.Fatalf("FreshPrefixIn: %v", err)
	}
	result := Result{
		Corpus:      Corpus{Roots: []string{"a hand built fixture, not the recorded corpus"}, Accounts: 2},
		Snapshots:   fixtureSnapshots(),
		FreshPrefix: prefixes,
		PrefixPath:  prefixPath,
		Separable:   true,
		ReadAt:      fixtureStart,
	}
	for _, arm := range Arms {
		result.Scores = append(result.Scores, replay(arm, result.Snapshots, prefixes))
	}
	rendered := Render(result)
	if strings.Contains(rendered, notSeparable) {
		t.Fatal("the table refused to score a fixture that holds two accounts")
	}
	t.Log("\n" + rendered)
}
