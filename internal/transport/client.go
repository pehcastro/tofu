package transport

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"tofu/internal/konst"
)

const (
	RequestIDHeader        = "X-Request-Id"
	RetryAfterMillisHeader = "Retry-After-Ms"
	RetryAfterHeader       = "Retry-After"
)

type Config struct {
	AttemptTimeout time.Duration
	Retries        int
	Backoff        time.Duration
	MaxBackoff     time.Duration
	Growth         float64
	JitterFraction float64
	TotalWait      time.Duration
	Concurrency    int
	HTTP           *http.Client
	Now            func() time.Time
	Sleep          func(context.Context, time.Duration) error
	NewRequestID   func() string
}

type Request struct {
	Method string
	URL    string
	Body   []byte
	Header http.Header
}

type Response struct {
	Status    int
	Body      []byte
	Header    http.Header
	RequestID string
	Attempts  int
	Elapsed   time.Duration
}

type Client struct {
	config Config
	http   *http.Client
	slots  chan struct{}
}

func New(config Config) (*Client, error) {
	if config.AttemptTimeout <= 0 {
		return nil, Fail("transport.New", KindBadRequest, nil, "attempt timeout must be positive")
	}
	if config.Retries < 0 {
		return nil, Fail("transport.New", KindBadRequest, nil, "retries must not be negative")
	}
	if config.Retries > 0 && config.Backoff <= 0 {
		return nil, Fail("transport.New", KindBadRequest, nil, "backoff must be positive when retries are enabled")
	}
	if config.MaxBackoff > 0 && config.MaxBackoff < config.Backoff {
		return nil, Fail("transport.New", KindBadRequest, nil, "max backoff must not be below backoff")
	}
	if config.Growth < 0 {
		return nil, Fail("transport.New", KindBadRequest, nil, "growth must not be negative")
	}
	if config.JitterFraction < 0 {
		return nil, Fail("transport.New", KindBadRequest, nil, "jitter fraction must not be negative")
	}
	if config.TotalWait < 0 {
		return nil, Fail("transport.New", KindBadRequest, nil, "total wait must not be negative")
	}
	if config.Concurrency < 1 {
		return nil, Fail("transport.New", KindBadRequest, nil, "concurrency must be at least one")
	}
	client := config.HTTP
	if client == nil {
		client = &http.Client{}
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.NewRequestID == nil {
		config.NewRequestID = newRequestID
	}
	return &Client{
		config: config,
		http:   client,
		slots:  make(chan struct{}, config.Concurrency),
	}, nil
}

func (c *Client) Do(ctx context.Context, request Request) (Response, error) {
	id := c.config.NewRequestID()
	select {
	case c.slots <- struct{}{}:
	case <-ctx.Done():
		return Response{}, &Error{Kind: KindTimeout, Op: "transport.Do", RequestID: id,
			Detail: "waiting for a concurrency slot", Err: ctx.Err()}
	}
	defer func() { <-c.slots }()

	start := c.config.Now()
	retry := c.config.Retry()
	for attempt := 1; ; attempt++ {
		response, advice, err := c.attempt(ctx, request, id)
		if err == nil {
			response.RequestID = id
			response.Attempts = attempt
			response.Elapsed = c.config.Now().Sub(start)
			return response, nil
		}
		if KindOf(err).Fatal() {
			return Response{}, err
		}
		next, again := retry.Next(attempt, advice, id)
		if !again {
			return Response{}, err
		}
		if paused := c.config.Pause(ctx, next); paused != nil {
			return Response{}, &Error{Kind: KindTimeout, Op: "transport.Do", RequestID: id,
				Detail: "waiting to retry", Err: paused}
		}
	}
}

func (c *Client) attempt(ctx context.Context, request Request, id string) (Response, time.Duration, error) {
	attemptCtx, cancel := context.WithTimeout(ctx, c.config.AttemptTimeout)
	defer cancel()

	httpRequest, err := http.NewRequestWithContext(attemptCtx, request.Method, request.URL, bytes.NewReader(request.Body))
	if err != nil {
		return Response{}, 0, &Error{Kind: KindBadRequest, Op: "transport.Do", RequestID: id, Err: err}
	}
	for name, values := range request.Header {
		for _, value := range values {
			httpRequest.Header.Add(name, value)
		}
	}
	httpRequest.Header.Set(RequestIDHeader, id)

	httpResponse, err := c.http.Do(httpRequest)
	if err != nil {
		kind := KindProvider
		if errors.Is(err, context.DeadlineExceeded) {
			kind = KindTimeout
		}
		if errors.Is(ctx.Err(), context.Canceled) {
			kind = KindUnknown
		}
		return Response{}, 0, &Error{Kind: kind, Op: "transport.Do", RequestID: id, Err: err}
	}
	defer func() { _ = httpResponse.Body.Close() }()

	body, err := io.ReadAll(httpResponse.Body)
	if err != nil {
		kind := KindProvider
		if errors.Is(err, context.DeadlineExceeded) {
			kind = KindTimeout
		}
		return Response{}, 0, &Error{Kind: kind, Op: "transport.Do", Status: httpResponse.StatusCode,
			RequestID: id, Detail: "reading the response body", Err: err}
	}

	if httpResponse.StatusCode >= 200 && httpResponse.StatusCode < 300 {
		return Response{Status: httpResponse.StatusCode, Body: body, Header: httpResponse.Header}, 0, nil
	}
	return Response{}, RetryAfter(httpResponse.Header, c.config.Now()), &Error{
		Kind:      StatusKind(httpResponse.StatusCode),
		Op:        "transport.Do",
		Status:    httpResponse.StatusCode,
		RequestID: id,
		Detail:    detail(body),
	}
}

func detail(body []byte) string {
	text := strings.TrimSpace(string(body))
	if len(text) > konst.TransportErrorDetailBytes {
		return text[:konst.TransportErrorDetailBytes]
	}
	return text
}

func newRequestID() string {
	raw := make([]byte, konst.TransportRequestIDBytes)
	if _, err := rand.Read(raw); err != nil {
		panic("transport: the system random source failed: " + err.Error())
	}
	return "req-" + hex.EncodeToString(raw)
}
