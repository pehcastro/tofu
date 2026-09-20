package turn

import (
	"context"
	"encoding/json"
	"testing"

	"tofu/internal/llm"
	"tofu/internal/recall"
	"tofu/internal/session"
)

func TestAMeasuredStepNamesTheCapsItWasMeasuredAgainstOnBothWrites(t *testing.T) {
	store := session.NewStore(t.TempDir())
	tool := &stubTool{name: "read", result: Result{Content: "file contents", Command: "read a.txt"}}
	model := &stubModel{decisions: []llm.Decision{
		toolCallDecision(llm.ToolCall{ID: "call-1", Name: "read", Arguments: json.RawMessage(`{"path":"a.txt"}`)}),
		messageDecision(),
	}}
	config := baseConfig(t, model, NewRegistry(tool))
	config.ArtifactDir = t.TempDir()
	config.Bands = recall.Bands{Identity: 1000, Facts: 0, WorkingSet: 2000, Recent: 3000}
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
			if *seen != config.Bands {
				t.Fatalf("step %d: %s names caps %+v against the %+v it ran under", step.Index, name, *seen, config.Bands)
			}
		}
		if step.Occupancy.Target != config.Bands.Target() {
			t.Fatalf("step %d marks %d against caps totalling %d", step.Index, step.Occupancy.Target, config.Bands.Target())
		}
	}
	if measured == 0 {
		t.Fatal("no step recorded an occupancy, so nothing carried caps")
	}
	t.Logf("%d of %d steps carry caps %+v on both writes", measured, len(row.Steps), config.Bands)
}

func TestAStepFromBeforeTheCapsReadsAsAnAbsenceAndCapsOfZeroDoNot(t *testing.T) {
	shipped := recall.ShippedBands()
	for _, written := range []struct {
		name string
		row  StepRow
	}{
		{"before the caps reached the row", StepRow{Index: 1, Occupancy: &Occupancy{Target: 50000}}},
		{"measured against caps of zero", StepRow{Index: 1, Occupancy: &Occupancy{}, Bands: &recall.Bands{}}},
		{"measured against the shipped caps", StepRow{Index: 1, Occupancy: &Occupancy{}, Bands: &shipped}},
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
