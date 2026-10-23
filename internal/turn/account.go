package turn

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"tofu/internal/konst"
	"tofu/internal/llm"
	"tofu/internal/recall"
	"tofu/internal/transport"
)

type Account struct {
	ID       int64
	Model    Model
	Headroom float64
	Window   string
}

type Accounts struct {
	Pick      func(context.Context) (Account, error)
	Next      func(context.Context, Account) (Account, bool, error)
	Resets    func(context.Context, Account) (time.Time, bool)
	Elsewhere func(ctx context.Context, left []string, conversationTokens int) (Moved, error)
	Keep      func(ctx context.Context, held Held) (kept Account, leftBecause string, err error)
}

type Held struct {
	ID   int64
	Idle time.Duration
}

type Moved struct {
	From, To, Setting string
	Account           Account
	Accounts          Accounts
	Wire              string
	Spend             Spend
	Budget            recall.Budget
}

func (m Moved) words() string {
	return "every " + m.From + " account is at its limit, so this turn continues on " + m.To + ", as " + m.Setting + " says"
}

type RateLimited struct {
	Wire       string
	Tried      []int64
	ResetAt    time.Time
	Cause      error
	Continuity string
}

func (r *RateLimited) Error() string {
	tried := make([]string, len(r.Tried))
	for i, id := range r.Tried {
		tried[i] = "#" + strconv.FormatInt(id, 10)
	}
	said := "every " + r.Wire + " account this turn could move to answered with its rate limit (" + strings.Join(tried, ", ") +
		"), and the soonest resets at " + r.ResetAt.Format(time.RFC3339)
	var refused *transport.Error
	if errors.As(r.Cause, &refused) {
		resetting := *refused
		resetting.ResetAt = r.ResetAt
		said = resetting.Sentence()
		if len(tried) > 1 {
			said += " Every account this turn could move to is limited: " + strings.Join(tried, ", ") + "."
		}
	}
	if r.Continuity != "" {
		said += " " + strings.ToUpper(r.Continuity[:1]) + r.Continuity[1:]
	}
	return said + "\n" + r.Cause.Error()
}

func (r *RateLimited) Unwrap() error { return r.Cause }

func (a Accounts) rotate(ctx context.Context, limited Account, tried []Account) (Account, bool) {
	if a.Next == nil || limited.ID == 0 {
		return Account{}, false
	}
	next, moved, err := a.Next(ctx, limited)
	return next, err == nil && moved && !slices.ContainsFunc(tried, func(spent Account) bool { return spent.ID == next.ID })
}

func (a Accounts) elsewhere(ctx context.Context, left []string, conversationTokens int) (Moved, string, bool) {
	if a.Elsewhere == nil {
		return Moved{}, "", false
	}
	moved, err := a.Elsewhere(ctx, left, conversationTokens)
	if err != nil {
		return Moved{}, err.Error(), false
	}
	return moved, "", true
}

func (a Accounts) limited(ctx context.Context, wire string, tried []Account, attempts []llm.Attempt, now time.Time, cause error) *RateLimited {
	limited := &RateLimited{Wire: wire, Cause: cause}
	for _, account := range tried {
		if account.ID != 0 {
			limited.Tried = append(limited.Tried, account.ID)
		}
		if a.Resets == nil {
			continue
		}
		if at, known := a.Resets(ctx, account); known && (limited.ResetAt.IsZero() || at.Before(limited.ResetAt)) {
			limited.ResetAt = at
		}
	}
	var refused *transport.Error
	if limited.ResetAt.IsZero() && errors.As(cause, &refused) {
		limited.ResetAt = refused.ResetAt
	}
	if !limited.ResetAt.IsZero() {
		return limited
	}
	wait := konst.QuotaCoolCapMinutes * time.Minute
	if len(attempts) > 0 {
		if advised := transport.RetryAfter(http.Header{transport.RetryAfterHeader: {attempts[len(attempts)-1].RetryAfter}}, now); advised > 0 {
			wait = advised
		}
	}
	limited.ResetAt = now.Add(wait)
	return limited
}

func rotatedWords(from, to Account) string {
	return fmt.Sprintf("account #%d answered with its rate limit, so this turn asked again on account #%d of the same subscription, "+
		"which has %.0f%% of its %s window left", from.ID, to.ID, to.Headroom*100, to.Window)
}

func movedAccountWords(from, to Account, freshTokens int) string {
	return fmt.Sprintf(
		"account #%d has no window left, so this session moved to account #%d, which has %.0f%% of its %s window left: "+
			"the move costs one fresh prefix of %d tokens, paid once, because the new session starts on a cold cache",
		from.ID, to.ID, to.Headroom*100, to.Window, freshTokens)
}
