package turn

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strconv"
	"sync"
	"testing"

	"tofu/internal/llm"
	"tofu/internal/recall"
	"tofu/internal/session"
)

const liveSessionsDir = "../../.tofu/sessions"

func TestAStepRowRecordedBeforeThisChangeStillDecodesWithItsWorkingSet(t *testing.T) {
	store := session.NewStore(filepath.FromSlash(liveSessionsDir))
	listing, err := store.Listing()
	if err != nil {
		t.Skipf("the live sessions are not readable from here, so nothing recorded by an earlier build is checked: %v", err)
	}
	sessions, steps, unreadable, first := 0, 0, 0, ""
	for _, header := range listing.Sessions {
		events, err := store.Body(header.ID)
		if err != nil {
			unreadable++
			continue
		}
		measured := 0
		for _, event := range events {
			if event.Kind != session.EventStep {
				continue
			}
			var step StepRow
			if err := json.Unmarshal(event.Body, &step); err != nil {
				t.Fatalf("%s: a recorded step does not parse: %v", header.ID, err)
			}
			var asWritten struct {
				Occupancy json.RawMessage `json:"occupancy"`
			}
			if err := json.Unmarshal(event.Body, &asWritten); err != nil {
				t.Fatalf("%s: a recorded step does not parse: %v", header.ID, err)
			}
			if step.Occupancy == nil {
				continue
			}
			again, err := json.Marshal(step.Occupancy)
			if err != nil {
				t.Fatalf("%s: re-encode a recorded occupancy: %v", header.ID, err)
			}
			if string(again) != string(asWritten.Occupancy) {
				t.Fatalf("%s step %d was written as %s and re-encodes as %s", header.ID, step.Index, asWritten.Occupancy, again)
			}
			if step.Occupancy.WorkingSet > 0 {
				measured++
				if first == "" {
					first = header.ID + " step " + strconv.Itoa(step.Index) + " " + string(asWritten.Occupancy)
				}
			}
		}
		steps += measured
		if measured > 0 {
			sessions++
		}
	}
	if steps == 0 {
		t.Skip("no session on disk records a working set, so nothing older than this change was read back")
	}
	t.Logf("%d steps across %d live sessions decode a working set and re-encode byte for byte, the first of them %s; %d sessions unreadable", steps, sessions, first, unreadable)
}

func TestAMeasuredStepNamesTheCapsItWasMeasuredAgainstOnBothWrites(t *testing.T) {
	store := session.NewStore(t.TempDir())
	tool := &stubTool{name: "read", result: Result{Content: "file contents", Command: "read a.txt"}}
	model := &stubModel{decisions: []llm.Decision{
		toolCallDecision(llm.ToolCall{ID: "call-1", Name: "read", Arguments: json.RawMessage(`{"path":"a.txt"}`)}),
		messageDecision(),
	}}
	config := baseConfig(t, model, NewRegistry(tool))
	config.ArtifactDir = t.TempDir()
	config.Budget = recall.Budget{
		CeilingTokens: 6000,
		Bands:         recall.Bands{Identity: 1000, Facts: 0, WorkingSet: 2000, Recent: 3000},
		Automatic:     true,
		Source:        "the caps this test runs under",
	}
	config.Sessions = store
	config.EndedSession = nil

	row, err := Run(context.Background(), config)
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	events, err := store.Body(row.ID)
	if err != nil {
		t.Fatalf("read the body back: %v", err)
	}
	var live []StepRow
	for _, event := range events {
		if event.Kind != session.EventStep {
			continue
		}
		var step StepRow
		if err := json.Unmarshal(event.Body, &step); err != nil {
			t.Fatalf("a recorded step does not parse: %v", err)
		}
		live = append(live, step)
	}
	if len(live) != len(row.Steps) {
		t.Fatalf("the recorder wrote %d steps and the turn returned %d", len(live), len(row.Steps))
	}

	measured := 0
	for i, step := range row.Steps {
		if step.Occupancy == nil {
			continue
		}
		measured++
		for name, seen := range map[string]*recall.Bands{"the returned row": step.Bands, "the live body": live[i].Bands} {
			if seen == nil {
				t.Fatalf("step %d measured %d tokens and %s names no caps", step.Index, step.Occupancy.Total(), name)
			}
			if *seen != config.Budget.Bands {
				t.Fatalf("step %d: %s names caps %+v against the %+v it ran under", step.Index, name, *seen, config.Budget.Bands)
			}
		}
		if step.Occupancy.Target != config.Budget.Bands.Target() {
			t.Fatalf("step %d marks %d against caps totalling %d", step.Index, step.Occupancy.Target, config.Budget.Bands.Target())
		}
	}
	if measured == 0 {
		t.Fatal("no step recorded an occupancy, so nothing carried caps")
	}
	t.Logf("%d of %d steps carry caps %+v on both writes", measured, len(row.Steps), config.Budget.Bands)
}

