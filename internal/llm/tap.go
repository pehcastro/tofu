package llm

import (
	"bytes"
	"cmp"
	"compress/flate"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"tofu/internal/konst"
)

type Attempt struct {
	At         time.Time `json:"at"`
	URL        string    `json:"url"`
	Body       []byte    `json:"-"`
	Status     int       `json:"status,omitempty"`
	RequestID  string    `json:"provider_request_id,omitempty"`
	RetryAfter string    `json:"retry_after,omitempty"`
	HeadersMS  int64     `json:"headers_ms"`
	StreamMS   int64     `json:"stream_ms,omitempty"`
	Received   int64     `json:"received_bytes,omitempty"`
	ErrorBody  string    `json:"error_body,omitempty"`
	Error      string    `json:"error,omitempty"`
}

type Tap struct {
	mu       sync.Mutex
	attempts []Attempt
}

type tapKey struct{}

func Tapped(ctx context.Context) (context.Context, *Tap) {
	tap := &Tap{}
	return context.WithValue(ctx, tapKey{}, tap), tap
}

func (t *Tap) Attempts() []Attempt {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]Attempt(nil), t.attempts...)
}

type tapping struct {
	base http.RoundTripper
}

func (r tapping) RoundTrip(request *http.Request) (*http.Response, error) {
	tap, tapped := request.Context().Value(tapKey{}).(*Tap)
	if !tapped {
		return r.base.RoundTrip(request)
	}
	attempt := Attempt{At: time.Now(), URL: request.URL.String()}
	if request.GetBody != nil {
		if body, err := request.GetBody(); err == nil {
			attempt.Body, _ = io.ReadAll(body)
		}
	}
	response, err := r.base.RoundTrip(request)
	attempt.HeadersMS = time.Since(attempt.At).Milliseconds()
	if err != nil {
		attempt.Error = err.Error()
	}
	if response != nil {
		attempt.Status = response.StatusCode
		attempt.RequestID = cmp.Or(response.Header.Get("request-id"), response.Header.Get("x-request-id"))
		attempt.RetryAfter = response.Header.Get("retry-after")
		if response.StatusCode >= http.StatusBadRequest {
			raw, _ := io.ReadAll(io.LimitReader(response.Body, konst.TransportErrorDetailBytes))
			_ = response.Body.Close()
			response.Body = io.NopCloser(bytes.NewReader(raw))
			attempt.ErrorBody = readable(response.Header.Get("Content-Encoding"), raw)
		}
	}
	tap.mu.Lock()
	tap.attempts = append(tap.attempts, attempt)
	index := len(tap.attempts) - 1
	tap.mu.Unlock()
	if response != nil && response.StatusCode < http.StatusBadRequest {
		response.Body = &tappedBody{ReadCloser: response.Body, tap: tap, index: index, started: attempt.At}
	}
	return response, err
}

func readable(encoding string, raw []byte) string {
	var reader io.Reader
	switch strings.ToLower(strings.TrimSpace(encoding)) {
	case "gzip":
		unzipped, err := gzip.NewReader(bytes.NewReader(raw))
		if err != nil {
			return string(raw)
		}
		reader = unzipped
	case "deflate":
		reader = flate.NewReader(bytes.NewReader(raw))
	default:
		return string(raw)
	}
	text, err := io.ReadAll(reader)
	if err != nil {
		return string(raw)
	}
	return string(text)
}

type tappedBody struct {
	io.ReadCloser
	tap     *Tap
	index   int
	started time.Time
}

func (b *tappedBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	b.tap.mu.Lock()
	defer b.tap.mu.Unlock()
	attempt := &b.tap.attempts[b.index]
	attempt.Received += int64(n)
	attempt.StreamMS = time.Since(b.started).Milliseconds()
	if err != nil && !errors.Is(err, io.EOF) {
		attempt.Error = err.Error()
	}
	return n, err
}
