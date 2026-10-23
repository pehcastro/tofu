package jev

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"tofu/internal/judge/ledger"
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
	Key         string
	Build       string
	Alias       string
	Provider    string
	RequestID   string
	TransportID string
	Answers     map[string]Answer
	Usage       Usage
	Charge      ledger.Charge
	AssetsUsed  json.RawMessage
	Attempts    int
	Latency     time.Duration
	Bytes       int
	Raw         []byte
}

type Config struct {
	Wire Wire
	Key  string
}

type Client struct {
	wire Wire
	key  string
}

func NewClient(config Config) (*Client, error) {
	if config.Wire == nil {
		return nil, transport.Fail("jev.NewClient", transport.KindBadRequest, nil, "the client has no wire")
	}
	return &Client{wire: config.Wire, key: config.Key}, nil
}

func (c *Client) Caps() WireCaps { return c.wire.Caps() }

func (c *Client) Model() string { return c.wire.Model() }

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

	raw, err := c.wire.Post(ctx, body)
	var refused *transport.Error
	if errors.As(err, &refused) && refused.Status != 0 && c.key != "" {
		_ = ledger.Tally(ledger.KeyUse{Key: c.key, Provider: caps.Name, Model: alias, Charge: ledger.ChargeOf(0, 0),
			Failed: &ledger.KeyError{Status: refused.Status, What: cmp.Or(refused.Said, providerMessage(refused.Detail), refused.Code, refused.Kind.String())}})
	}
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
		Key:         c.key,
		Build:       response.Build,
		Alias:       alias,
		Provider:    response.Provider,
		RequestID:   response.RequestID,
		TransportID: raw.RequestID,
		Answers:     response.Answers,
		Usage:       response.Usage,
		Charge:      ledger.ChargeOf(response.Usage.Cost, float64(response.Usage.InputTokens)*caps.ListUSDPerInput),
		AssetsUsed:  response.AssetsUsed,
		Attempts:    raw.Attempts,
		Latency:     raw.Latency,
		Bytes:       len(body),
		Raw:         response.Raw,
	}, nil
}

func providerMessage(body string) string {
	_, message, _ := strings.Cut(body, `"message":"`)
	message, _, _ = strings.Cut(message, `"`)
	return strings.ReplaceAll(message, `\n`, " ")
}
