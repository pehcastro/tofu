package session

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	originID     = "turn-18d6d295dfac466c"
	forkedID     = "turn-18d6d295dfac466c-f2"
	schemaZeroID = "turn-18d68bcceb3d56e8"
)

func stepEvent(t *testing.T, index int) Event {
	t.Helper()
	body, err := json.Marshal(map[string]int{"index": index})
	if err != nil {
		t.Fatalf("marshal step %d: %v", index, err)
	}
	return Event{Kind: EventStep, Body: body}
}

func chainOfThree(t *testing.T) *Store {
	t.Helper()
	store := NewStore(t.TempDir())
	start := time.Date(2026, 9, 20, 9, 0, 0, 0, time.UTC)
	records := []Header{
		{ID: "a", At: start, Root: "a", ForkedInto: "b", ForkKind: "continuation", Task: "a task"},
		{ID: "b", At: start.Add(time.Minute), Root: "a", Parent: "a", ForkedInto: "c", ForkKind: "continuation", Task: "a task"},
		{ID: "c", At: start.Add(2 * time.Minute), Root: "a", Parent: "b", ForkKind: "continuation", Task: "a task"},
	}
	for i, header := range records {
		if err := store.Write(header, []Event{stepEvent(t, i+1), stepEvent(t, i+2)}); err != nil {
			t.Fatalf("write %s: %v", header.ID, err)
		}
	}
	return store
}

func TestALineageIsWalkedRootToHeadWithOneReadPerSessionAndNoTranscriptRead(t *testing.T) {
	store := chainOfThree(t)
	var paths []string
	inner := store.readFile
	store.readFile = func(path string) ([]byte, error) {
		paths = append(paths, path)
		return inner(path)
	}

	lineage, err := store.Lineage("c")
	if err != nil {
		t.Fatalf("lineage of c: %v", err)
	}

	var walked []string
	for _, header := range lineage {
		walked = append(walked, header.ID)
		if header.Root != "a" {
			t.Errorf("%s names root %q, want a", header.ID, header.Root)
		}
	}
	if strings.Join(walked, " ") != "a b c" {
		t.Fatalf("the lineage walked %v, want a b c, root first", walked)
	}
	if len(paths) != 3 {
		t.Fatalf("walking three sessions took %d reads: %v", len(paths), paths)
	}
	for _, path := range paths {
		if strings.Contains(path, bodyName) {
			t.Fatalf("the walk read a transcript: %s", path)
		}
	}
	t.Logf("three headers, three reads: %v", paths)
}

func TestABodyCutInsideItsLastLineLosesThatLineAndKeepsTheRest(t *testing.T) {
	store := NewStore(t.TempDir())
	if err := store.Write(Header{ID: "a", Root: "a"}, []Event{stepEvent(t, 1), stepEvent(t, 2), stepEvent(t, 3)}); err != nil {
		t.Fatalf("write a: %v", err)
	}
	path := filepath.Join(store.dir, "a", bodyName)
	whole, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the body: %v", err)
	}
	cut := len(whole) - 6
	if whole[cut] == '\n' {
		t.Fatalf("byte %d is the line ending, so this test truncates between lines and proves nothing", cut)
	}
	if err := os.WriteFile(path, whole[:cut], 0o644); err != nil {
		t.Fatalf("truncate the body: %v", err)
	}

	events, err := store.Body("a")
	if err != nil {
		t.Fatalf("read the truncated body: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("a body of three lines cut inside the third gave back %d events", len(events))
	}
	if string(events[1].Body) != `{"index":2}` {
		t.Fatalf("the second event reads %s, so an earlier line did not survive", events[1].Body)
	}
	t.Logf("%d of %d bytes kept, %d whole events", cut, len(whole), len(events))
}

