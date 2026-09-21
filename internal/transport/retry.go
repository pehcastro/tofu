package transport

import (
	"context"
	"hash/fnv"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const jitterSteps = 2000

type Retry struct {
	config Config
	waited time.Duration
}

func (c Config) Retry() *Retry { return &Retry{config: c} }

func (r *Retry) Next(attempt int, advice time.Duration, id string) (time.Duration, bool) {
	if attempt > r.config.Retries {
		return 0, false
	}
	wait := advice
	if wait <= 0 {
		wait = r.config.backoff(attempt, id)
	}
	if r.config.TotalWait > 0 && r.waited+wait > r.config.TotalWait {
		return 0, false
	}
	r.waited += wait
	return wait, true
}

func (c Config) backoff(attempt int, id string) time.Duration {
	growth := c.Growth
	if growth <= 0 {
		growth = 1
	}
	base := float64(c.Backoff) * math.Pow(growth, float64(attempt-1))
	if c.MaxBackoff > 0 && base > float64(c.MaxBackoff) {
		base = float64(c.MaxBackoff)
	}
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(id))
	_, _ = hash.Write([]byte{byte(attempt)})
	spread := float64(hash.Sum32()%jitterSteps)/(jitterSteps/2) - 1
	wait := base + base*c.JitterFraction*spread
	if wait < 0 {
		return 0
	}
	return time.Duration(wait)
}

func (c Config) Pause(ctx context.Context, wait time.Duration) error {
	if c.Sleep != nil {
		return c.Sleep(ctx, wait)
	}
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

func RetryAfter(header http.Header, now time.Time) time.Duration {
	if value := strings.TrimSpace(header.Get(RetryAfterMillisHeader)); value != "" {
		if millis, err := strconv.ParseFloat(value, 64); err == nil && millis >= 0 {
			return time.Duration(millis) * time.Millisecond
		}
	}
	value := strings.TrimSpace(header.Get(RetryAfterHeader))
	if value == "" {
		return 0
	}
	if seconds, err := strconv.ParseFloat(value, 64); err == nil && seconds >= 0 {
		return time.Duration(seconds * float64(time.Second))
	}
	if when, err := http.ParseTime(value); err == nil {
		if wait := when.Sub(now); wait > 0 {
			return wait
		}
	}
	return 0
}
