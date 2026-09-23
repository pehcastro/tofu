package prefix

import (
	"bytes"
	"encoding/json"
	"fmt"

	"tofu/internal/llm"
	"tofu/internal/llm/wire/anthropic"
)

const bytesPerEstimatedToken = 4

const measureModel = "claude-opus-4-1-20250805"

type RewriteFigure struct {
	Bytes     int
	Tokens    int
	Rewritten bool
}

func fingerprintedFirstMessage(charAtIndex7 byte) string {
	body := []byte("0123456x0123456789012345")
	body[7] = charAtIndex7
	return string(body)
}

func encodedSystemArray(system string, firstMessage string, oauth bool) ([]byte, error) {
	request := anthropic.Request{
		Model:    measureModel,
		System:   []string{system},
		Messages: []llm.Message{{Role: llm.RoleUser, Content: firstMessage}},
	}
	body, err := request.Encode(oauth)
	if err != nil {
		return nil, err
	}
	var decoded struct {
		System json.RawMessage `json:"system"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		return nil, fmt.Errorf("decoding the encoded request: %w", err)
	}
	return decoded.System, nil
}

func MeasureRewrite(system string, oauth bool) (RewriteFigure, error) {
	a, err := encodedSystemArray(system, fingerprintedFirstMessage('a'), oauth)
	if err != nil {
		return RewriteFigure{}, err
	}
	b, err := encodedSystemArray(system, fingerprintedFirstMessage('b'), oauth)
	if err != nil {
		return RewriteFigure{}, err
	}
	if len(a) != len(b) {
		return RewriteFigure{}, fmt.Errorf("two first messages of equal length produced system arrays of %d and %d bytes, the measurement assumes equal length", len(a), len(b))
	}
	return RewriteFigure{Bytes: len(a), Tokens: len(a) / bytesPerEstimatedToken, Rewritten: !bytes.Equal(a, b)}, nil
}
