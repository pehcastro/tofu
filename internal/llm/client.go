package llm

import (
	"context"
	"strconv"
	"time"

	"tofu/internal/transport"
)

type Attempts int

const AttemptsUnreported Attempts = 0

func (a Attempts) String() string {
	if a <= AttemptsUnreported {
		return "the wire did not report an attempt count"
	}
	return strconv.Itoa(int(a))
}

type Raw struct {
	Body      []byte
	RequestID string
	Attempts  int
	Latency   time.Duration
}

type Wire interface {
	Name() string
	Model() string
	Post(ctx context.Context, body []byte) (Raw, error)
}

type Decision struct {
	Build            string
	RequestID        string
	TransportID      string
	Attempts         Attempts
	Outcome          Outcome
	Stop             string
	Content          string
	ToolCalls        []ToolCall
	Refusal          string
	Usage            Usage
	PromptAccounting PromptAccounting
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
		Build:            response.Build,
		RequestID:        response.RequestID,
		TransportID:      raw.RequestID,
		Attempts:         Attempts(raw.Attempts),
		Outcome:          response.Outcome,
		Stop:             response.Stop,
		Content:          response.Content,
		ToolCalls:        response.ToolCalls,
		Refusal:          response.Refusal,
		Usage:            response.Usage,
		PromptAccounting: PromptAccountingFor(c.wire.Name()),
		CacheReadTokens:  response.CacheReadTokens,
		Warnings:         response.Warnings,
	}, nil
}
