package crew

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestTheCounterFindsTheWideningsAndTheDeferredQuestionsTheLogsHold(t *testing.T) {
	tally, err := Count(filepath.Join("testdata", "counted"))
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"BOJI-006", "BOJI-011", "BOJI-015"}; !slices.Equal(tally.Widenings, want) {
		t.Fatalf("widenings %v, want %v", tally.Widenings, want)
	}
	if want := []string{"BOJI-006", "BOJI-011", "BOJI-015", "BOJI-020", "BOJI-023"}; !slices.Equal(tally.Deferred, want) {
		t.Fatalf("deferred %v, want %v", tally.Deferred, want)
	}
	if len(tally.Grants) != 0 {
		t.Fatalf("grants %v, want none: no worker asked before widening", tally.Grants)
	}
	t.Logf("widenings %d %v", len(tally.Widenings), tally.Widenings)
	t.Logf("deferred  %d %v", len(tally.Deferred), tally.Deferred)
	t.Logf("grants    %d %v", len(tally.Grants), tally.Grants)
}

func TestATicketWithNeitherShapeCountsZeroForBoth(t *testing.T) {
	dir := t.TempDir()
	clean, err := os.ReadFile(filepath.Join("testdata", "counted", "BOJI-025.md"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "BOJI-025.md"), clean, 0o600); err != nil {
		t.Fatal(err)
	}
	tally, err := Count(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(tally.Widenings)+len(tally.Deferred)+len(tally.Grants) != 0 {
		t.Fatalf("a ticket that declared no open question and widened nothing counted %+v", tally)
	}
}

func TestATypedRowIsCountedWithoutReadingTheProse(t *testing.T) {
	dir := t.TempDir()
	ticket := "---\nid: BOJI-210\nowns:\n  - internal/crew/**\n---\n\n## Log\n\n" +
		Question{Ticket: "BOJI-210", Kind: Grant, Where: "internal/turn/loop.go", Ask: "grant it", Default: "refused"}.Block() +
		"\n" +
		Question{Ticket: "BOJI-210", Kind: Deferred, Where: "internal/turn/loop.go", Ask: "who takes it", Default: "left it"}.Block() +
		"\n" +
		Question{Ticket: "BOJI-210", Kind: Assumption, Ask: "read the brief as the filed owns", Default: "read it that way"}.Block()
	if err := os.WriteFile(filepath.Join(dir, "BOJI-210.md"), []byte(ticket), 0o600); err != nil {
		t.Fatal(err)
	}
	tally, err := Count(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(tally.Grants, []string{"BOJI-210"}) || !slices.Equal(tally.Deferred, []string{"BOJI-210"}) {
		t.Fatalf("typed rows counted as %+v", tally)
	}
	if len(tally.Widenings) != 0 {
		t.Fatalf("a typed grant was read as a widening: %+v", tally)
	}
}
