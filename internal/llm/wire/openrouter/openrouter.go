package openrouter

import (
	"cmp"
	"context"
	"errors"
	"net/http"
	"time"

	"tofu/internal/judge/ledger"
	"tofu/internal/llm"
	"tofu/internal/transport"
)

const (
	Name     = llm.WireOpenRouter
	Endpoint = "https://openrouter.ai/api/v1/chat/completions"
)

type Config struct {
	Endpoint  string
	Model     string
	Key       string
	KeyName   string
	Transport transport.Config
}

type Wire struct {
	endpoint string
	model    string
	key      string
	keyName  string
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
	return &Wire{endpoint: endpoint, model: config.Model, key: config.Key, keyName: config.KeyName, client: client}, nil
}

func (w *Wire) Name() string { return Name }

func (w *Wire) Model() string { return w.model }

func (w *Wire) Post(ctx context.Context, body []byte) (llm.Raw, error) {
	marked, err := MarkStablePrefix(body)
	if err != nil {
		return llm.Raw{}, err
	}
	return w.send(ctx, marked)
}

func Headers(key string) []llm.Header {
	return []llm.Header{
		{Name: "Authorization", Value: "Bearer " + key},
		{Name: "Content-Type", Value: "application/json"},
		{Name: "Accept", Value: "application/json"},
	}
}

func (w *Wire) send(ctx context.Context, body []byte) (llm.Raw, error) {
	header := http.Header{}
	for _, pair := range Headers(w.key) {
		header.Set(pair.Name, pair.Value)
	}

	response, err := w.client.Do(ctx, transport.Request{
		Method: http.MethodPost,
		URL:    w.endpoint,
		Body:   body,
		Header: header,
	})
	w.tally(response, err)
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

func (w *Wire) tally(response transport.Response, err error) {
	if w.keyName == "" {
		return
	}
	use := ledger.KeyUse{Key: w.keyName, Provider: Name, Model: w.model, Charge: ledger.ChargeOf(0, 0)}
	var refused *transport.Error
	switch {
	case errors.As(err, &refused) && refused.Status != 0:
		use.Failed = &ledger.KeyError{Status: refused.Status, What: cmp.Or(refused.Said, refused.Code, refused.Kind.String())}
	case err != nil:
		return
	default:
		accounted, err := llm.Accounted(response.Body)
		if err != nil {
			return
		}
		use.Model = cmp.Or(accounted.Build, w.model)
		use.Usage = ledger.Usage{InputTokens: accounted.Usage.InputTokens, OutputTokens: accounted.Usage.OutputTokens,
			CacheReadInputTokens: accounted.CacheReadTokens, CacheWriteInputTokens: accounted.CacheWriteTokens, ReasoningOutputTokens: accounted.Usage.ReasoningTokens}
		use.Charge = ledger.ChargeOf(accounted.Usage.Cost, 0)
		use.UpstreamUSD = accounted.UpstreamCostUSD
		use.Allowances = ledger.HeaderAllowance(response.Header, time.Now())
	}
	_ = ledger.Tally(use)
}
