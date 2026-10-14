package turn

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"tofu/internal/konst"
	"tofu/internal/llm"
	"tofu/internal/llm/wire/codex"
	"tofu/internal/transport"
)

type answersWithItsLimit struct{ asked int }

func (m *answersWithItsLimit) Ask(context.Context, llm.Request) (llm.Decision, error) {
	m.asked++
	return llm.Decision{}, &transport.Error{Kind: transport.KindRateLimit, Op: "anthropic.Ask", Status: 429, Detail: "rate_limit_error"}
}

type answersDone struct{ asked int }

func (m *answersDone) Ask(context.Context, llm.Request) (llm.Decision, error) {
	m.asked++
	return llm.Decision{Build: "stub", Outcome: llm.OutcomeMessage, Content: "done"}, nil
}

func TestRotateAccountsOnARateLimitAndStopWhenEveryAccountIsTried(t *testing.T) {
	now := time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC)
	soon, later := now.Add(40*time.Minute), now.Add(3*time.Hour)
	for _, c := range []struct {
		name     string
		accounts map[int64]Model
		next     map[int64]int64
		nextErr  bool
		resets   map[int64]time.Time
		moved    []AccountTaken
		tried    []int64
		resetAt  time.Time
	}{
		{name: "the second account serves", accounts: map[int64]Model{1: &answersWithItsLimit{}, 2: &answersDone{}}, next: map[int64]int64{1: 2},
			moved: []AccountTaken{{ID: 1, Reason: AccountPicked}, {ID: 2, From: 1, Reason: AccountMoved}}},
		{name: "next offers the account that just failed", accounts: map[int64]Model{1: &answersWithItsLimit{}}, next: map[int64]int64{1: 1},
			resets: map[int64]time.Time{1: later}, moved: []AccountTaken{{ID: 1, Reason: AccountPicked}}, tried: []int64{1}, resetAt: later},
		{name: "both accounts are spent and the soonest reset wins", accounts: map[int64]Model{1: &answersWithItsLimit{}, 2: &answersWithItsLimit{}}, next: map[int64]int64{1: 2, 2: 1},
			resets: map[int64]time.Time{1: later, 2: soon}, moved: []AccountTaken{{ID: 1, Reason: AccountPicked}, {ID: 2, From: 1, Reason: AccountMoved}}, tried: []int64{1, 2}, resetAt: soon},
		{name: "the windows cannot be read", accounts: map[int64]Model{1: &answersWithItsLimit{}}, nextErr: true,
			moved: []AccountTaken{{ID: 1, Reason: AccountPicked}}, tried: []int64{1}, resetAt: now.Add(konst.QuotaCoolCapMinutes * time.Minute)},
	} {
		t.Run(c.name, func(t *testing.T) {
			config := baseConfig(t, nil, NewRegistry())
			config.Spend, config.Now = SpendSubscription, func() time.Time { return now }
			nexts := 0
			config.Accounts = Accounts{
				Pick: func(context.Context) (Account, error) { return Account{ID: 1, Model: c.accounts[1]}, nil },
				Next: func(_ context.Context, pinned Account) (Account, bool, error) {
					nexts++
					if c.nextErr {
						return Account{}, false, errors.New("the quota shelf is locked")
					}
					id, found := c.next[pinned.ID]
					return Account{ID: id, Model: c.accounts[id], Headroom: 0.5, Window: "5h"}, found, nil
				},
				Resets: func(_ context.Context, account Account) (time.Time, bool) {
					at, known := c.resets[account.ID]
					return at, known
				},
			}
			var taken []AccountTaken
			config.AccountTaken = func(account AccountTaken) { taken = append(taken, account) }
			row, err := Run(context.Background(), config)
			if !slices.Equal(taken, c.moved) || nexts > len(c.accounts) {
				t.Errorf("accounts taken %+v after %d calls to next, want %+v and at most %d calls", taken, nexts, c.moved, len(c.accounts))
			}
			if c.tried == nil {
				if err != nil || row.Outcome != OutcomeStopped || row.Account != 2 || !slices.ContainsFunc(row.Warnings, func(w string) bool { return strings.Contains(w, "account #1 answered with its rate limit") }) {
					t.Fatalf("got %v, outcome %s on account %d, warnings %q: want the turn finished on account 2 and saying why", err, row.Outcome, row.Account, row.Warnings)
				}
				return
			}
			var limited *RateLimited
			if !errors.As(err, &limited) || transport.KindOf(err) != transport.KindRateLimit || !slices.Equal(limited.Tried, c.tried) || !limited.ResetAt.Equal(c.resetAt) {
				t.Fatalf("got %v (%+v), want a rate limit after trying %v that resets at %s", err, limited, c.tried, c.resetAt)
			}
			t.Logf("%s: %v", c.name, err)
		})
	}
}

func TestLimitOnAFixedModelWaitsForItsRetryAfter(t *testing.T) {
	now := time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC)
	limited := Accounts{}.limited(context.Background(), "openrouter", []Account{{}}, []llm.Attempt{{Status: 429, RetryAfter: "30"}}, now, errors.New("429"))
	if !limited.ResetAt.Equal(now.Add(30*time.Second)) || len(limited.Tried) != 0 {
		t.Fatalf("got %+v, want a reset 30 s out from the Retry-After and no account named", limited)
	}
}

func TestLimitCodesInTheCodexStreamAreRateLimits(t *testing.T) {
	for code, want := range map[string]transport.Kind{
		"usage_limit_reached":     transport.KindRateLimit,
		"rate_limit_exceeded":     transport.KindRateLimit,
		"context_length_exceeded": transport.KindRequestTooLarge,
		"invalid_prompt":          transport.KindProvider,
	} {
		stream := `data: {"type":"response.failed","response":{"error":{"code":"` + code + `","message":"the limit"}}}` + "\n"
		if _, err := codex.ReadStream(strings.NewReader(stream), nil); transport.KindOf(err) != want {
			t.Errorf("%s read as %s, want %s: %v", code, transport.KindOf(err), want, err)
		}
	}
}
