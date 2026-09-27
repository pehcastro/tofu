package harness

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"tofu/internal/session"
	"tofu/internal/turn"
)

const recordedSessionID = "turn-18d6a5df2caeac68"

func recordedSingleFileSession(t *testing.T, dir string) turn.Row {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("..", "stopcheck", "corpus", recordedSessionID+".json"))
	if err != nil {
		t.Fatalf("read the session recorded under .boji/sessions before the writer changed, kept once in the stopcheck corpus: %v", err)
	}
	var row turn.Row
	if err := json.Unmarshal(body, &row); err != nil {
		t.Fatalf("the recorded session is not a turn row: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, recordedSessionID+".json"), body, 0o600); err != nil {
		t.Fatalf("place the recorded session: %v", err)
	}
	return row
}

func sessionTheLoopWrites(t *testing.T, dir string, at time.Time) turn.Row {
	t.Helper()
	row := turn.Row{
		ID:      "turn-header-and-body",
		Schema:  turn.SchemaVersion,
		At:      at,
		Task:    "read one file and answer",
		Wire:    "anthropic",
		Model:   "claude-opus-5",
		Spend:   turn.SpendSubscription,
		Outcome: turn.OutcomeStopped,
		Steps: []turn.StepRow{
			{
				Index:            1,
				AssistantText:    "I will read the file.",
				StopReason:       "tool_use",
				PromptTokens:     103,
				CompletionTokens: 58,
				ToolCalls:        []turn.ToolCallRow{{Tool: "read", Command: "read notes.txt", ResultBytes: 11210}},
			},
			{
				Index:            2,
				AssistantText:    "the marker is SPARROW-7731",
				StopReason:       "end_turn",
				PromptTokens:     694,
				CompletionTokens: 124,
			},
		},
		WallClockMS: 3999,
	}
	if err := turn.WriteSession(session.NewStore(dir), row); err != nil {
		t.Fatalf("write a session the way the loop writes one: %v", err)
	}
	return row
}

func sameWorkMeasured(t *testing.T, row turn.Row, wantInput int64) {
	t.Helper()
	calls := countToolCalls(row)
	input, output := countTokens(row)
	if calls.Read != 1 || calls.Failed != 0 || input != wantInput || output != 182 {
		t.Fatalf("measured %+v and %d tokens in, %d out, want one read, none failed, %d in and 182 out", calls, input, output, wantInput)
	}
	t.Logf("%s measures %d turns, one read, %d tokens in and %d out", row.ID, len(row.Steps), input, output)
}

func sameRowBesidesAssignedIDs(t *testing.T, read, want turn.Row) {
	t.Helper()
	for _, step := range read.Steps {
		for _, call := range step.ToolCalls {
			if call.ID == "" || call.Call == "" {
				t.Fatalf("step %d read back a call with id %q and call %q, want both assigned by the reader", step.Index, call.ID, call.Call)
			}
		}
	}
	settled := func(row turn.Row) turn.Row {
		steps := make([]turn.StepRow, len(row.Steps))
		for i, step := range row.Steps {
			calls := make([]turn.ToolCallRow, len(step.ToolCalls))
			for j, call := range step.ToolCalls {
				var args bytes.Buffer
				if len(call.Args) > 0 && json.Compact(&args, call.Args) == nil {
					call.Args = args.Bytes()
				}
				call.ID, call.Call = "", ""
				calls[j] = call
			}
			step.ToolCalls = calls
			steps[i] = step
		}
		row.Steps = steps
		return row
	}
	if got, expected := settled(read), settled(want); !reflect.DeepEqual(got, expected) {
		t.Fatalf("read back, ids the reader assigns set aside\n%+v\nwant\n%+v", got, expected)
	}
}

const (
	billedWithoutCache = 797
	billedWithCache    = 2075
)

