package boardy

import (
	"slices"
	"strings"
	"time"
)

type Actual struct {
	Rounds  int `json:"rounds"`
	Minutes int `json:"minutes"`
}

func Actuals(events []Event, now time.Time) map[string]Actual {
	actuals := map[string]Actual{}
	started := map[string]time.Time{}
	for _, event := range events {
		if event.Kind != Moved {
			continue
		}
		actual := actuals[event.Ticket]
		if at, doing := started[event.Ticket]; doing && event.From == Doing {
			actual.Minutes += int(event.At.Sub(at).Minutes())
			delete(started, event.Ticket)
		}
		if event.To == Doing {
			started[event.Ticket] = event.At
		}
		if event.To == Review {
			actual.Rounds++
		}
		actuals[event.Ticket] = actual
	}
	for ticket, at := range started {
		actual := actuals[ticket]
		actual.Minutes += int(now.Sub(at).Minutes())
		actuals[ticket] = actual
	}
	return actuals
}

type Tally struct {
	Name    string `json:"name"`
	Title   string `json:"title,omitempty"`
	Tickets int    `json:"tickets"`
	Done    int    `json:"done"`
	Points  int    `json:"points"`
	Settled int    `json:"points_done"`
}

func (t *Tally) add(ticket Ticket) {
	t.Tickets++
	t.Points += ticket.Points
	if !ticket.Status.Live() {
		t.Done++
		t.Settled += ticket.Points
	}
}

type Aging struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Reason string `json:"reason,omitempty"`
	Days   int    `json:"days"`
}

type Report struct {
	Board    string    `json:"board"`
	Since    time.Time `json:"since"`
	Sprint   *Tally    `json:"sprint,omitempty"`
	Statuses []Tally   `json:"statuses"`
	Epics    []Tally   `json:"epics"`
	Finished []string  `json:"finished"`
	Moves    int       `json:"moves"`
	Rounds   int       `json:"rounds"`
	Minutes  int       `json:"minutes"`
	Review   []Aging   `json:"review"`
	Blocked  []Aging   `json:"blocked"`
}

func BuildReport(key string, since, now time.Time, tickets []Ticket, events []Event, epics []Epic, sprints []Sprint) Report {
	report := Report{Board: key, Since: since, Finished: []string{}, Review: []Aging{}, Blocked: []Aging{}}
	active, hasActive := ActiveSprint(sprints)
	if hasActive {
		report.Sprint = &Tally{Name: active.ID, Title: active.Title}
	}
	for _, status := range Statuses() {
		report.Statuses = append(report.Statuses, Tally{Name: string(status)})
	}
	for _, epic := range epics {
		report.Epics = append(report.Epics, Tally{Name: epic.ID, Title: epic.Title})
	}
	for _, ticket := range tickets {
		report.Statuses[slices.Index(Statuses(), ticket.Status)].add(ticket)
		if index := slices.IndexFunc(report.Epics, func(epic Tally) bool { return epic.Name == ticket.Epic }); index >= 0 {
			report.Epics[index].add(ticket)
		}
		if hasActive && ticket.Sprint == active.ID {
			report.Sprint.add(ticket)
		}
		aging := Aging{ID: ticket.ID, Title: ticket.Title, Reason: ticket.Reason, Days: int(now.Sub(ticket.Updated).Hours() / 24)}
		switch ticket.Status {
		case Review:
			report.Review = append(report.Review, aging)
		case Blocked:
			report.Blocked = append(report.Blocked, aging)
		}
	}
	for _, list := range [][]Aging{report.Review, report.Blocked} {
		slices.SortStableFunc(list, func(a, b Aging) int { return b.Days - a.Days })
	}
	events = slices.DeleteFunc(slices.Clone(events), func(event Event) bool { return event.At.Before(since) })
	for _, event := range events {
		if event.Kind != Moved {
			continue
		}
		report.Moves++
		if event.To == Done {
			report.Finished = append(report.Finished, event.Ticket)
		}
	}
	for _, actual := range Actuals(events, now) {
		report.Rounds += actual.Rounds
		report.Minutes += actual.Minutes
	}
	slices.SortFunc(report.Finished, strings.Compare)
	report.Finished = slices.Compact(report.Finished)
	return report
}
