package jev

import (
	"encoding/json"

	"tofu/internal/transport"
)

type Usage struct {
	InputTokens  int
	OutputTokens int
	Cost         float64
}

type Answer struct {
	Kind            QuestionKind
	Noul            float64
	Choice          string
	Score           float64
	Probabilities   map[string]float64
	Confidence      float64
	LocalConfidence float64
	Legend          Legend
	Stats           json.RawMessage
}

type Response struct {
	Build      string
	Provider   string
	RequestID  string
	Answers    map[string]Answer
	Usage      Usage
	AssetsUsed json.RawMessage
	Raw        []byte
}

type wireUsage struct {
	InputTokens  int     `json:"input_tokens"`
	OutputTokens int     `json:"output_tokens"`
	Cost         float64 `json:"cost"`
}

type wireAnswer struct {
	Type          string             `json:"type"`
	Noul          *float64           `json:"noul"`
	Choice        *string            `json:"choice"`
	Score         *float64           `json:"score"`
	Probabilities map[string]float64 `json:"probabilities"`
	Confidence    *float64           `json:"confidence"`
	Legend        json.RawMessage    `json:"legend"`
	Stats         json.RawMessage    `json:"stats"`
}

type wireResponse struct {
	Model      string                `json:"model"`
	Provider   string                `json:"provider"`
	ID         string                `json:"id"`
	Answers    map[string]wireAnswer `json:"answers"`
	Usage      wireUsage             `json:"usage"`
	AssetsUsed json.RawMessage       `json:"assets_used"`
}

func Decode(raw []byte) (Response, error) {
	var wire wireResponse
	if err := json.Unmarshal(raw, &wire); err != nil {
		return Response{}, transport.Fail("jev.Decode", transport.KindInvalidAnswer, err, "the response is not the expected object")
	}
	if wire.Model == "" {
		return Response{}, transport.Fail("jev.Decode", transport.KindInvalidAnswer, nil, "the response reports no build id")
	}
	if len(wire.Answers) == 0 {
		return Response{}, transport.Fail("jev.Decode", transport.KindInvalidAnswer, nil, "the response carries no answers")
	}

	answers := make(map[string]Answer, len(wire.Answers))
	for id, raw := range wire.Answers {
		answer, err := decodeAnswer(id, raw)
		if err != nil {
			return Response{}, err
		}
		answers[id] = answer
	}

	return Response{
		Build:      wire.Model,
		Provider:   wire.Provider,
		RequestID:  wire.ID,
		Answers:    answers,
		Usage:      Usage{InputTokens: wire.Usage.InputTokens, OutputTokens: wire.Usage.OutputTokens, Cost: wire.Usage.Cost},
		AssetsUsed: wire.AssetsUsed,
		Raw:        raw,
	}, nil
}

func decodeAnswer(id string, wire wireAnswer) (Answer, error) {
	switch wire.Type {
	case QuestionNoul.String():
		if wire.Noul == nil {
			return Answer{}, transport.Fail("jev.Decode", transport.KindInvalidAnswer, nil, "answer %q is a noul with no value", id)
		}
		return Answer{Kind: QuestionNoul, Noul: *wire.Noul, Stats: wire.Stats}, nil
	case QuestionChoice.String():
		if wire.Choice == nil {
			return Answer{}, transport.Fail("jev.Decode", transport.KindInvalidAnswer, nil, "answer %q is a choice with no option", id)
		}
		if len(wire.Probabilities) == 0 {
			return Answer{}, transport.Fail("jev.Decode", transport.KindInvalidAnswer, nil, "answer %q is a choice with no distribution", id)
		}
		return Answer{
			Kind:            QuestionChoice,
			Choice:          *wire.Choice,
			Probabilities:   wire.Probabilities,
			Confidence:      value(wire.Confidence),
			LocalConfidence: choiceConfidence(wire.Probabilities),
			Stats:           wire.Stats,
		}, nil
	case QuestionScore.String():
		if wire.Score == nil {
			return Answer{}, transport.Fail("jev.Decode", transport.KindInvalidAnswer, nil, "answer %q is a score with no value", id)
		}
		if len(wire.Probabilities) == 0 {
			return Answer{}, transport.Fail("jev.Decode", transport.KindInvalidAnswer, nil, "answer %q is a score with no distribution", id)
		}
		legend, err := decodeLegend(id, wire.Legend)
		if err != nil {
			return Answer{}, err
		}
		return Answer{
			Kind:            QuestionScore,
			Score:           *wire.Score,
			Probabilities:   wire.Probabilities,
			Confidence:      value(wire.Confidence),
			LocalConfidence: scoreConfidence(wire.Probabilities),
			Legend:          legend,
			Stats:           wire.Stats,
		}, nil
	}
	return Answer{}, transport.Fail("jev.Decode", transport.KindInvalidAnswer, nil, "answer %q has unknown type %q", id, wire.Type)
}

func value(pointer *float64) float64 {
	if pointer == nil {
		return 0
	}
	return *pointer
}