func TestASessionFromBeforeTheHeaderStillReads(t *testing.T) {
	store := NewStore("testdata")

	header, err := store.Header(forkedID)
	if err != nil {
		t.Fatalf("header of %s: %v", forkedID, err)
	}
	if header.Parent != originID {
		t.Errorf("parent = %q, want %s", header.Parent, originID)
	}
	if header.Root != originID {
		t.Errorf("root = %q, want %s", header.Root, originID)
	}
	if header.ForkKind != "continuation" || header.Outcome != "forked" {
		t.Errorf("fork kind %q outcome %q", header.ForkKind, header.Outcome)
	}
	for _, id := range []string{originID, forkedID} {
		recorded, err := store.Header(id)
		if err != nil {
			t.Fatalf("header of %s: %v", id, err)
		}
		if recorded.Outcome != "forked" {
			t.Errorf("%s reads outcome %q, want forked", id, recorded.Outcome)
		}
	}

	events, err := store.Body(forkedID)
	if err != nil {
		t.Fatalf("body of %s: %v", forkedID, err)
	}
	if len(events) < 2 {
		t.Fatalf("an old row produced %d events, want its steps and an outcome", len(events))
	}
	steps, last := events[:len(events)-1], events[len(events)-1]
	for _, event := range steps {
		if event.Kind != EventStep {
			t.Fatalf("an old row produced a %q event before its outcome", event.Kind)
		}
	}
	if last.Kind != EventOutcome {
		t.Fatalf("the last event of an old row is %q, want the outcome", last.Kind)
	}
	var recorded struct {
		Schema      int      `json:"schema"`
		Spend       string   `json:"spend"`
		WallClockMS int64    `json:"wall_clock_ms"`
		DecisionIDs []string `json:"decision_ids"`
	}
	if err := json.Unmarshal(last.Body, &recorded); err != nil {
		t.Fatalf("the outcome of %s does not read back: %v", forkedID, err)
	}
	if recorded.Schema != 1 || recorded.Spend != "subscription" || recorded.WallClockMS != 21179 || len(recorded.DecisionIDs) == 0 {
		t.Fatalf("the outcome reads %+v, and the header has no field for any of it", recorded)
	}
	t.Logf("%s: parent %s root %s, %d steps and an outcome carrying spend %s over %d ms",
		header.ID, header.Parent, header.Root, len(steps), recorded.Spend, recorded.WallClockMS)
}

func TestTheHeadSurvivesARenameThatFails(t *testing.T) {
	store := NewStore(t.TempDir())
	if err := store.Write(Header{ID: "a", Root: "a"}, nil); err != nil {
		t.Fatalf("write a: %v", err)
	}
	if err := store.SetHead("a"); err != nil {
		t.Fatalf("set the head to a: %v", err)
	}

	store.rename = func(string, string) error { return errors.New("the rename failed") }
	if err := store.SetHead("b"); err == nil {
		t.Fatal("setting the head reported success while the rename failed")
	}

	store.rename = os.Rename
	head, err := store.Head()
	if err != nil {
		t.Fatalf("read the head: %v", err)
	}
	if head.ID != "a" || head.Derived {
		t.Fatalf("the head reads %+v, want a, not derived", head)
	}
	entries, err := os.ReadDir(store.dir)
	if err != nil {
		t.Fatalf("read the store directory: %v", err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".tofu-") {
			t.Fatalf("the failed write left %s behind", entry.Name())
		}
	}
}

func TestAMissingOrDanglingHeadFallsBackToTheNewestUnforkedSession(t *testing.T) {
	store := chainOfThree(t)

	head, err := store.Head()
	if err != nil {
		t.Fatalf("head with no pointer: %v", err)
	}
	if head.ID != "c" || !head.Derived {
		t.Fatalf("with no pointer the head reads %+v, want c, derived", head)
	}

	if err := os.WriteFile(filepath.Join(store.dir, headName), []byte("gone\n"), 0o644); err != nil {
		t.Fatalf("write a dangling head: %v", err)
	}
	head, err = store.Head()
	if err != nil {
		t.Fatalf("head with a dangling pointer: %v", err)
	}
	if head.ID != "c" || !head.Derived {
		t.Fatalf("with a dangling pointer the head reads %+v, want c, derived", head)
	}

	if err := store.SetHead("b"); err != nil {
		t.Fatalf("set the head to b: %v", err)
	}
	head, err = store.Head()
	if err != nil {
		t.Fatalf("head after setting it: %v", err)
	}
	if head.ID != "b" || head.Derived {
		t.Fatalf("after setting it the head reads %+v, want b, not derived", head)
	}
}

func TestARecordThatNamesNoRootIsRefused(t *testing.T) {
	store := NewStore(t.TempDir())
	if err := store.Write(Header{ID: "a"}, nil); err == nil {
		t.Fatal("a record with no root was written, and nothing can place it in a lineage")
	}
	if err := store.Write(Header{Root: "a"}, nil); err == nil {
		t.Fatal("a record with no id was written")
	}
}

