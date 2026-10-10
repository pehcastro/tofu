package main

import (
	"cmp"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"tofu/interface/cli"
	"tofu/internal/host"
	"tofu/internal/session"
	"tofu/internal/widget"
)

const sessionFindUsage = "tofu session find <name|id> [--tool T] [--command C] [--file F] [--text T] [--agent A] [--since 2h|RFC3339] [--until 2h|RFC3339] [--json]"

type familyGeneration struct {
	Generation int               `json:"generation"`
	Session    string            `json:"session"`
	Name       string            `json:"name,omitempty"`
	Kind       string            `json:"kind,omitempty"`
	At         time.Time         `json:"at"`
	EndedAt    *time.Time        `json:"ended_at,omitempty"`
	EndReason  session.EndReason `json:"end_reason,omitempty"`
	Outcome    string            `json:"outcome,omitempty"`
	Steps      int               `json:"steps"`
	ActiveMS   int64             `json:"active_ms"`
	Bytes      int64             `json:"bytes"`
	CostUSD    float64           `json:"cost_usd,omitempty"`
}

type familySubAgent struct {
	Agent      string  `json:"agent"`
	Definition string  `json:"definition,omitempty"`
	Runs       int     `json:"runs"`
	Status     string  `json:"status"`
	CostUSD    float64 `json:"cost_usd,omitempty"`
}

type sessionFamilyReport struct {
	session.Identity
	Handle       string             `json:"handle"`
	ActiveMS     int64              `json:"active_ms"`
	CostUSD      float64            `json:"cost_usd,omitempty"`
	Generations  []familyGeneration `json:"generations"`
	BranchedFrom string             `json:"branched_from,omitempty"`
	Branches     []string           `json:"branches,omitempty"`
	SubAgents    []familySubAgent   `json:"sub_agents,omitempty"`
}

type sessionFindReport = host.SessionFind

func sessionReport[R any](o verbOutput, build func(*session.Store) (R, error), lines func(cli.Page, R) []string) int {
	store, err := session.Open()
	if err != nil {
		return o.fail(err)
	}
	report, err := build(store)
	if err != nil {
		return o.fail(err)
	}
	return o.done(true, report, func(page cli.Page) []string { return lines(page, report) })
}

func sessionFamily(store *session.Store, handle string) (session.Family, []session.Family, error) {
	header, err := sessionHeader(store, handle)
	if err != nil {
		return session.Family{}, nil, err
	}
	listing, err := store.Listing()
	if err != nil {
		return session.Family{}, nil, err
	}
	return listing.FamilyOf(header.ID)
}

func handleOf(store *session.Store, id string) string {
	if identity, err := store.Identity(id); err == nil {
		return identity.Handle()
	}
	return sessionShortID(id)
}

func generationHandle(families []session.Family, id string) string {
	for _, family := range families {
		for at, header := range family.Generations {
			if header.ID == id {
				generation := family.Identity
				generation.Generation = at + 1
				return generation.Handle()
			}
		}
	}
	return id
}

func sessionFamilyOf(store *session.Store, handle string) (sessionFamilyReport, error) {
	family, families, err := sessionFamily(store, handle)
	if err != nil {
		return sessionFamilyReport{}, err
	}
	report := sessionFamilyReport{Identity: family.Identity, Handle: family.Handle()}
	if family.BranchedFrom != nil {
		report.BranchedFrom = generationHandle(families, family.BranchedFrom.Session)
	}
	for _, branch := range family.Branches {
		report.Branches = append(report.Branches, generationHandle(families, branch))
	}
	agents := map[string]int{}
	for at, header := range family.Generations {
		generation := familyGeneration{Generation: at + 1, Session: header.ID, Kind: header.ForkKind, At: header.At, EndedAt: header.EndedAt,
			EndReason: header.EndReason, Outcome: header.Outcome, CostUSD: header.CostUSD}
		if header.Named() != family.Name {
			generation.Name = header.Named()
		}
		if info, err := os.Stat(store.EventsPath(header.ID)); err == nil {
			generation.Bytes = info.Size()
		}
		events, err := store.Events(header.ID)
		if err != nil {
			return sessionFamilyReport{}, err
		}
		began, last := map[string]time.Time{}, map[string]time.Time{}
		for _, event := range events {
			if event.Agent != "" || event.Turn == "" {
				continue
			}
			if event.Kind == session.EventRequest || event.Kind == session.EventStep {
				generation.Steps++
			}
			if first, seen := began[event.Turn]; !seen || event.At.Before(first) {
				began[event.Turn] = event.At
			}
			if event.At.After(last[event.Turn]) {
				last[event.Turn] = event.At
			}
		}
		for turn, at := range began {
			generation.ActiveMS += last[turn].Sub(at).Milliseconds()
		}
		report.ActiveMS, report.CostUSD = report.ActiveMS+generation.ActiveMS, report.CostUSD+header.CostUSD
		report.Generations = append(report.Generations, generation)
		for _, run := range header.Agents {
			base := run.Agent
			if cut := strings.LastIndex(base, "-f"); cut > 0 {
				if _, err := strconv.Atoi(base[cut+2:]); err == nil {
					base = base[:cut]
				}
			}
			at, known := agents[base]
			if !known {
				at, agents[base] = len(report.SubAgents), len(report.SubAgents)
				report.SubAgents = append(report.SubAgents, familySubAgent{Agent: base, Definition: run.Definition})
			}
			agent := &report.SubAgents[at]
			agent.Runs, agent.Status, agent.CostUSD = agent.Runs+1, run.Status, agent.CostUSD+run.CostUSD
		}
	}
	return report, nil
}

