package recall_test

import (
	"encoding/json"
	"testing"

	"tofu/internal/recall"
)

const recordedOccupancyBytes = `{"identity":1200,"facts":340,"working_set":19000,"recent":31483,"target":50000}`

func TestARecordedOccupancyDecodesWithEveryBandIntact(t *testing.T) {
	var read recall.Occupancy
	if err := json.Unmarshal([]byte(recordedOccupancyBytes), &read); err != nil {
		t.Fatalf("a recorded occupancy does not parse: %v", err)
	}
	want := recall.Occupancy{Identity: 1200, Facts: 340, WorkingSet: 19000, Recent: 31483, Target: 50000}
	if read != want {
		t.Fatalf("%s reads back as %+v, and the bands it lost are the largest ones on most rows", recordedOccupancyBytes, read)
	}

	raw, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("marshal an occupancy: %v", err)
	}
	if string(raw) != recordedOccupancyBytes {
		t.Fatalf("an occupancy encodes as %s and a recorded row carries %s", raw, recordedOccupancyBytes)
	}
}
