package jev

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"tofu/internal/transport"
)

type Raw struct {
	Body      []byte
	RequestID string
	Attempts  int
	Latency   time.Duration
}

type Wire interface {
	Caps() WireCaps
	Model() string
	Post(ctx context.Context, body []byte) (Raw, error)
}

type Decision struct {
	Wire        string
	Build       string
	Alias       string
	Provider    string
	RequestID   string
	TransportID string
	Answers     map[string]Answer
	Usage       Usage
	AssetsUsed  json.RawMessage
	Attempts    int
	Latency     time.Duration
	Bytes       int
	Raw         []byte
}

type Failover struct {
	From, To string
	Cause    error
}

type Config struct {
	Wire       Wire
	Fallback   Wire
	OnFailover func(Failover)
}

type Client struct {
	wire       Wire
	fallback   Wire
	onFailover func(Failover)
}

func NewClient(config Config) (*Client, error) {
	if config.Wire == nil {
		return nil, transport.Fail("jev.NewClient", transport.KindBadRequest, nil, "the client has no wire")
	}
	return &Client{wire: config.Wire, fallback: config.Fallback, onFailover: config.OnFailover}, nil
}

func (c *Client) Caps() WireCaps { return c.wire.Caps() }

func (c *Client) Model() string { return c.wire.Model() }

func (c *Client) Ask(ctx context.Context, request Request) (Decision, error) {
	decision, err := askOn(ctx, c.wire, request)
	if c.fallback == nil || !failsOver(ctx, err) {
		return decision, err
	}
	decision, fallbackErr := askOn(ctx, c.fallback, request)
	if fallbackErr != nil {
		return Decision{}, errors.Join(err, fallbackErr)
	}
	if c.onFailover != nil {
		c.onFailover(Failover{From: c.wire.Caps().Name, To: decision.Wire, Cause: err})
	}
	return decision, nil
}

func askOn(ctx context.Context, wire Wire, request Request) (Decision, error) {
	alias := wire.Model()
	body, err := request.Encode(alias)
	if err != nil {
		return Decision{}, err
	}
	caps := wire.Caps()
	if caps.MaxRequestBytes > 0 && len(body) > caps.MaxRequestBytes {
		return Decision{}, transport.Fail("jev.Ask", transport.KindRequestTooLarge, nil,
			"the request is %d bytes and %s accepts at most %d", len(body), caps.Name, caps.MaxRequestBytes)
	}

	raw, err := wire.Post(ctx, body)
	if err != nil {
		return Decision{}, err
	}
	response, err := Decode(raw.Body)
	if err != nil {
		return Decision{}, err
	}
	if err := Validate(request, response); err != nil {
		return Decision{}, err
	}

	return Decision{
		Wire:        caps.Name,
		Build:       response.Build,
		Alias:       alias,
		Provider:    response.Provider,
		RequestID:   response.RequestID,
		TransportID: raw.RequestID,
		Answers:     response.Answers,
		Usage:       response.Usage,
		AssetsUsed:  response.AssetsUsed,
		Attempts:    raw.Attempts,
		Latency:     raw.Latency,
		Bytes:       len(body),
		Raw:         response.Raw,
	}, nil
}
