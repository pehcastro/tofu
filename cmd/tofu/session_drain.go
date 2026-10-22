package main

import (
	"cmp"
	"errors"
	"fmt"
	"os"
	"slices"
	"strconv"
	"sync"
	"time"

	"tofu/interface/cli"
	"tofu/internal/drain"
	"tofu/internal/judge/ledger"
	"tofu/internal/recall"
	"tofu/internal/session"
	"tofu/internal/sys"
)

const (
	sessionDrainUsage = "tofu session drain [name|id] [--json]"
	drainShownRows    = 10
	drainMillion      = 1_000_000
	drainThousand     = 1_000
	drainPercent      = 100
)

type drainReport struct {
	Handle string `json:"handle"`
	drain.Report
}

func sessionDrainVerb(o verbOutput, args []string) int {
	o.verb, o.usageLine = "session drain", sessionDrainUsage
	handles, _, err := verbArgs(args)
	if err == nil && len(handles) > 1 {
		err = fmt.Errorf("%d operands, want at most one <name|id>", len(handles))
	}
	if err != nil {
		return o.usage(err)
	}
	return sessionReport(o, func(store *session.Store) (drainReport, error) { return sessionDrain(store, handles) }, drainLines)
}

func sessionDrain(store *session.Store, handles []string) (drainReport, error) {
	once, handle := store.ReadOnce(), ""
	if len(handles) == 1 {
		handle = handles[0]
	} else if head, err := once.Head(); err == nil {
		handle = head.ID
	} else {
		return drainReport{}, err
	}
	header, err := sessionHeader(once, handle)
	if err != nil {
		return drainReport{}, err
	}
	ancestors, err := once.Ancestors(header.ID)
	if err != nil {
		return drainReport{}, err
	}
	registry, err := modelRegistry()
	if err != nil {
		return drainReport{}, err
	}
	estimate, err := recall.LoadConfig()
	if err != nil {
		return drainReport{}, err
	}
	logs, err := sys.LogDir()
	if err != nil {
		return drainReport{}, err
	}
	until := time.Now()
	if header.EndedAt != nil {
		until = *header.EndedAt
	}
	var events, borrowed []session.Event
	var exchanges []session.Exchange
	var classified []session.ClassifierCall
	runs := slices.Clone(header.Agents)
	failed := make([]error, 4)
	var loads sync.WaitGroup
	loads.Go(func() { events, failed[0] = store.Events(header.ID) })
	loads.Go(func() { exchanges, failed[1] = store.Exchanges(header.ID) })
	loads.Go(func() {
		for _, from := range ancestors {
			runs = append(runs, from.Agents...)
			outlived := slices.ContainsFunc(from.Agents, func(run session.AgentRun) bool { return run.EndedAt == nil || run.EndedAt.After(header.At) })
			if info, err := os.Stat(store.EventsPath(from.ID)); !outlived || err != nil || info.ModTime().Before(header.At) {
				continue
			}
			recorded, err := store.Events(from.ID)
			if failed[2] = err; err != nil {
				return
			}
			borrowed = append(borrowed, slices.DeleteFunc(recorded, func(event session.Event) bool {
				return event.Agent == "" || event.At.Before(header.At) || !event.At.Before(until)
			})...)
		}
	})
	loads.Go(func() {
		_, failed[3] = ledger.NewReader(logs).Each(ledger.Filter{Since: header.At, Until: until}, func(row ledger.Row) error {
			classified = append(classified, session.ClassifierCall{At: row.At, Turn: row.TurnID, CostUSD: row.Cost})
			return nil
		})
	})
	loads.Wait()
	if err := errors.Join(failed...); err != nil {
		return drainReport{}, err
	}
	events = append(slices.DeleteFunc(events, func(event session.Event) bool { return header.CarriedFrom != nil && event.Turn == header.ID }), borrowed...)
	slices.SortStableFunc(events, func(a, b session.Event) int { return a.At.Compare(b.At) })
	turns := map[string]bool{}
	for _, event := range events {
		if event.Kind == session.EventTurnStart {
			turns[event.Turn] = true
		}
	}
	classified = slices.DeleteFunc(classified, func(call session.ClassifierCall) bool { return !turns[call.Turn] })
	var breaks []drain.Break
	for _, cut := range cacheTrace(events, exchanges).Breaks {
		breaks = append(breaks, drain.Break{Request: cut.Request, Differs: cut.Differs})
	}
	report := drain.Read(header.ID, events, drain.Input{Breaks: breaks, Classifier: classified, Runs: runs, Weights: drain.WeightsFrom(registry), BytesPerThousandTokens: estimate.BytesPerThousandTokens})
	return drainReport{Handle: handleOf(once, header.ID), Report: report}, nil
}

func weighted(tokens float64) string {
	switch {
	case tokens >= drainMillion:
		return strconv.FormatFloat(tokens/drainMillion, 'f', 1, 64) + "M"
	case tokens >= drainThousand:
		return strconv.FormatFloat(tokens/drainThousand, 'f', 0, 64) + "k"
	}
	return strconv.FormatFloat(tokens, 'f', 0, 64)
}

func percentOf(part, whole float64) string {
	if whole <= 0 {
		return "0%"
	}
	return strconv.FormatFloat(part/whole*drainPercent, 'f', 0, 64) + "%"
}

