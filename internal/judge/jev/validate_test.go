package jev

import (
	"strings"
	"testing"

	"tofu/internal/transport"
)

func battery() Request {
	return Request{
		State: map[string]string{"tool": "bash", "input": "ls -la"},
		Questions: []Question{
			{ID: "act", Kind: QuestionChoice, Instructions: "what is this", Options: []Option{
				{Name: "read", Criteria: "it reads"},
				{Name: "write", Criteria: "it writes"},
				{Name: "ask", Criteria: nil},
			}},
			{ID: "risk", Kind: QuestionScore, Instructions: "how risky", Levels: []string{"none", "low", "high", "grave"}},
			{ID: "approval", Kind: QuestionNoul, Instructions: "needs approval", True: "yes", False: "no"},
		},
	}
}

func answers(overrides map[string]Answer) map[string]Answer {
	base := map[string]Answer{
		"act": {Kind: QuestionChoice, Choice: "read", Confidence: 0.9,
			Probabilities: map[string]float64{"read": 0.8, "write": 0.15, "ask": 0.05}},
		"risk": {Kind: QuestionScore, Score: 0, Confidence: 1,
			Probabilities: map[string]float64{"0": 1, "1": 0, "2": 0, "3": 0}},
		"approval": {Kind: QuestionNoul, Noul: 0.11},
	}
	for id, answer := range overrides {
		base[id] = answer
	}
	return base
}

func TestValidate(t *testing.T) {
	cases := []struct {
		name    string
		answers map[string]Answer
		reject  string
	}{
		{
			name:    "a clean battery is accepted",
			answers: answers(nil),
		},
		{
			name: "a choice distribution summing to 0.80 is rejected",
			answers: answers(map[string]Answer{
				"act": {Kind: QuestionChoice, Choice: "read", Confidence: 0.9,
					Probabilities: map[string]float64{"read": 0.6, "write": 0.15, "ask": 0.05}},
			}),
			reject: "has probabilities summing to 0.8",
		},
		{
			name: "a score distribution summing to 0.80 is rejected",
			answers: answers(map[string]Answer{
				"risk": {Kind: QuestionScore, Score: 1, Confidence: 0.5,
					Probabilities: map[string]float64{"0": 0.2, "1": 0.5, "2": 0.1, "3": 0}},
			}),
			reject: "has probabilities summing to 0.7999999999999999",
		},
		{
			name: "a choice absent from the criteria is rejected",
			answers: answers(map[string]Answer{
				"act": {Kind: QuestionChoice, Choice: "delete", Confidence: 0.9,
					Probabilities: map[string]float64{"read": 0.8, "write": 0.15, "ask": 0.05}},
			}),
			reject: "not in its criteria",
		},
		{
			name: "a choice that is not the argmax is rejected",
			answers: answers(map[string]Answer{
				"act": {Kind: QuestionChoice, Choice: "ask", Confidence: 0.9,
					Probabilities: map[string]float64{"read": 0.8, "write": 0.15, "ask": 0.05}},
			}),
			reject: `chose "ask" at 0.05 while "read" has 0.8`,
		},
		{
			name: "a score with the wrong level count is rejected",
			answers: answers(map[string]Answer{
				"risk": {Kind: QuestionScore, Score: 0, Confidence: 1,
					Probabilities: map[string]float64{"0": 0.5, "1": 0.3, "2": 0.2}},
			}),
			reject: "asked for 4 levels and answered with 3",
		},
		{
			name: "a choice summing to 0.99 is accepted",
			answers: answers(map[string]Answer{
				"act": {Kind: QuestionChoice, Choice: "read", Confidence: 0.9,
					Probabilities: map[string]float64{"read": 0.79, "write": 0.15, "ask": 0.05}},
			}),
		},
		{
			name: "a score summing to 0.99 is accepted",
			answers: answers(map[string]Answer{
				"risk": {Kind: QuestionScore, Score: 1.3, Confidence: 0.54,
					Probabilities: map[string]float64{"0": 0.1, "1": 0.55, "2": 0.3, "3": 0.04}},
			}),
		},
		{
			name: "a choice summing to 0.98 is accepted",
			answers: answers(map[string]Answer{
				"act": {Kind: QuestionChoice, Choice: "read", Confidence: 0.9,
					Probabilities: map[string]float64{"read": 0.49, "write": 0.49, "ask": 0}},
			}),
		},
		{
			name: "a noul above one is rejected",
			answers: answers(map[string]Answer{
				"approval": {Kind: QuestionNoul, Noul: 1.4},
			}),
			reject: "outside zero to one",
		},
		{
			name: "an answer of the wrong type is rejected",
			answers: answers(map[string]Answer{
				"approval": {Kind: QuestionChoice, Choice: "read", Confidence: 1,
					Probabilities: map[string]float64{"read": 1}},
			}),
			reject: "is a noul and was answered with a choice",
		},
		{
			name: "a missing answer is rejected",
			answers: map[string]Answer{
				"act": {Kind: QuestionChoice, Choice: "read", Confidence: 0.9,
					Probabilities: map[string]float64{"read": 0.8, "write": 0.15, "ask": 0.05}},
				"risk": {Kind: QuestionScore, Score: 0, Confidence: 1,
					Probabilities: map[string]float64{"0": 1, "1": 0, "2": 0, "3": 0}},
			},
			reject: "2 answers for 3 questions",
		},
		{
			name: "a score outside its level range is rejected",
			answers: answers(map[string]Answer{
				"risk": {Kind: QuestionScore, Score: 3.4, Confidence: 1,
					Probabilities: map[string]float64{"0": 0, "1": 0, "2": 0, "3": 1}},
			}),
			reject: "outside zero to 3",
		},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			err := Validate(battery(), Response{Build: "typesafe/jev-1.13-20260917", Answers: test.answers})
			if test.reject == "" {
				if err != nil {
					t.Fatalf("expected the answer to be accepted, got %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected a rejection mentioning %q, got none", test.reject)
			}
			if kind := transport.KindOf(err); kind != transport.KindInvalidAnswer {
				t.Fatalf("expected kind invalid_answer, got %s", kind)
			}
			if !strings.Contains(err.Error(), test.reject) {
				t.Fatalf("expected a rejection mentioning %q, got %v", test.reject, err)
			}
		})
	}
}

func TestValidateNeverRenormalises(t *testing.T) {
	request := battery()
	response := Response{Build: "b", Answers: answers(map[string]Answer{
		"act": {Kind: QuestionChoice, Choice: "read", Confidence: 0.9,
			Probabilities: map[string]float64{"read": 0.79, "write": 0.15, "ask": 0.05}},
	})}
	if err := Validate(request, response); err != nil {
		t.Fatalf("expected the 0.99 sum to be accepted, got %v", err)
	}
	if got := response.Answers["act"].Probabilities["read"]; got != 0.79 {
		t.Fatalf("validation changed a probability: read is %v", got)
	}
}
