package jev

import (
	"context"
	"time"

	"boji/internal/transport"
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
	Build       string
	Alias       string
	Provider    string
	RequestID   string
	TransportID string
	Answers     map[string]Answer
	Usage       Usage
	Attempts    int
	Latency     time.Duration
	Bytes       int
	Raw         []byte
}

type Config struct {
	Wire           Wire
	Meter          *SpendMeter
	ReservePerCall float64
}

type Client struct {
	wire    Wire
	meter   *SpendMeter
	reserve float64
}

func NewClient(config Config) (*Client, error) {
	if config.Wire == nil {
		return nil, transport.Fail("jev.NewClient", transport.KindBadRequest, nil, "the client has no wire")
	}
	if config.Meter != nil && config.ReservePerCall <= 0 {
		return nil, transport.Fail("jev.NewClient", transport.KindBadRequest, nil,
			"a spend meter needs a positive reservation per call")
	}
	return &Client{wire: config.Wire, meter: config.Meter, reserve: config.ReservePerCall}, nil
}

func (c *Client) Caps() WireCaps { return c.wire.Caps() }

func (c *Client) Ask(ctx context.Context, request Request) (Decision, error) {
	alias := c.wire.Model()
	body, err := request.Encode(alias)
	if err != nil {
		return Decision{}, err
	}
	caps := c.wire.Caps()
	if caps.MaxRequestBytes > 0 && len(body) > caps.MaxRequestBytes {
		return Decision{}, transport.Fail("jev.Ask", transport.KindRequestTooLarge, nil,
			"the request is %d bytes and %s accepts at most %d", len(body), caps.Name, caps.MaxRequestBytes)
	}

	if c.meter != nil {
		if err := c.meter.Reserve(c.reserve); err != nil {
			return Decision{}, err
		}
	}

	raw, err := c.wire.Post(ctx, body)
	if err != nil {
		c.release()
		return Decision{}, err
	}
	response, err := Decode(raw.Body)
	if err != nil {
		c.release()
		return Decision{}, err
	}
	if err := Validate(request, response); err != nil {
		c.release()
		return Decision{}, err
	}
	if c.meter != nil {
		c.meter.Settle(c.reserve, response.Usage.Cost)
	}

	return Decision{
		Build:       response.Build,
		Alias:       alias,
		Provider:    response.Provider,
		RequestID:   response.RequestID,
		TransportID: raw.RequestID,
		Answers:     response.Answers,
		Usage:       response.Usage,
		Attempts:    raw.Attempts,
		Latency:     raw.Latency,
		Bytes:       len(body),
		Raw:         response.Raw,
	}, nil
}

func (c *Client) release() {
	if c.meter != nil {
		c.meter.Release(c.reserve)
	}
}