func spendRows(lines []drain.Line, whole float64) []cli.Row {
	rows := make([]cli.Row, len(lines))
	for i, line := range lines {
		rows[i] = cli.Row{Mark: cli.Idle, Cells: []string{line.Name, percentOf(line.Weighted, whole), weighted(line.Weighted), plural(line.Calls, "call"), dollars(line.USD)}}
	}
	return rows
}

func drainTldr(report drainReport) []cli.Fact {
	total := report.Total.Weighted
	tldr := []cli.Fact{{Label: "sub-agents", Text: percentOf(total-report.Lead.Weighted, total) + " of weighted tokens across " + plural(len(report.Agents), "sub-agent")}}
	if len(report.Wakes) > 0 {
		tldr = append(tldr, cli.Fact{Label: "lead woken", Text: "by " + report.Wakes[0].Name + " for " + percentOf(report.Wakes[0].Weighted, report.Lead.Weighted) + " of the lead's weighted tokens"})
	}
	heaviest := slices.MaxFunc(report.Bands, func(a, b drain.Line) int { return cmp.Compare(a.Weighted, b.Weighted) })
	var rebuilt, idle float64
	for _, rebuild := range report.Rebuilds {
		rebuilt += rebuild.Weighted
		if rebuild.Cause == drain.CauseIdle {
			idle += rebuild.Weighted
		}
	}
	return append(tldr, cli.Fact{Label: "context", Text: heaviest.Name + " band took " + percentOf(heaviest.Weighted, total)},
		cli.Fact{Label: "rebuilds", Text: plural(len(report.Rebuilds), "cache rebuild") + ", " + percentOf(rebuilt, total) + " of the total, " + percentOf(idle, rebuilt) + " of it after idle"},
		cli.Fact{Label: "unread", Text: percentOf(report.Unread, total) + " went to cache writes the next call never read"},
		cli.Fact{Label: "outside", Text: "memory writer " + weighted(report.Memory.Weighted) + " " + dollars(report.Memory.USD) + ", not in the total"})
}

func drainLines(page cli.Page, report drainReport) []string {
	total := report.Total.Weighted
	lines := append(page.Title("Drain", []string{report.Handle, plural(report.Total.Calls, "call"), weighted(total) + " weighted tokens", dollars(report.Total.USD)}, cli.Verdict{}), "")
	lines = append(lines, cli.Indent(page.Facts(drainTldr(report))...)...)
	section := func(name string, rows []cli.Row, render func([]cli.Row) []string) {
		lines = append(append(lines, "", page.Section(name, cli.Verdict{})), render(rows[:min(len(rows), drainShownRows)])...)
	}
	fit := func(rows []cli.Row) []string { return fitted(page, rows) }
	wrap := func(rows []cli.Row) []string { return cli.Indent(page.Rows(rows)...) }
	findings := make([]cli.Row, len(report.Findings))
	for i, finding := range report.Findings {
		findings[i] = cli.Row{Mark: cli.Idle, Cells: []string{string(finding.Rule), cmp.Or(finding.Agent, traceLead), "turn " + finding.Turn},
			Detail: traced(finding.Evidence, finding.At)}
		if finding.Severity == drain.SeverityWarning {
			findings[i].Mark = cli.Warn
		}
	}
	section("findings", findings, wrap)
	section("lead woken by", spendRows(report.Wakes, total), fit)
	section("sub-agents woken by", spendRows(report.Children, total), fit)
	section("context band", spendRows(report.Bands, total), fit)
	residency := make([]cli.Row, len(report.Residency))
	for i, resident := range report.Residency {
		residency[i] = cli.Row{Mark: cli.Idle, Cells: []string{resident.Kind, plural(resident.Blocks, "block"), weighted(float64(resident.Tokens)) + " tokens",
			weighted(float64(resident.TokenCalls)) + " token-calls", "stays " + strconv.FormatInt(resident.TokenCalls/int64(max(resident.Tokens, 1)), 10) + " calls", weighted(resident.Weighted) + " weighted"}}
	}
	section("residency", residency, fit)
	slices.SortStableFunc(report.Rebuilds, func(a, b drain.Rebuild) int { return cmp.Compare(b.Weighted, a.Weighted) })
	rebuilds := make([]cli.Row, len(report.Rebuilds))
	for i, rebuild := range report.Rebuilds {
		rebuilds[i] = cli.Row{Mark: cli.Warn, Cells: []string{cmp.Or(rebuild.Agent, traceLead), string(rebuild.Cause), weighted(rebuild.Weighted) + " weighted",
			"gap " + (time.Duration(rebuild.GapMS) * time.Millisecond).Round(time.Second).String()}, Detail: traced("first difference: "+rebuild.Differs, "turn "+rebuild.Turn, rebuild.Request)}
	}
	section("cache rebuilds", rebuilds, wrap)
	section("agent types", spendRows(report.Types, total), fit)
	agents := make([]cli.Row, len(report.Agents))
	for i, run := range report.Agents {
		agents[i] = cli.Row{Mark: cli.Idle, Cells: []string{run.Name, run.Type, percentOf(run.Weighted, total), weighted(run.Weighted), plural(run.Calls, "call"),
			"peak " + weighted(float64(run.PeakContext)), "rebuilt " + weighted(run.Rebuilt), "unread " + weighted(run.Unread)}}
	}
	section("sub-agents", agents, fit)
	return lines
}
