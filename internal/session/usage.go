package session

import (
	"cmp"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"
)

const (
	hoursInADay  = 24
	daysInAWeek  = 7
	daysInAMonth = 30
)

type UsageSpan string

const (
	UsageDay   UsageSpan = "day"
	UsageWeek  UsageSpan = "week"
	UsageMonth UsageSpan = "month"
)

func (s UsageSpan) starts(now time.Time) []time.Time {
	year, month, day := now.Date()
	count, back := daysInAWeek, func(steps int) time.Time { return time.Date(year, month, day-steps, 0, 0, 0, 0, now.Location()) }
	switch s {
	case UsageDay:
		hour := time.Date(year, month, day, now.Hour(), 0, 0, 0, now.Location())
		count, back = hoursInADay, func(steps int) time.Time { return hour.Add(-time.Duration(steps) * time.Hour) }
	case UsageWeek:
	case UsageMonth:
		count = daysInAMonth
	default:
		panic("session: unknown usage span " + string(s))
	}
	starts := make([]time.Time, count)
	for i := range starts {
		starts[i] = back(count - 1 - i)
	}
	return starts
}

type UsageRole string

const (
	UsageLead       UsageRole = "lead"
	UsageSubAgent   UsageRole = "sub-agent"
	UsageClassifier UsageRole = "classifier"
)

type UsageTotals struct {
	TokensIn    int     `json:"tokens_in"`
	TokensOut   int     `json:"tokens_out"`
	CacheRead   int     `json:"cache_read"`
	CacheWrite  int     `json:"cache_write"`
	Requests    int     `json:"requests"`
	TurnMS      int64   `json:"turn_ms"`
	CostUSD     float64 `json:"cost_usd"`
	SiftedBytes int     `json:"sifted_bytes"`
}

func (u *UsageTotals) Add(more UsageTotals) {
	u.TokensIn, u.TokensOut, u.CacheRead, u.CacheWrite = u.TokensIn+more.TokensIn, u.TokensOut+more.TokensOut, u.CacheRead+more.CacheRead, u.CacheWrite+more.CacheWrite
	u.Requests, u.TurnMS, u.CostUSD, u.SiftedBytes = u.Requests+more.Requests, u.TurnMS+more.TurnMS, u.CostUSD+more.CostUSD, u.SiftedBytes+more.SiftedBytes
}

type UsageBucket struct {
	Start time.Time `json:"start"`
	UsageTotals
}

type UsageSpender struct {
	Session string    `json:"session,omitempty"`
	Role    UsageRole `json:"role"`
	Agent   string    `json:"agent,omitempty"`
	Wire    string    `json:"wire,omitempty"`
	Spend   string    `json:"spend,omitempty"`
	Account int64     `json:"account,omitempty"`
	Model   string    `json:"model,omitempty"`
}

type UsageRow struct {
	UsageSpender
	UsageTotals
}

type UsageHistory struct {
	Span     UsageSpan     `json:"span"`
	Buckets  []UsageBucket `json:"buckets"`
	Total    UsageTotals   `json:"total"`
	Spenders []UsageRow    `json:"spenders"`
	Skipped  []Skip        `json:"-"`
}

type ClassifierCall struct {
	At      time.Time
	Turn    string
	CostUSD float64
}

type spentBody struct {
	Model            string  `json:"model"`
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	CacheReadTokens  int     `json:"cache_read_tokens"`
	CacheWriteTokens int     `json:"cache_write_tokens"`
	CostUSD          float64 `json:"cost_usd"`
	SiftSavedBytes   int     `json:"sift_saved_bytes"`
	WallClockMS      int64   `json:"wall_clock_ms"`
}

func spentIn(event Event) spentBody {
	var body spentBody
	_ = json.Unmarshal(event.Body, &body)
	return body
}

func (s *Store) UsageHistory(span UsageSpan, now time.Time, classifier []ClassifierCall) (UsageHistory, error) {
	listing, err := s.Listing()
	if err != nil {
		return UsageHistory{}, err
	}
	history := UsageHistory{Span: span, Skipped: listing.Skipped}
	starts := span.starts(now)
	for _, start := range starts {
		history.Buckets = append(history.Buckets, UsageBucket{Start: start})
	}
	spent := map[UsageSpender]*UsageTotals{}
	count := func(at time.Time, spender UsageSpender, totals UsageTotals) {
		if at.Before(starts[0]) || at.After(now) || totals == (UsageTotals{}) {
			return
		}
		history.Buckets[sort.Search(len(starts), func(i int) bool { return starts[i].After(at) })-1].Add(totals)
		history.Total.Add(totals)
		if spent[spender] == nil {
			spent[spender] = &UsageTotals{}
		}
		spent[spender].Add(totals)
	}
	sessionOf := map[string]string{}
	for _, header := range listing.Sessions {
		if header.EndedAt != nil && header.EndedAt.Before(starts[0]) {
			continue
		}
		events, err := s.Events(header.ID)
		if err != nil {
			history.Skipped = append(history.Skipped, Skip{ID: header.ID, Reason: err})
			continue
		}
		definitions := map[string]string{}
		for _, run := range header.Agents {
			definitions[run.Agent] = cmp.Or(run.Definition, run.Agent)
		}
		continuedInTheFork := -1
		for i, event := range events {
			if header.ForkedInto != "" && event.Kind == EventTurnEnd && event.Agent == "" {
				continuedInTheFork = i
			}
		}
		started, models := map[string]TurnStart{}, map[string]string{}
		for i, event := range events {
			sessionOf[event.Turn], sessionOf[event.Agent] = header.ID, header.ID
			var totals UsageTotals
			switch event.Kind {
			case EventTurnStart:
				var start TurnStart
				_ = json.Unmarshal(event.Body, &start)
				started[event.Agent] = start
			case EventRequest:
				body := spentIn(event)
				models[event.Agent] = body.Model
				totals = UsageTotals{TokensIn: body.PromptTokens, TokensOut: body.CompletionTokens, CacheRead: body.CacheReadTokens, CacheWrite: body.CacheWriteTokens, CostUSD: body.CostUSD}
				if event.Attempt <= FirstAttempt {
					totals.Requests = 1
				}
			case EventToolResult:
				totals.SiftedBytes = spentIn(event).SiftSavedBytes
			case EventTurnEnd:
				if event.Agent == "" && i != continuedInTheFork {
					totals.TurnMS = spentIn(event).WallClockMS
				}
			}
			start := started[event.Agent]
			spender := UsageSpender{Session: header.ID, Role: UsageLead, Wire: start.Wire, Spend: start.Spend, Account: start.Account, Model: models[event.Agent]}
			if event.Agent != "" {
				spender.Role, spender.Agent = UsageSubAgent, cmp.Or(definitions[event.Agent], event.Agent)
			}
			count(event.At, spender, totals)
		}
	}
	delete(sessionOf, "")
	for _, call := range classifier {
		count(call.At, UsageSpender{Session: sessionOf[call.Turn], Role: UsageClassifier}, UsageTotals{Requests: 1, CostUSD: call.CostUSD})
	}
	for spender, totals := range spent {
		history.Spenders = append(history.Spenders, UsageRow{UsageSpender: spender, UsageTotals: *totals})
	}
	slices.SortFunc(history.Spenders, func(a, b UsageRow) int {
		return strings.Compare(fmt.Sprint(a.UsageSpender), fmt.Sprint(b.UsageSpender))
	})
	return history, nil
}
