package jev

import (
	"encoding/json"
	"strings"
	"testing"

	"tofu/internal/transport"
)

func TestEncodeKeepsOptionOrderAndCriteriaKinds(t *testing.T) {
	request := Request{
		State: map[string]any{"tool": "bash"},
		Questions: []Question{
			{ID: "act", Kind: QuestionChoice, Instructions: "what is this", Options: []Option{
				{Name: "read", Criteria: "it reads"},
				{Name: "write", Criteria: map[string]any{"what": "it writes"}},
				{Name: "ask", Criteria: nil},
			}},
			{ID: "approval", Kind: QuestionNoul, Instructions: "needs approval", True: "yes", False: "no"},
			{ID: "risk", Kind: QuestionScore, Instructions: "how risky", Levels: []string{"none", "grave"}},
		},
	}

	body, err := request.Encode("~typesafe/jev-latest")
	if err != nil {
		t.Fatalf("encoding: %v", err)
	}
	if !json.Valid(body) {
		t.Fatalf("the body is not valid json: %s", body)
	}
	text := string(body)
	for _, want := range []string{
		`"model":"~typesafe/jev-latest"`,
		`"act":{"type":"choice","instructions":"what is this","criteria":{"read":"it reads","write":{"what":"it writes"},"ask":null}}`,
		`"approval":{"type":"noul","instructions":"needs approval","criteria":{"true":"yes","false":"no"}}`,
		`"risk":{"type":"score","instructions":"how risky","criteria":["none","grave"]}`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected %s in the body, got %s", want, text)
		}
	}
	if strings.Index(text, `"act"`) > strings.Index(text, `"risk"`) {
		t.Fatalf("question order was not kept: %s", text)
	}
}

func TestEncodeRefusesABrokenRequest(t *testing.T) {
	cases := []struct {
		name    string
		request Request
		reject  string
	}{
		{"no questions", Request{State: "x"}, "asks nothing"},
		{"no state", Request{Questions: []Question{{ID: "a", Kind: QuestionNoul}}}, "no state"},
		{"an unnamed question", Request{State: "x", Questions: []Question{{Kind: QuestionNoul}}}, "has no id"},
		{"a repeated id", Request{State: "x", Questions: []Question{
			{ID: "a", Kind: QuestionNoul}, {ID: "a", Kind: QuestionNoul}}}, "appears twice"},
		{"a choice with no options", Request{State: "x", Questions: []Question{
			{ID: "a", Kind: QuestionChoice}}}, "offers no options"},
		{"a score with no levels", Request{State: "x", Questions: []Question{
			{ID: "a", Kind: QuestionScore}}}, "offers no levels"},
		{"a repeated option", Request{State: "x", Questions: []Question{
			{ID: "a", Kind: QuestionChoice, Options: []Option{{Name: "one"}, {Name: "one"}}}}}, "repeats option"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			_, err := test.request.Encode("~typesafe/jev-latest")
			if err == nil {
				t.Fatalf("expected a refusal mentioning %q", test.reject)
			}
			if kind := transport.KindOf(err); kind != transport.KindBadRequest {
				t.Fatalf("expected kind bad_request, got %s", kind)
			}
			if !strings.Contains(err.Error(), test.reject) {
				t.Fatalf("expected a refusal mentioning %q, got %v", test.reject, err)
			}
		})
	}
}

func TestDecodeReadsTheMeasuredResponse(t *testing.T) {
	response, err := Decode([]byte(gateReply))
	if err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if response.Build != "typesafe/jev-1.13-20260917" {
		t.Fatalf("build is %q", response.Build)
	}
	if response.Provider != "TypeSafe" {
		t.Fatalf("provider is %q", response.Provider)
	}
	if response.Usage.InputTokens != 776 || response.Usage.OutputTokens != 69 {
		t.Fatalf("usage is %+v", response.Usage)
	}
	if response.Answers["risk"].Kind != QuestionScore || string(response.Answers["risk"].Legend) != `{"0":"none"}` {
		t.Fatalf("the score answer is %+v", response.Answers["risk"])
	}
}

func TestDecodeRefusesAResponseWithoutABuild(t *testing.T) {
	_, err := Decode([]byte(`{"answers":{"a":{"type":"noul","noul":0.5}}}`))
	if transport.KindOf(err) != transport.KindInvalidAnswer {
		t.Fatalf("expected kind invalid_answer, got %v", err)
	}
	if !strings.Contains(err.Error(), "no build id") {
		t.Fatalf("expected the build id to be named, got %v", err)
	}
}

func TestDecodeRefusesAnUnknownAnswerType(t *testing.T) {
	_, err := Decode([]byte(`{"model":"b","answers":{"a":{"type":"vibe","noul":0.5}}}`))
	if transport.KindOf(err) != transport.KindInvalidAnswer {
		t.Fatalf("expected kind invalid_answer, got %v", err)
	}
	if !strings.Contains(err.Error(), `unknown type "vibe"`) {
		t.Fatalf("expected the type to be named, got %v", err)
	}
}

func TestDecodeRefusesANoulWithNoValue(t *testing.T) {
	_, err := Decode([]byte(`{"model":"b","answers":{"a":{"type":"noul"}}}`))
	if !strings.Contains(err.Error(), "noul with no value") {
		t.Fatalf("expected the missing value to be named, got %v", err)
	}
}
