package shipping

import "testing"

func TestShipLabelsByZone(t *testing.T) {
	carrier := &RecordingCarrier{}
	tracking, err := NewDispatcher(carrier).Ship(Parcel{Order: "o7", Grams: 1200, Country: "fr"})
	if err != nil {
		t.Fatal(err)
	}
	if tracking != "DP1-o7" || len(carrier.Labels) != 1 {
		t.Fatalf("tracking %q, %d labels", tracking, len(carrier.Labels))
	}
}
