package llm

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
	Model() string
	Post(ctx context.Context, body []byte) (Raw, error)
}

type Decision struct {
	Build            string
	RequestID        string
	TransportID      string
	Outcome          Outcome
	Stop             string
	Content          string
	ToolCalls        []ToolCall
	Refusal          string
	Usage            Usage
	CacheReadTokens  int
	CacheWriteTokens int
	Warnings         []string
}

type Client struct {
	wire Wire
}

func NewClient(wire Wire) (*Client, error) {
	if wire == nil {
		return nil, transport.Fail("llm.NewClient", transport.KindBadRequest, nil, "the client has no wire")
	}
	return &Client{wire: wire}, nil
}

func (c *Client) Ask(ctx context.Context, request Request) (Decision, error) {
	body, err := request.Encode(c.wire.Model())
	if err != nil {
		return Decision{}, err
	}

	raw, err := c.wire.Post(ctx, body)
	if err != nil {
		return Decision{}, err
	}
	response, err := Decode(raw.Body)
	if err != nil {
		return Decision{}, err
	}

	return Decision{
		Build:       response.Build,
		RequestID:   response.RequestID,
		TransportID: raw.RequestID,
		Outcome:     response.Outcome,
		Content:     response.Content,
		ToolCalls:   response.ToolCalls,
		Refusal:     response.Refusal,
		Usage:       response.Usage,
	}, nil
}
