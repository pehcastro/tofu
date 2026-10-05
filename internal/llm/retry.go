package llm

import (
	"context"
	"errors"
	"io"
	"net/http"
	"time"

	"tofu/internal/konst"
	"tofu/internal/transport"
)

type retrying struct {
	base http.RoundTripper
	plan transport.Config
}

func StreamingClient(given *http.Client, plan transport.Config, headersWithin time.Duration) *http.Client {
	client := &http.Client{}
	if given != nil {
		*client = *given
	}
	if client.Transport == nil {
		bounded := http.DefaultTransport.(*http.Transport).Clone()
		bounded.ResponseHeaderTimeout = headersWithin
		client.Transport = bounded
	}
	if plan.Retries > 0 {
		client.Transport = retrying{base: client.Transport, plan: plan}
	}
	return client
}

func (r retrying) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.Body != nil && request.GetBody == nil {
		return r.base.RoundTrip(request)
	}
	retry := r.plan.Retry()
	for attempt := 1; ; attempt++ {
		sending := request.Clone(request.Context())
		if attempt > 1 {
			body, err := request.GetBody()
			if err != nil {
				return nil, err
			}
			sending.Body = body
		}
		response, err := r.base.RoundTrip(sending)
		advice, worth := worthRepeating(response, err)
		if !worth {
			return response, err
		}
		wait, again := retry.Next(attempt, advice, request.URL.Host)
		if !again {
			return response, err
		}
		if response != nil && response.Body != nil {
			_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, konst.TransportErrorDetailBytes))
			_ = response.Body.Close()
		}
		if paused := r.plan.Pause(request.Context(), wait); paused != nil {
			return nil, transport.Fail("llm.StreamingClient", transport.KindTimeout, paused, "waiting to send the request again")
		}
	}
}

func RetryQuiet[T any](ctx context.Context, plan transport.Config, onRetry func(), post func() (T, error)) (T, error) {
	retry := plan.Retry()
	for attempt := 1; ; attempt++ {
		result, err := post()
		if !errors.Is(err, ErrStreamBroke) || ctx.Err() != nil {
			return result, err
		}
		wait, again := retry.Next(attempt, 0, "")
		if !again || plan.Pause(ctx, wait) != nil {
			return result, err
		}
		if onRetry != nil {
			onRetry()
		}
	}
}

func worthRepeating(response *http.Response, err error) (time.Duration, bool) {
	if err != nil {
		return 0, !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded)
	}
	if response.StatusCode < http.StatusBadRequest || transport.StatusKind(response.StatusCode).Fatal() {
		return 0, false
	}
	return transport.RetryAfter(response.Header, time.Now()), true
}
