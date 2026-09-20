package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"tofu/internal/sys"
)

func readStep(t *testing.T, index int, assistantText string, paths ...string) Event {
	t.Helper()
	var calls []map[string]any
	for i, path := range paths {
		calls = append(calls, map[string]any{
			"tool":          "read",
			"args":          map[string]any{"path": path},
			"result_bytes":  100 * (i + 1),
			"result_handle": "artifact-" + path,
		})
	}
	body, err := json.Marshal(map[string]any{
		"index":          index,
		"assistant_text": assistantText,
		"tool_calls":     calls,
	})
	if err != nil {
		t.Fatalf("marshal step: %v", err)
	}
	return Event{Kind: EventStep, Body: body}
}

func recorded(t *testing.T, settings Settings, events ...Event) (*Store, string) {
	t.Helper()
	store := NewStore(t.TempDir())
	store.Use(settings)
	id := "turn-1"
	recorder, err := store.Begin(Header{ID: id, Root: id, At: time.Now()})
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	for _, event := range events {
		var body any
		if err := json.Unmarshal(event.Body, &body); err != nil {
			t.Fatalf("unmarshal event: %v", err)
		}
		if err := recorder.Append(event.Kind, body); err != nil {
			t.Fatalf("append %s: %v", event.Kind, err)
		}
	}
	if err := recorder.End(Header{ID: id, Root: id, At: time.Now()}, map[string]string{"outcome": "stopped"}); err != nil {
		t.Fatalf("end: %v", err)
	}
	return store, id
}

func TestATurnThatReadsThreeFilesRecordsThreeReadsWithTheirPathsAndSizes(t *testing.T) {
	store, id := recorded(t, DefaultSettings(), readStep(t, 1, "looking at the store", "a.go", "b.go"), readStep(t, 2, "", "c.go"))

	reads, err := store.ReadsOf(id)
	if err != nil {
		t.Fatalf("reads of %s: %v", id, err)
	}
	if len(reads.Reads) != 3 {
		t.Fatalf("the record holds %d reads, want 3: %+v", len(reads.Reads), reads.Reads)
	}
	want := []Read{
		{Step: 1, Tool: "read", Source: "a.go", Bytes: 100, Artifact: "artifact-a.go", Reasoning: "looking at the store", ReasoningSource: ReasoningFromAssistantText},
		{Step: 1, Tool: "read", Source: "b.go", Bytes: 200, Artifact: "artifact-b.go", Reasoning: "looking at the store", ReasoningSource: ReasoningFromAssistantText},
		{Step: 2, Tool: "read", Source: "c.go", Bytes: 100, Artifact: "artifact-c.go", ReasoningSource: ReasoningNoneSent},
	}
	for i, read := range reads.Reads {
		if read != want[i] {
			t.Errorf("read %d is %+v, want %+v", i, read, want[i])
		}
	}
	if reads.Unrecorded != 0 {
		t.Errorf("%d reads happened without being recorded, want 0", reads.Unrecorded)
	}
	t.Logf("%+v", reads.Reads)
}

func TestReadsAreFoundByTheirOwnLineWithoutParsingTheWholeBody(t *testing.T) {
	store, id := recorded(t, DefaultSettings(), readStep(t, 1, "", "a.go"))

	body, err := os.ReadFile(filepath.Join(store.dir, id, bodyName))
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	var found []string
	for _, line := range strings.Split(strings.TrimSpace(string(body)), "\n") {
		if strings.HasPrefix(line, `{"kind":"read"`) {
			found = append(found, line)
		}
	}
	if len(found) != 1 {
		t.Fatalf("%d lines start as a read, want 1: %s", len(found), body)
	}
	t.Logf("%s", found[0])
}

