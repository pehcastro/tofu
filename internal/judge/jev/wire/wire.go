package wire

import (
	"cmp"
	"context"
	"net/http"

	"tofu/internal/judge/jev"
	"tofu/internal/konst"
	"tofu/internal/transport"
)

type Spec struct {
	Name            string
	Endpoint        string
	Alias           string
	FailOp          string
	RequestIDHeader string
}

type Config struct {
	Endpoint  string
	Key       string
	Transport transport.Config
}

type Wire struct {
	spec   Spec
	key    string
	client *transport.Client
}

func New(spec Spec, config Config) (*Wire, error) {
	if config.Key == "" {
		return nil, transport.Fail(spec.FailOp, transport.KindMissingCredential, nil, "the wire has no credential")
	}
	client, err := transport.New(config.Transport)
	if err != nil {
		return nil, err
	}
	spec.Endpoint = cmp.Or(config.Endpoint, spec.Endpoint)
	return &Wire{spec: spec, key: config.Key, client: client}, nil
}

func (w *Wire) Model() string { return w.spec.Alias }

func (w *Wire) Caps() jev.WireCaps {
	return jev.WireCaps{
		Name:              w.spec.Name,
		MaxStateTokens:    konst.JudgeStateTokenCeiling,
		MaxRequestTokens:  konst.JudgeRequestTokenCeiling,
		MaxRequestBytes:   jev.EstimateBytes(konst.JudgeStateTokenCeiling),
		CriteriaKinds:     []jev.CriteriaKind{jev.CriteriaString, jev.CriteriaObject, jev.CriteriaNull},
		MaxChoiceOptions:  konst.ChoiceCeiling,
		MaxScoreLevels:    konst.JudgeScoreLevelCeiling,
		ReturnsConfidence: true,
	}
}

func (w *Wire) Post(ctx context.Context, body []byte) (jev.Raw, error) {
	header := http.Header{}
	header.Set("Authorization", "Bearer "+w.key)
	header.Set("Content-Type", "application/json")
	header.Set("Accept", "application/json")
	response, err := w.client.Do(ctx, transport.Request{Method: http.MethodPost, URL: w.spec.Endpoint, Body: body, Header: header})
	if err != nil {
		return jev.Raw{}, err
	}
	return jev.Raw{
		Body:      response.Body,
		RequestID: cmp.Or(response.Header.Get(w.spec.RequestIDHeader), response.RequestID),
		Attempts:  response.Attempts,
		Latency:   response.Elapsed,
	}, nil
}