func TestTheListingReadsBothShapesNewestFirstAndSkipsNothing(t *testing.T) {
	store := chainOfThree(t)
	old, err := os.ReadFile(filepath.Join("testdata", originID+singleFileSuffix))
	if err != nil {
		t.Fatalf("read the old fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(store.dir, originID+singleFileSuffix), old, 0o644); err != nil {
		t.Fatalf("plant the old fixture: %v", err)
	}

	listing, err := store.Listing()
	if err != nil {
		t.Fatalf("listing: %v", err)
	}
	var ids []string
	for _, header := range listing.Sessions {
		ids = append(ids, header.ID)
	}
	if strings.Join(ids, " ") != "c b a "+originID {
		t.Fatalf("the listing reads %v, want c b a then the old session, newest first", ids)
	}
	if listing.Sessions[3].Root != originID {
		t.Errorf("the old session with no parent lists root %q, want its own id", listing.Sessions[3].Root)
	}
	if len(listing.Skipped) != 0 {
		t.Errorf("both shapes are readable and %d were skipped: %v", len(listing.Skipped), listing.Skipped)
	}
}

func TestOneUnreadableFileIsSkippedWithItsReasonAndTheRestStillList(t *testing.T) {
	store := NewStore(t.TempDir())
	start := time.Date(2026, 9, 20, 9, 0, 0, 0, time.UTC)
	for _, header := range []Header{{ID: "a", Root: "a", At: start}, {ID: "b", Root: "b", At: start.Add(time.Minute)}} {
		if err := store.Write(header, []Event{stepEvent(t, 1)}); err != nil {
			t.Fatalf("write %s: %v", header.ID, err)
		}
	}
	if err := os.WriteFile(filepath.Join(store.dir, "turn-torn"+singleFileSuffix), []byte(`{"outcome":`), 0o644); err != nil {
		t.Fatalf("plant a torn row: %v", err)
	}

	listing, err := store.Listing()
	if err != nil {
		t.Fatalf("listing: %v", err)
	}
	var ids []string
	for _, header := range listing.Sessions {
		ids = append(ids, header.ID)
	}
	if strings.Join(ids, " ") != "b a" {
		t.Fatalf("the listing reads %v, want b a, the two readable ones newest first", ids)
	}
	if len(listing.Skipped) != 1 {
		t.Fatalf("%d rows were skipped, want the torn one alone: %v", len(listing.Skipped), listing.Skipped)
	}
	skipped := listing.Skipped[0]
	if skipped.ID != "turn-torn" || skipped.Reason == nil || !strings.Contains(skipped.Reason.Error(), "turn-torn") {
		t.Fatalf("the skip reads %+v, want turn-torn with a reason that names it", skipped)
	}
	t.Logf("%d listed, 1 skipped: %s because %v", len(listing.Sessions), skipped.ID, skipped.Reason)
}

func TestTheNumberAnEarlierSchemaWroteForAnOutcomeReadsAsTheNameSchemaOneWrites(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", schemaZeroID+singleFileSuffix))
	if err != nil {
		t.Fatalf("read the schema 0 fixture: %v", err)
	}
	named := strings.Replace(string(raw), `"Outcome": 1`, `"Outcome": "stopped"`, 1)
	if named == string(raw) {
		t.Fatal("the fixture no longer carries a numeric outcome, so this test proves nothing")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, schemaZeroID+singleFileSuffix), []byte(named), 0o644); err != nil {
		t.Fatalf("plant the named variant: %v", err)
	}

	numeric, err := NewStore("testdata").Header(schemaZeroID)
	if err != nil {
		t.Fatalf("header of the numeric row: %v", err)
	}
	spelled, err := NewStore(dir).Header(schemaZeroID)
	if err != nil {
		t.Fatalf("header of the named row: %v", err)
	}
	if numeric != spelled {
		t.Fatalf("the two spellings read differently:\n numeric %+v\n named   %+v", numeric, spelled)
	}
	if numeric.Outcome != "stopped" {
		t.Fatalf("outcome = %q, want stopped", numeric.Outcome)
	}
	t.Logf("%s reads outcome %q from the number 1 and from the name alike", schemaZeroID, numeric.Outcome)
}

func TestReadingANamedSessionThatIsUnreadableNamesIt(t *testing.T) {
	store := NewStore(t.TempDir())
	if err := os.WriteFile(filepath.Join(store.dir, "turn-torn"+singleFileSuffix), []byte(`{"outcome": 99}`), 0o644); err != nil {
		t.Fatalf("plant a row with an outcome no build names: %v", err)
	}

	_, err := store.Header("turn-torn")
	if err == nil {
		t.Fatal("a row carrying an outcome this build cannot name read back as a header")
	}
	if !strings.Contains(err.Error(), "turn-torn") || strings.Contains(err.Error(), "neither shape") {
		t.Fatalf("the error reads %q, want it to name the row and its real reason rather than an absence", err)
	}
	t.Logf("unreadable named session: %v", err)
}
