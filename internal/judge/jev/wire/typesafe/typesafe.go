package typesafe

import (
	"context"
	"net/http"

	"tofu/internal/judge/jev"
	"tofu/internal/konst"
	"tofu/internal/transport"
)

const (
	Name            = "typesafe"
	Endpoint        = "https://api.typesafe.ai/v1/systemone"
	Alias           = "jev-latest"
	RequestIDHeader = "x-typesafe-request-id"

	DollarsPerInputToken = 42.0 / 1e9
)

type Config struct {
	Endpoint  string
	Alias     string
	Key       string
	Transport transport.Config
}

type Wire struct {
	endpoint string
	alias    string
	key      string
	client   *transport.Client
}

func New(config Config) (*Wire, error) {
	if config.Key == "" {
		return nil, transport.Fail("typesafe.New", transport.KindMissingCredential, nil, "the wire has no credential")
	}
	endpoint := config.Endpoint
	if endpoint == "" {
		endpoint = Endpoint
	}
	alias := config.Alias
	if alias == "" {
		alias = Alias
	}
	client, err := transport.New(config.Transport)
	if err != nil {
		return nil, err
	}
	return &Wire{endpoint: endpoint, alias: alias, key: config.Key, client: client}, nil
}

func (w *Wire) Model() string { return w.alias }

func (w *Wire) Caps() jev.WireCaps {
	return jev.WireCaps{
		Name:              Name,
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

	response, err := w.client.Do(ctx, transport.Request{
		Method: http.MethodPost,
		URL:    w.endpoint,
		Body:   body,
		Header: header,
	})
	if err != nil {
		return jev.Raw{}, err
	}
	served := response.Header.Get(RequestIDHeader)
	if served == "" {
		served = response.RequestID
	}
	return jev.Raw{
		Body:      response.Body,
		RequestID: served,
		Attempts:  response.Attempts,
		Latency:   response.Elapsed,
	}, nil
}