func sessionFamilyLines(page cli.Page, report sessionFamilyReport, now time.Time) []string {
	head := report.Generations[len(report.Generations)-1]
	facts := []string{plural(len(report.Generations), "generation"), "started " + sessionWhen(report.Started, now),
		"active " + widget.Until(time.Duration(report.ActiveMS)*time.Millisecond)}
	lines := append(page.Title(report.Handle, facts, cli.Verdict{Mark: cli.Active, Text: cmp.Or(head.Outcome, "open")}), "")
	rows := make([]cli.Row, len(report.Generations))
	for i, generation := range report.Generations {
		kind := cmp.Or(generation.Kind, "began")
		if generation.Name != "" {
			kind += " · was " + generation.Name
		}
		rows[i] = cli.Row{Mark: cli.Idle, Cells: []string{"." + strconv.Itoa(generation.Generation), sessionWhen(generation.At, now), plural(generation.Steps, "step"),
			widget.Until(time.Duration(generation.ActiveMS) * time.Millisecond), widget.Size(int(generation.Bytes)), kind}}
	}
	rows[len(rows)-1].Mark = cli.Active
	lines = append(append(lines, page.Section("generations", cli.Verdict{})), cli.Indent(page.Rows(rows)...)...)
	var links []cli.Fact
	if report.BranchedFrom != "" {
		links = append(links, cli.Fact{Label: "branched from", Text: report.BranchedFrom})
	}
	if len(report.Branches) > 0 {
		links = append(links, cli.Fact{Label: "branches", Text: strings.Join(report.Branches, ", ")})
	}
	if report.CostUSD > 0 {
		links = append(links, cli.Fact{Label: "cost", Text: dollars(report.CostUSD)})
	}
	if len(links) > 0 {
		lines = append(append(lines, ""), cli.Indent(page.Facts(links)...)...)
	}
	if len(report.SubAgents) > 0 {
		agents := make([]cli.Row, len(report.SubAgents))
		for i, agent := range report.SubAgents {
			agents[i] = cli.Row{Mark: cli.Idle, Cells: []string{agent.Agent, agent.Definition, plural(agent.Runs, "run"), agent.Status}}
		}
		lines = append(append(lines, "", page.Section("sub-agents", cli.Verdict{})), cli.Indent(page.Rows(agents)...)...)
	}
	return append(append(lines, ""), cli.Indent(page.Hint("tofu session find "+report.Handle+" --tool bash"))...)
}

func sessionFindArgs(args []string, now time.Time) (string, session.Query, error) {
	var query session.Query
	texts := map[string]*string{"--tool": &query.Tool, "--command": &query.Command, "--file": &query.File, "--text": &query.Text, "--agent": &query.Agent}
	times := map[string]*time.Time{"--since": &query.Since, "--until": &query.Until}
	var handles []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == jsonFlag:
			continue
		case !strings.HasPrefix(arg, "-"):
			handles = append(handles, arg)
			continue
		case i+1 == len(args):
			return "", query, fmt.Errorf("%s needs a value", arg)
		}
		i++
		if text, known := texts[arg]; known {
			*text = args[i]
			continue
		}
		at, known := times[arg]
		if !known {
			return "", query, fmt.Errorf("unknown argument %q", arg)
		}
		if ago, err := time.ParseDuration(args[i]); err == nil {
			*at = now.Add(-ago)
			continue
		}
		parsed, err := time.Parse(time.RFC3339, args[i])
		if err != nil {
			return "", query, fmt.Errorf("%s %q is neither a duration like 2h nor a time like 2026-10-07T09:00:00Z", arg, args[i])
		}
		*at = parsed
	}
	if len(handles) != 1 {
		return "", query, fmt.Errorf("%d operands, want <name|id>", len(handles))
	}
	return handles[0], query, nil
}

func sessionFind(store *session.Store, handle string, query session.Query) (sessionFindReport, error) {
	family, _, err := sessionFamily(store, handle)
	if err != nil {
		return sessionFindReport{}, err
	}
	hits, err := store.Find(family, query)
	return sessionFindReport{Handle: family.Handle(), Query: query, Hits: append([]session.Hit{}, hits...)}, err
}

func sessionFindLines(page cli.Page, report sessionFindReport, now time.Time) []string {
	lines := append(page.Title("Found", []string{report.Handle, plural(len(report.Hits), "hit")}, cli.Verdict{}), "")
	if len(report.Hits) == 0 {
		return append(lines, cli.Indent(page.Label("nothing matched"))...)
	}
	rows := make([]cli.Row, len(report.Hits))
	for i, hit := range report.Hits {
		what, detail := cmp.Or(hit.Tool, hit.Role), cmp.Or(hit.Command, oneLine(hit.Text), oneLine(string(hit.Args)))
		mark := cli.Idle
		if hit.Ran != nil && (hit.Ran.Error != "" || hit.Ran.ExitCode != nil && *hit.Ran.ExitCode != 0) {
			mark = cli.Fail
		}
		rows[i] = cli.Row{Mark: mark, Cells: []string{"." + strconv.Itoa(hit.Generation), sessionWhen(hit.At, now), cmp.Or(hit.Agent, "lead"), what}, Detail: detail}
	}
	return append(lines, cli.Indent(page.Rows(rows)...)...)
}
