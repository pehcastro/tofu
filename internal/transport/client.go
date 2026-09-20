package transport

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"hash/fnv"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"tofu/internal/konst"
)

const (
	RequestIDHeader  = "X-Request-Id"
	RetryAfterMillis = "Retry-After-Ms"
	RetryAfter       = "Retry-After"
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
	if config.Growth == 0 {
		config.Growth = 1
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.Sleep == nil {
		config.Sleep = sleep
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
	attempts := c.config.Retries + 1
	var failure error
	var waited time.Duration
	for attempt := 1; attempt <= attempts; attempt++ {
		response, advice, err := c.attempt(ctx, request, id)
		if err == nil {
			response.RequestID = id
			response.Attempts = attempt
			response.Elapsed = c.config.Now().Sub(start)
			return response, nil
		}
		failure = err
		if KindOf(err).Fatal() || attempt == attempts {
			return Response{}, err
		}
		next := c.wait(advice, attempt, id)
		if c.config.TotalWait > 0 && waited+next > c.config.TotalWait {
			return Response{}, failure
		}
		if err := c.config.Sleep(ctx, next); err != nil {
			return Response{}, &Error{Kind: KindTimeout, Op: "transport.Do", RequestID: id,
				Detail: "waiting to retry", Err: err}
		}
		waited += next
	}
	return Response{}, failure
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
	return Response{}, c.retryAfter(httpResponse.Header), &Error{
		Kind:      statusKind(httpResponse.StatusCode),
		Op:        "transport.Do",
		Status:    httpResponse.StatusCode,
		RequestID: id,
		Detail:    detail(body),
	}
}

func (c *Client) wait(advice time.Duration, attempt int, id string) time.Duration {
	if advice > 0 {
		return advice
	}
	base := float64(c.config.Backoff) * math.Pow(c.config.Growth, float64(attempt-1))
	if c.config.MaxBackoff > 0 && base > float64(c.config.MaxBackoff) {
		base = float64(c.config.MaxBackoff)
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(id))
	_, _ = h.Write([]byte{byte(attempt)})
	jitter := float64(h.Sum32()%2000)/1000 - 1
	result := base + base*c.config.JitterFraction*jitter
	if result < 0 {
		result = 0
	}
	return time.Duration(result)
}

func (c *Client) retryAfter(header http.Header) time.Duration {
	if value := strings.TrimSpace(header.Get(RetryAfterMillis)); value != "" {
		if millis, err := strconv.ParseFloat(value, 64); err == nil && millis >= 0 {
			return time.Duration(millis) * time.Millisecond
		}
	}
	value := strings.TrimSpace(header.Get(RetryAfter))
	if value == "" {
		return 0
	}
	if seconds, err := strconv.ParseFloat(value, 64); err == nil && seconds >= 0 {
		return time.Duration(seconds * float64(time.Second))
	}
	if when, err := http.ParseTime(value); err == nil {
		wait := when.Sub(c.config.Now())
		if wait > 0 {
			return wait
		}
	}
	return 0
}

func statusKind(status int) Kind {
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return KindAuth
	case http.StatusPaymentRequired:
		return KindBilling
	case http.StatusNotFound:
		return KindModelAccess
	case http.StatusRequestTimeout:
		return KindTimeout
	case http.StatusRequestEntityTooLarge:
		return KindRequestTooLarge
	case http.StatusTooManyRequests:
		return KindRateLimit
	}
	if status >= 500 {
		return KindProvider
	}
	return KindBadRequest
}

func detail(body []byte) string {
	text := strings.TrimSpace(string(body))
	if len(text) > konst.TransportErrorDetailBytes {
		return text[:konst.TransportErrorDetailBytes]
	}
	return text
}

func sleep(ctx context.Context, wait time.Duration) error {
	if wait <= 0 {
		return nil
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func newRequestID() string {
	raw := make([]byte, konst.TransportRequestIDBytes)
	if _, err := rand.Read(raw); err != nil {
		panic("transport: the system random source failed: " + err.Error())
	}
	return "req-" + hex.EncodeToString(raw)
}
