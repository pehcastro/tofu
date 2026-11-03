package boardy

import (
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestStoreCounterHandsOutDistinctNumbersUnderContention(t *testing.T) {
	store := Store{Dir: t.TempDir()}
	if _, err := store.Init("RACE", "race"); err != nil {
		t.Fatal(err)
	}
	const writers = 24
	var wait sync.WaitGroup
	ids := make([]string, writers)
	errs := make([]error, writers)
	started := time.Now()
	for i := range writers {
		wait.Go(func() {
			created, err := Local{Store: store}.Create("RACE", Ticket{Front: Front{Title: fmt.Sprint("ticket ", i)}}, "test")
			ids[i], errs[i] = created.ID, err
		})
	}
	wait.Wait()
	if elapsed := time.Since(started); elapsed > lockWait/2 {
		t.Errorf("%d creates took %s, so a released lock was left behind for the next taker", writers, elapsed)
	}
	for i, err := range errs {
		if err != nil {
			t.Fatalf("writer %d: %v", i, err)
		}
	}
	slices.Sort(ids)
	if distinct := slices.Compact(slices.Clone(ids)); len(distinct) != writers {
		t.Fatalf("%d writers got %d distinct numbers: %v", writers, len(distinct), ids)
	}
	tickets, err := store.Tickets("RACE")
	if err != nil || len(tickets) != writers {
		t.Fatalf("read back %d tickets, %v; want %d whole files", len(tickets), err, writers)
	}
	events, err := store.Events("RACE", 0)
	if err != nil || len(events) != writers {
		t.Fatalf("events.jsonl holds %d lines, %v; want %d", len(events), err, writers)
	}
}

func TestTicketSurvivesAWriteAndAParse(t *testing.T) {
	want := Ticket{
		Front: Front{ID: "DEMO-7", Title: "parse: the config", Type: Bug, Status: Blocked, Reason: "waits on DEMO-2", Priority: "P0", Points: 3,
			Owns: []string{"internal/a/**", "docs/x.md"}, Depends: []string{"DEMO-2"}, Labels: []string{"wire"}, Epic: "E1", Sprint: "S4",
			Created: time.Date(2026, 10, 9, 1, 2, 3, 0, time.UTC), Updated: time.Date(2026, 10, 9, 4, 5, 6, 0, time.UTC)},
		Problem: "it drops a key", Scope: "the parser\n\n- one\n- two", Acceptance: []string{"- first round", "- second round"}, Log: "- 2026-10-09 person: done",
	}
	got, err := ParseTicket(want.Markdown())
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprintf("%#v", got) != fmt.Sprintf("%#v", want) {
		t.Fatalf("round trip changed the ticket:\n got %#v\nwant %#v", got, want)
	}
	broken := map[string]string{
		"a revision before the first acceptance": strings.Replace(string(want.Markdown()), "## Acceptance\n", "## Acceptance 3\n", 1),
		"a section twice":                        string(want.Markdown()) + "\n## Scope\n",
		"an unknown front matter key":            strings.Replace(string(want.Markdown()), "epic: E1\n", "epic: E1\ncolour: red\n", 1),
		"no status":                              strings.Replace(string(want.Markdown()), "status: blocked\n", "", 1),
	}
	for name, raw := range broken {
		if _, err := ParseTicket([]byte(raw)); err == nil {
			t.Errorf("%s parsed without complaint", name)
		}
	}
}