func TestAStepFromBeforeTheCapsReadsAsAnAbsenceAndCapsOfZeroDoNot(t *testing.T) {
	shipped := recall.ShippedBands()
	for _, written := range []struct {
		name string
		row  StepRow
	}{
		{"before the caps reached the row", StepRow{Index: 1, Occupancy: &recall.Occupancy{Target: 50000}}},
		{"measured against caps of zero", StepRow{Index: 1, Occupancy: &recall.Occupancy{}, Bands: &recall.Bands{}}},
		{"measured against the shipped caps", StepRow{Index: 1, Occupancy: &recall.Occupancy{}, Bands: &shipped}},
	} {
		raw, err := json.Marshal(written.row)
		if err != nil {
			t.Fatalf("%s: marshal: %v", written.name, err)
		}
		var read StepRow
		if err := json.Unmarshal(raw, &read); err != nil {
			t.Fatalf("%s: unmarshal: %v", written.name, err)
		}
		if (read.Bands == nil) != (written.row.Bands == nil) {
			t.Fatalf("%s: wrote %v and read %v from %s", written.name, written.row.Bands, read.Bands, raw)
		}
		if read.Bands != nil && *read.Bands != *written.row.Bands {
			t.Fatalf("%s: wrote %+v and read %+v", written.name, *written.row.Bands, *read.Bands)
		}
		t.Logf("%s: %s", written.name, raw)
	}
}

func baseConfig(t *testing.T, model Model, tools Registry) Config {
	t.Helper()
	scratch := session.NewStore(t.TempDir())
	return Config{
		Model:          model,
		Spend:          SpendAPIKey,
		Tools:          tools,
		Task:           "say pong",
		Wire:           "anthropic",
		Caps:           Caps{MaxSteps: 10},
		ResultBytesCap: 4096,
		ArtifactDir:    t.TempDir(),
		EndedSession:   func(row Row) error { return WriteSession(scratch, row) },
	}
}

type stubModel struct {
	decisions []llm.Decision
	requests  []llm.Request
	calls     int
}

func (m *stubModel) Ask(_ context.Context, request llm.Request) (llm.Decision, error) {
	m.requests = append(m.requests, request)
	if m.calls >= len(m.decisions) {
		return llm.Decision{}, errors.New("stubModel: no more decisions queued")
	}
	decision := m.decisions[m.calls]
	m.calls++
	return decision, nil
}

type stubTool struct {
	name    string
	result  Result
	err     error
	varying bool
	running sync.Mutex
	calls   int
}

func (t *stubTool) Name() string { return t.name }

func (t *stubTool) Definition() llm.Tool {
	return llm.Tool{Name: t.name, Description: "a stub tool", Parameters: map[string]any{"type": "object"}}
}

func (t *stubTool) Run(_ context.Context, _ json.RawMessage) (Result, error) {
	t.running.Lock()
	t.calls++
	calls := t.calls
	t.running.Unlock()
	result := t.result
	if t.varying {
		result.Content += " " + strconv.Itoa(calls)
	}
	return result, t.err
}

func toolCallDecision(calls ...llm.ToolCall) llm.Decision {
	return llm.Decision{Build: "m1", Outcome: llm.OutcomeToolCalls, ToolCalls: calls}
}

func messageDecision() llm.Decision {
	return llm.Decision{Build: "m1", Outcome: llm.OutcomeMessage, Content: "done"}
}
