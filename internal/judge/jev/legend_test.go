package jev

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestTheRecordedCassetteCarriesItsLegendLevelByLevel(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "adapter-cassette-response.json"))
	if err != nil {
		t.Fatal(err)
	}
	response, err := Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	want := Legend{
		"The reviewer condemns the book and urges readers to avoid it.",
		"The reviewer is mostly critical and does not recommend the book.",
		"The reviewer expresses mixed or neutral feelings about the book.",
		"The reviewer praises the book overall while noting meaningful flaws.",
		"The reviewer offers unreserved praise and an emphatic recommendation.",
	}
	if got := response.Answers["rating"].Legend; !slices.Equal(got, want) {
		t.Fatalf("the recorded legend decodes to %#v, want the five levels it was sent", got)
	}
	if got := response.Answers["genre"].Legend; got != nil {
		t.Errorf("the choice answer carries a legend %#v, and only a score has one", got)
	}
}

func TestAMalformedLegendIsRefusedRatherThanDropped(t *testing.T) {
	for _, bad := range []struct {
		name   string
		legend string
	}{
		{"a level that is not a number", `{"none":"safe","1":"grave"}`},
		{"a level past the last one", `{"0":"safe","7":"grave"}`},
		{"a word that is empty", `{"0":"","1":"grave"}`},
		{"a word that is not a string", `{"0":3,"1":"grave"}`},
	} {
		t.Run(bad.name, func(t *testing.T) {
			if _, err := decodeLegend("risk", json.RawMessage(bad.legend)); err == nil {
				t.Fatalf("decoding %s was accepted", bad.legend)
			}
		})
	}
}

func TestAnAbsentLegendIsNoLegendAndNotAnError(t *testing.T) {
	for _, absent := range []string{"", "null"} {
		legend, err := decodeLegend("risk", json.RawMessage(absent))
		if err != nil {
			t.Fatalf("%q was read as a failure: %v", absent, err)
		}
		if legend != nil {
			t.Errorf("%q decoded to %#v, want no legend at all", absent, legend)
		}
	}
}
