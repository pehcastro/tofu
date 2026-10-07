package main

import (
	"slices"
	"time"

	"tofu/interface/cli"
	"tofu/internal/session"
)

type traceOutlived struct {
	Agent      string `json:"agent"`
	Calls      int    `json:"calls"`
	RecordedIn string `json:"recorded_in"`
	LeadIn     string `json:"lead_in"`
}

func callsAfterTheLeadLeft(store *session.Store, header session.Header, events []session.Event) []traceOutlived {
	if header.ForkedInto == "" || header.EndedAt == nil {
		return nil
	}
	into := sessionHandle(header.ForkedInto, "")
	if next, err := store.Header(header.ForkedInto); err == nil {
		into = sessionHandle(next.ID, next.Named())
	}
	var outlived []traceOutlived
	for _, event := range events {
		if event.Kind == session.EventToolCall && event.Agent != "" && event.At.After(*header.EndedAt) {
			outlived = counted(outlived, traceOutlived{Agent: event.Agent, RecordedIn: sessionHandle(header.ID, header.Named()), LeadIn: into})
		}
	}
	return outlived
}

func withAncestorsSubAgents(store *session.Store, header session.Header, report sessionTraceReport) (sessionTraceReport, error) {
	here := sessionHandle(header.ID, header.Named())
	ancestors, err := store.Ancestors(header.ID)
	if err != nil {
		return sessionTraceReport{}, err
	}
	whileHere := func(agent string, at time.Time) bool {
		return agent != "" && !at.Before(header.At) && (header.EndedAt == nil || at.Before(*header.EndedAt))
	}
	for _, from := range ancestors {
		events, err := store.Events(from.ID)
		if err != nil {
			return sessionTraceReport{}, err
		}
		borrowed, err := traceBody(store, from.ID, events, whileHere)
		if err != nil {
			return sessionTraceReport{}, err
		}
		there := sessionHandle(from.ID, from.Named())
		for i := range borrowed.Requests {
			borrowed.Requests[i].RecordedIn = there
		}
		for i, call := range borrowed.Calls {
			borrowed.Calls[i].RecordedIn = there
			report.Outlived = counted(report.Outlived, traceOutlived{Agent: call.Agent, RecordedIn: there, LeadIn: here})
		}
		for _, run := range from.Agents {
			if slices.ContainsFunc(borrowed.Requests, func(request traceRequest) bool { return request.Agent == run.Agent }) {
				report.Agents = append(report.Agents, run)
			}
		}
		report.Requests, report.Calls = append(report.Requests, borrowed.Requests...), append(report.Calls, borrowed.Calls...)
		report.Hooks, report.Failures = append(report.Hooks, borrowed.Hooks...), append(report.Failures, borrowed.Failures...)
		report.Inserted, report.Changes, report.Notices = append(report.Inserted, borrowed.Inserted...), append(report.Changes, borrowed.Changes...), append(report.Notices, borrowed.Notices...)
	}
	return report, nil
}

func counted(outlived []traceOutlived, call traceOutlived) []traceOutlived {
	at := slices.IndexFunc(outlived, func(seen traceOutlived) bool { return seen.Agent == call.Agent && seen.RecordedIn == call.RecordedIn })
	if at < 0 {
		call.Calls = 1
		return append(outlived, call)
	}
	outlived[at].Calls++
	return outlived
}

func outlivedRows(outlived []traceOutlived) []cli.Row {
	rows := make([]cli.Row, len(outlived))
	for i, run := range outlived {
		rows[i] = cli.Row{Mark: cli.Idle, Cells: []string{run.Agent, countOf(run.Calls, "call"), "recorded in " + run.RecordedIn}, Detail: "while the lead was in " + run.LeadIn}
	}
	return rows
}

func recordedIn(session string) string {
	if session == "" {
		return ""
	}
	return "recorded in " + session + " · "
}
