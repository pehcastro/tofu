package main

import (
	"slices"
	"time"

	"tofu/interface/cli"
	"tofu/internal/host"
	"tofu/internal/session"
)

type traceOutlived = host.TraceOutlived

func callsAfterTheLeadLeft(store *session.Store, header session.Header, events []session.Event) []traceOutlived {
	if header.ForkedInto == "" || header.EndedAt == nil {
		return nil
	}
	into, here := handleOf(store, header.ForkedInto), handleOf(store, header.ID)
	var outlived []traceOutlived
	for _, event := range events {
		if event.Kind == session.EventToolCall && event.Agent != "" && event.At.After(*header.EndedAt) {
			outlived = counted(outlived, traceOutlived{Agent: event.Agent, RecordedIn: here, LeadIn: into})
		}
	}
	return outlived
}

func withAncestors(store *session.Store, header session.Header, report sessionTraceReport) (sessionTraceReport, error) {
	here := handleOf(store, header.ID)
	ancestors, err := store.Ancestors(header.ID)
	if err != nil {
		return sessionTraceReport{}, err
	}
	whileHere := func(agent string, at time.Time) bool {
		return agent != "" && !at.Before(header.At) && (header.EndedAt == nil || at.Before(*header.EndedAt))
	}
	carried := map[string]int{}
	for i, call := range report.Calls {
		if header.CarriedFrom != nil && call.Turn == header.ID {
			carried[call.Call] = i
		}
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
		there := handleOf(store, from.ID)
		recorded := slices.DeleteFunc(slices.Clone(events), func(event session.Event) bool {
			_, wanted := carried[event.Call]
			return !wanted || event.Turn == from.ID
		})
		if len(recorded) > 0 {
			original, err := traceBody(store, from.ID, recorded, func(string, time.Time) bool { return true })
			if err != nil {
				return sessionTraceReport{}, err
			}
			for _, call := range original.Calls {
				call.RecordedIn = there
				report.Calls[carried[call.Call]] = call
				delete(carried, call.Call)
			}
		}
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
		rows[i] = cli.Row{Mark: cli.Idle, Cells: []string{run.Agent, plural(run.Calls, "call"), "recorded in " + run.RecordedIn}, Detail: "while the lead was in " + run.LeadIn}
	}
	return rows
}

func recordedIn(session string) string {
	if session == "" {
		return ""
	}
	return "recorded in " + session + " · "
}