func TestRecordingReadsSwitchedOffWritesNoReadAndSaysHowManyWereNotRecorded(t *testing.T) {
	settings := DefaultSettings()
	settings.RecordReads = false
	store, id := recorded(t, settings, readStep(t, 1, "", "a.go", "b.go"), readStep(t, 2, "", "c.go"))

	body, err := os.ReadFile(filepath.Join(store.dir, id, bodyName))
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if strings.Contains(string(body), `"kind":"read"`) {
		t.Fatalf("a read was recorded with the setting off: %s", body)
	}
	reads, err := store.ReadsOf(id)
	if err != nil {
		t.Fatalf("reads of %s: %v", id, err)
	}
	if len(reads.Reads) != 0 {
		t.Fatalf("%d reads came back with the setting off", len(reads.Reads))
	}
	if reads.Unrecorded != 3 {
		t.Fatalf("the record says %d reads went unrecorded, want 3", reads.Unrecorded)
	}
}

func TestReasoningSwitchedOffSaysSoRatherThanLeavingTheFieldEmpty(t *testing.T) {
	settings := DefaultSettings()
	settings.RecordReasoning = false
	store, id := recorded(t, settings, readStep(t, 1, "looking at the store", "a.go"))

	reads, err := store.ReadsOf(id)
	if err != nil {
		t.Fatalf("reads of %s: %v", id, err)
	}
	if len(reads.Reads) != 1 {
		t.Fatalf("%d reads with reasoning off, want the read itself kept", len(reads.Reads))
	}
	read := reads.Reads[0]
	if read.Reasoning != "" {
		t.Errorf("reasoning %q was kept with the setting off", read.Reasoning)
	}
	if read.ReasoningSource != ReasoningNotRecorded {
		t.Errorf("the source is %q, want %q", read.ReasoningSource, ReasoningNotRecorded)
	}
}

func TestTheSessionsAlreadyOnDiskStillReadAndTheirReadsAreCountedRatherThanInvented(t *testing.T) {
	store := NewStore(filepath.Join(sys.StateDir(filepath.Join("..", "..")), "sessions"))
	listing, err := store.Listing()
	if err != nil {
		t.Fatalf("listing the recorded sessions: %v", err)
	}
	if len(listing.Sessions) == 0 {
		t.Skip("skipped: this working copy has no recorded sessions to read")
	}
	kept, unrecorded, readers := 0, 0, 0
	for _, header := range listing.Sessions {
		reads, err := store.ReadsOf(header.ID)
		if err != nil {
			t.Fatalf("reads of %s: %v", header.ID, err)
		}
		kept += len(reads.Reads)
		unrecorded += reads.Unrecorded
		if len(reads.Reads)+reads.Unrecorded > 0 {
			readers++
		}
		if header.Ended() {
			t.Errorf("%s reads back as ended, and nothing has ended a session yet", header.ID)
		}
	}
	t.Logf("%d sessions on disk, %d skipped, %d of them read something, %d reads recorded, %d from before reads were recorded",
		len(listing.Sessions), len(listing.Skipped), readers, kept, unrecorded)
	if unrecorded+kept == 0 {
		t.Fatal("no session on disk read anything, so this proves nothing about the old records")
	}
}

func TestARangedReadAndAnArtifactFetchKeepWhatPartWasRead(t *testing.T) {
	body, err := json.Marshal(map[string]any{
		"index": 3,
		"tool_calls": []map[string]any{
			{"tool": "read", "args": map[string]any{"path": "a.go", "start_line": 12, "end_line": 40}},
			{"tool": "artifact_fetch", "args": map[string]any{"handle": "9f2a", "offset": 100, "length": 50}},
			{"tool": "bash", "args": map[string]any{"command": "ls"}},
		},
	})
	if err != nil {
		t.Fatalf("marshal step: %v", err)
	}
	store, id := recorded(t, DefaultSettings(), Event{Kind: EventStep, Body: body})

	reads, err := store.ReadsOf(id)
	if err != nil {
		t.Fatalf("reads of %s: %v", id, err)
	}
	if len(reads.Reads) != 2 {
		t.Fatalf("%d reads, want 2, and bash is not one: %+v", len(reads.Reads), reads.Reads)
	}
	if reads.Reads[0].Span != "lines 12-40" {
		t.Errorf("the ranged read says %q, want lines 12-40", reads.Reads[0].Span)
	}
	if reads.Reads[1].Source != "9f2a" || reads.Reads[1].Span != "bytes 100-149" {
		t.Errorf("the fetch says %q %q, want 9f2a bytes 100-149", reads.Reads[1].Source, reads.Reads[1].Span)
	}
}
