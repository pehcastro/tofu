package session

import (
	"encoding/json"
	"testing"
)

func TestEveryOutcomeTheEnumHasCarriesOneNameAndReadsBackAsItself(t *testing.T) {
	named := map[string]Outcome{}
	for outcome := OutcomeUnset; outcome < outcomeCount; outcome++ {
		name := nameOf(t, outcome)
		if name == "" {
			t.Fatalf("outcome %d names itself with the empty string", int(outcome))
		}
		if first, taken := named[name]; taken {
			t.Fatalf("outcome %d and outcome %d both name themselves %q", int(first), int(outcome), name)
		}
		named[name] = outcome

		written, err := json.Marshal(outcome)
		if err != nil {
			t.Fatalf("write outcome %d: %v", int(outcome), err)
		}
		var read Outcome
		if err := json.Unmarshal(written, &read); err != nil {
			t.Fatalf("read %s back: %v", written, err)
		}
		if read != outcome {
			t.Fatalf("outcome %d wrote %s and read back as %d", int(outcome), written, int(read))
		}
	}
	t.Logf("%d outcomes, each with its own name: %v", len(named), named)
}

const recordedBeforeTheWallClockRename = `[
	{"outcome":"unset"},
	{"outcome":"stopped"},
	{"outcome":"step_cap"},
	{"outcome":"cost_cap"},
	{"outcome":"wall_clock_cap"},
	{"outcome":"decision_cap"},
	{"outcome":"forked"},
	{"outcome":"error"}
]`

func TestRowsRecordedBeforeTheRenameStillDecodeToTheSameNumber(t *testing.T) {
	var recorded []struct {
		Outcome Outcome `json:"outcome"`
	}
	if err := json.Unmarshal([]byte(recordedBeforeTheWallClockRename), &recorded); err != nil {
		t.Fatalf("read the rows recorded before the rename: %v", err)
	}
	if len(recorded) > int(outcomeCount) {
		t.Fatalf("the fixture holds %d rows and the enum holds %d outcomes, so the enum lost one", len(recorded), int(outcomeCount))
	}
	for want, row := range recorded {
		if int(row.Outcome) != want {
			t.Fatalf("a row recorded as outcome %d now decodes to %d, named %q", want, int(row.Outcome), row.Outcome)
		}
	}
	if OutcomeRetiredWallClockCap.String() != "wall_clock_cap" {
		t.Fatalf("the retired wall clock outcome writes itself as %q, so an old row no longer round trips", OutcomeRetiredWallClockCap)
	}
}

func nameOf(t *testing.T, outcome Outcome) (name string) {
	t.Helper()
	defer func() {
		if recovered := recover(); recovered != nil {
			t.Fatalf("outcome %d has no name: %v", int(outcome), recovered)
		}
	}()
	return outcome.String()
}
