package openrouter

import (
	"context"
	"net/http"

	"tofu/internal/llm"
	"tofu/internal/transport"
)

const (
	Name     = "openrouter"
	Endpoint = "https://openrouter.ai/api/v1/chat/completions"
)

type Config struct {
	Endpoint  string
	Model     string
	Key       string
	Transport transport.Config
}

type Wire struct {
	endpoint string
	model    string
	key      string
	client   *transport.Client
}

func New(config Config) (*Wire, error) {
	if config.Key == "" {
		return nil, transport.Fail("openrouter.New", transport.KindMissingCredential, nil, "the wire has no credential")
	}
	if config.Model == "" {
		return nil, transport.Fail("openrouter.New", transport.KindBadRequest, nil, "the wire has no model")
	}
	endpoint := config.Endpoint
	if endpoint == "" {
		endpoint = Endpoint
	}
	client, err := transport.New(config.Transport)
	if err != nil {
		return nil, err
	}
	return &Wire{endpoint: endpoint, model: config.Model, key: config.Key, client: client}, nil
}

func (w *Wire) Model() string { return w.model }

func (w *Wire) Post(ctx context.Context, body []byte) (llm.Raw, error) {
	marked, err := MarkStablePrefix(body)
	if err != nil {
		return llm.Raw{}, err
	}
	return w.send(ctx, marked)
}

func (w *Wire) send(ctx context.Context, body []byte) (llm.Raw, error) {
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
		return llm.Raw{}, err
	}
	return llm.Raw{
		Body:      response.Body,
		RequestID: response.RequestID,
		Attempts:  response.Attempts,
		Latency:   response.Elapsed,
	}, nil
}
