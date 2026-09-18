package jev

import (
	"bytes"
	"encoding/json"

	"boji/internal/transport"
)

type QuestionKind int

const (
	QuestionNoul QuestionKind = iota
	QuestionChoice
	QuestionScore
)

func (q QuestionKind) String() string {
	switch q {
	case QuestionNoul:
		return "noul"
	case QuestionChoice:
		return "choice"
	case QuestionScore:
		return "score"
	}
	panic("jev: unknown question kind")
}

type Option struct {
	Name     string
	Criteria any
}

type Question struct {
	ID           string
	Kind         QuestionKind
	Instructions string
	Options      []Option
	Levels       []string
	True         string
	False        string
}

type Request struct {
	State     any
	Questions []Question
}

func (r Request) Encode(model string) ([]byte, error) {
	if model == "" {
		return nil, transport.Fail("jev.Encode", transport.KindBadRequest, nil, "the model is empty")
	}
	if len(r.Questions) == 0 {
		return nil, transport.Fail("jev.Encode", transport.KindBadRequest, nil, "the request asks nothing")
	}
	if r.State == nil {
		return nil, transport.Fail("jev.Encode", transport.KindBadRequest, nil, "the request carries no state")
	}

	state, err := json.Marshal(r.State)
	if err != nil {
		return nil, transport.Fail("jev.Encode", transport.KindBadRequest, err, "encoding the state")
	}

	body := &bytes.Buffer{}
	body.WriteString(`{"model":`)
	writeString(body, model)
	body.WriteString(`,"state":`)
	body.Write(state)
	body.WriteString(`,"questions":{`)

	seen := make(map[string]bool, len(r.Questions))
	for index, question := range r.Questions {
		if question.ID == "" {
			return nil, transport.Fail("jev.Encode", transport.KindBadRequest, nil, "question %d has no id", index)
		}
		if seen[question.ID] {
			return nil, transport.Fail("jev.Encode", transport.KindBadRequest, nil, "question id %q appears twice", question.ID)
		}
		seen[question.ID] = true
		if index > 0 {
			body.WriteByte(',')
		}
		writeString(body, question.ID)
		body.WriteByte(':')
		if err := writeQuestion(body, question); err != nil {
			return nil, err
		}
	}
	body.WriteString("}}")
	return body.Bytes(), nil
}

func writeQuestion(body *bytes.Buffer, question Question) error {
	body.WriteString(`{"type":`)
	writeString(body, question.Kind.String())
	body.WriteString(`,"instructions":`)
	writeString(body, question.Instructions)
	body.WriteString(`,"criteria":`)

	switch question.Kind {
	case QuestionNoul:
		body.WriteString(`{"true":`)
		writeString(body, question.True)
		body.WriteString(`,"false":`)
		writeString(body, question.False)
		body.WriteByte('}')
	case QuestionChoice:
		if len(question.Options) == 0 {
			return transport.Fail("jev.Encode", transport.KindBadRequest, nil, "choice %q offers no options", question.ID)
		}
		body.WriteByte('{')
		names := make(map[string]bool, len(question.Options))
		for index, option := range question.Options {
			if option.Name == "" {
				return transport.Fail("jev.Encode", transport.KindBadRequest, nil, "choice %q has an unnamed option", question.ID)
			}
			if names[option.Name] {
				return transport.Fail("jev.Encode", transport.KindBadRequest, nil, "choice %q repeats option %q", question.ID, option.Name)
			}
			names[option.Name] = true
			if index > 0 {
				body.WriteByte(',')
			}
			writeString(body, option.Name)
			body.WriteByte(':')
			criteria, err := json.Marshal(option.Criteria)
			if err != nil {
				return transport.Fail("jev.Encode", transport.KindBadRequest, err, "encoding option %q of %q", option.Name, question.ID)
			}
			body.Write(criteria)
		}
		body.WriteByte('}')
	case QuestionScore:
		if len(question.Levels) == 0 {
			return transport.Fail("jev.Encode", transport.KindBadRequest, nil, "score %q offers no levels", question.ID)
		}
		levels, err := json.Marshal(question.Levels)
		if err != nil {
			return transport.Fail("jev.Encode", transport.KindBadRequest, err, "encoding the levels of %q", question.ID)
		}
		body.Write(levels)
	default:
		panic("jev: unknown question kind")
	}
	body.WriteByte('}')
	return nil
}

func writeString(body *bytes.Buffer, value string) {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic("jev: a string failed to encode: " + err.Error())
	}
	body.Write(encoded)
}
