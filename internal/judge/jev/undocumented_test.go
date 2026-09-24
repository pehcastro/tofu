package jev

import (
	"encoding/json"
	"os"
	"testing"
)

func TestTheUndocumentedFieldsSurviveTheRoundTrip(t *testing.T) {
	body, err := os.ReadFile("testdata/adapter-cassette-response.json")
	if err != nil {
		t.Fatalf("reading the cassette: %v", err)
	}
	response, err := Decode(body)
	if err != nil {
		t.Fatalf("decoding the cassette: %v", err)
	}
	if response.Build != "speed_v12_snowy_flower" {
		t.Fatalf("expected the internal build id the answer reports, got %q", response.Build)
	}
	for id, answer := range response.Answers {
		if string(answer.Stats) != "{}" {
			t.Fatalf("answer %q lost its stats, it holds %q", id, answer.Stats)
		}
	}
	if string(response.AssetsUsed) != "null" {
		t.Fatalf("the response lost assets_used, it holds %q", response.AssetsUsed)
	}

	again, err := json.Marshal(map[string]json.RawMessage{
		"stats":       response.Answers["genre"].Stats,
		"assets_used": response.AssetsUsed,
	})
	if err != nil {
		t.Fatalf("re-encoding: %v", err)
	}
	if string(again) != `{"assets_used":null,"stats":{}}` {
		t.Fatalf("the round trip produced %s", again)
	}
}

const gateReply = `{"model":"typesafe/jev-1.13-20260917",
 "answers":{
  "act":{"type":"choice","choice":"read","probabilities":{"read":0.8,"write":0.15,"ask":0.05},"confidence":0.75},
  "risk":{"type":"score","score":0,"legend":{"0":"none"},"probabilities":{"0":1,"1":0,"2":0,"3":0},"confidence":1},
  "approval":{"type":"noul","noul":0.11}},
 "usage":{"input_tokens":776,"output_tokens":69,"cost":0.000032592},
 "id":"gen-dec-1789749261-nXRyUDXmVQe5htOwSK6M",
 "provider":"TypeSafe"}`

func TestAResponseWithoutTheUndocumentedFieldsDecodes(t *testing.T) {
	response, err := Decode([]byte(gateReply))
	if err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if response.AssetsUsed != nil {
		t.Fatalf("expected assets_used absent, got %q", response.AssetsUsed)
	}
	for id, answer := range response.Answers {
		if answer.Stats != nil {
			t.Fatalf("answer %q invented stats: %q", id, answer.Stats)
		}
	}
}