func TestBilledInputCountsCacheTheWayTheClaudeArmCountsIt(t *testing.T) {
	recorded := recordedSingleFileSession(t, t.TempDir())
	var prompt, cacheRead, cacheWrite int
	for _, step := range recorded.Steps {
		prompt += step.PromptTokens
		cacheRead += step.CacheReadTokens
		cacheWrite += step.CacheWriteTokens
	}
	if cacheRead == 0 || cacheWrite == 0 {
		t.Fatalf("%s reports %d cache reads and %d cache writes, so it cannot show how they are counted", recorded.ID, cacheRead, cacheWrite)
	}

	input, _ := countTokens(recorded)
	want := int64(prompt + cacheRead + cacheWrite)
	if input != want {
		t.Fatalf("billed input %d over a turn that reports %d prompt, %d cache read and %d cache write, want %d: "+
			"ParseClaude adds all three into the same column and the two arms sit in the same table",
			input, prompt, cacheRead, cacheWrite, want)
	}
}

func TestLatestSessionReadsTheHeaderAndBodyShapeTheLoopWritesNow(t *testing.T) {
	dir := t.TempDir()
	written := sessionTheLoopWrites(t, dir, time.Date(2026, 9, 19, 3, 44, 39, 0, time.UTC))

	read, err := LatestSession(dir, written.At.Add(-time.Minute))
	if err != nil {
		t.Fatalf("LatestSession over a directory holding one session in the shape tofu writes: %v", err)
	}
	sameRowBesidesAssignedIDs(t, read, written)
	sameWorkMeasured(t, read, billedWithoutCache)
}

func TestLatestSessionStillReadsARecordedSingleFileSession(t *testing.T) {
	dir := t.TempDir()
	recorded := recordedSingleFileSession(t, dir)

	read, err := LatestSession(dir, recorded.At.Add(-time.Minute))
	if err != nil {
		t.Fatalf("LatestSession over a session recorded before the writer changed: %v", err)
	}
	if read.ID != recordedSessionID || len(read.Steps) != 2 || read.Outcome != turn.OutcomeStopped {
		t.Fatalf("read session %q with outcome %q over %d steps, want %q stopped over 2", read.ID, read.Outcome, len(read.Steps), recordedSessionID)
	}
	if read.Spend != turn.SpendSubscription || read.WallClockMS != 3999 {
		t.Fatalf("read spend %q and wall clock %d ms, want %q and 3999 ms: a recorded session that loses these cannot be measured",
			read.Spend, read.WallClockMS, turn.SpendSubscription)
	}
	sameRowBesidesAssignedIDs(t, read, recorded)
	sameWorkMeasured(t, read, billedWithCache)
}

func TestASessionCarryingAKindThisBuildDoesNotKnowStillReads(t *testing.T) {
	dir := t.TempDir()
	written := sessionTheLoopWrites(t, dir, time.Date(2026, 9, 24, 11, 8, 0, 0, time.UTC))
	unknown := session.EventKind("weather")
	if err := session.NewStore(dir).AppendEvent(written.ID, unknown, map[string]int{"degrees": 12}); err != nil {
		t.Fatalf("append an event of a kind a newer build writes and this one has never seen: %v", err)
	}

	read, err := LatestSession(dir, written.At.Add(-time.Minute))
	if err != nil {
		t.Fatalf("LatestSession over a session carrying one %q event: %v", unknown, err)
	}
	sameRowBesidesAssignedIDs(t, read, written)
}

func TestADirectoryHoldingBothShapesReturnsTheNewerSession(t *testing.T) {
	newerIsWritten := t.TempDir()
	recorded := recordedSingleFileSession(t, newerIsWritten)
	written := sessionTheLoopWrites(t, newerIsWritten, recorded.At.Add(time.Hour))

	read, err := LatestSession(newerIsWritten, recorded.At.Add(-time.Minute))
	if err != nil {
		t.Fatalf("LatestSession over both shapes with the written one newer: %v", err)
	}
	if read.ID != written.ID {
		t.Fatalf("read session %q, want the newer %q", read.ID, written.ID)
	}

	newerIsRecorded := t.TempDir()
	recordedSingleFileSession(t, newerIsRecorded)
	sessionTheLoopWrites(t, newerIsRecorded, recorded.At.Add(-time.Hour))

	read, err = LatestSession(newerIsRecorded, recorded.At.Add(-2*time.Hour))
	if err != nil {
		t.Fatalf("LatestSession over both shapes with the recorded one newer: %v", err)
	}
	if read.ID != recordedSessionID {
		t.Fatalf("read session %q, want the newer %q", read.ID, recordedSessionID)
	}
}
