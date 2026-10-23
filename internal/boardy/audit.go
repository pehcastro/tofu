package boardy

import (
	"cmp"
	"errors"
	"slices"
	"strings"
	"time"
	"unicode"

	"tofu/internal/konst"
)

type AuditRequest struct {
	Board string `json:"board,omitempty"`
}

type AuditAnswer struct {
	Audit
	Words Words `json:"words"`
}

type AgeBucket struct {
	FromDays int      `json:"from_days"`
	ToDays   int      `json:"to_days,omitempty"`
	Tickets  []string `json:"tickets"`
}

type Unacted struct {
	Ticket  string    `json:"ticket"`
	At      time.Time `json:"at"`
	Agent   string    `json:"agent,omitempty"`
	Session string    `json:"session,omitempty"`
}

type Duplicate struct {
	Tickets [2]string `json:"tickets"`
	Overlap float64   `json:"title_overlap"`
}

type Lagged struct {
	Ticket  string `json:"ticket"`
	Minutes int    `json:"minutes"`
}

type Lag struct {
	Reports    int      `json:"reports"`
	Median     int      `json:"median_minutes"`
	Percentile int      `json:"slow_percentile"`
	Slow       int      `json:"slow_minutes"`
	Slowest    []Lagged `json:"slowest"`
}

type Audit struct {
	Board      string      `json:"board"`
	Tickets    int         `json:"tickets"`
	Live       int         `json:"live"`
	Review     []AgeBucket `json:"review"`
	Unacted    []Unacted   `json:"unacted"`
	Unplanned  []string    `json:"unplanned"`
	Duplicates []Duplicate `json:"duplicates"`
	Unasked    []Aging     `json:"blocked_unasked"`
	StaleTodo  []Aging     `json:"stale_todo"`
	Lag        Lag         `json:"lag"`
}

func (a API) Audit(request AuditRequest) (AuditAnswer, error) {
	key, err := a.boardKey(request.Board)
	if err != nil {
		return AuditAnswer{}, err
	}
	tickets, ticketsErr := a.Board.Store.Tickets(key)
	events, eventsErr := a.Board.Store.Events(key, 0)
	return AuditAnswer{Audit: audit(key, time.Now(), tickets, events), Words: a.Words}, errors.Join(ticketsErr, eventsErr)
}

func audit(key string, now time.Time, tickets []Ticket, events []Event) Audit {
	result := Audit{Board: key, Tickets: len(tickets), Unacted: []Unacted{}, Unplanned: []string{}, Duplicates: likelyDuplicates(tickets), Unasked: []Aging{}, StaleTodo: []Aging{}, Lag: Lag{Percentile: konst.BoardAuditSlowPercentile, Slowest: []Lagged{}}}
	result.Review = []AgeBucket{
		{ToDays: konst.BoardAuditFreshDays, Tickets: []string{}},
		{FromDays: konst.BoardAuditFreshDays, ToDays: konst.BoardAuditStaleDays, Tickets: []string{}},
		{FromDays: konst.BoardAuditStaleDays, Tickets: []string{}},
	}
	entered := map[string]time.Time{}
	reported := map[string]Event{}
	var lags []Lagged
	for _, event := range events {
		if event.To != "" && (event.Kind == Created || event.Kind == Moved) {
			entered[event.Ticket] = event.At
		}
		switch event.Kind {
		case Reported:
			reported[event.Ticket] = event
		case Moved:
			if report, waiting := reported[event.Ticket]; waiting {
				lags = append(lags, Lagged{Ticket: event.Ticket, Minutes: int(event.At.Sub(report.At).Minutes())})
				delete(reported, event.Ticket)
			}
		}
	}
	for _, ticket := range tickets {
		since := cmp.Or(entered[ticket.ID], ticket.Updated)
		aging := Aging{ID: ticket.ID, Title: ticket.Title, Reason: ticket.Reason, Days: int(now.Sub(since).Hours() / 24)}
		if report, waiting := reported[ticket.ID]; waiting {
			result.Unacted = append(result.Unacted, Unacted{Ticket: ticket.ID, At: report.At, Agent: report.Agent, Session: report.Session})
		}
		if ticket.Status.Live() {
			result.Live++
			if ticket.Epic == "" && ticket.Sprint == "" {
				result.Unplanned = append(result.Unplanned, ticket.ID)
			}
		}
		switch {
		case ticket.Status == Review:
			bucket := slices.IndexFunc(result.Review, func(b AgeBucket) bool { return b.ToDays == 0 || aging.Days < b.ToDays })
			result.Review[bucket].Tickets = append(result.Review[bucket].Tickets, ticket.ID)
		case (ticket.Status == Blocked || ticket.Status == NeedsDecision) && len(ticket.OpenQuestions()) == 0:
			result.Unasked = append(result.Unasked, aging)
		case ticket.Status == Todo && aging.Days >= konst.BoardAuditStaleDays:
			result.StaleTodo = append(result.StaleTodo, aging)
		}
	}
	if len(lags) == 0 {
		return result
	}
	slices.SortStableFunc(lags, func(a, b Lagged) int { return b.Minutes - a.Minutes })
	result.Lag.Reports, result.Lag.Median = len(lags), lags[len(lags)/2].Minutes
	slowCount := max(1, len(lags)*(100-konst.BoardAuditSlowPercentile)/100)
	result.Lag.Slow, result.Lag.Slowest = lags[slowCount-1].Minutes, lags[:slowCount]
	return result
}

func likelyDuplicates(tickets []Ticket) []Duplicate {
	words := make([]map[string]bool, len(tickets))
	for i, ticket := range tickets {
		words[i] = map[string]bool{}
		for _, word := range strings.FieldsFunc(strings.ToLower(ticket.Title), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
			words[i][word] = true
		}
	}
	found := []Duplicate{}
	for i, a := range tickets {
		for j := i + 1; j < len(tickets); j++ {
			b := tickets[j]
			if slices.Contains(a.Duplicates, b.ID) || slices.Contains(b.Duplicates, a.ID) {
				continue
			}
			shared := 0
			for word := range words[i] {
				if words[j][word] {
					shared++
				}
			}
			union := len(words[i]) + len(words[j]) - shared
			if overlap := float64(shared) / float64(max(union, 1)); overlap >= konst.BoardAuditDuplicateOverlap {
				found = append(found, Duplicate{Tickets: [2]string{a.ID, b.ID}, Overlap: overlap})
			}
		}
	}
	return found
}
