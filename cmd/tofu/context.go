package main

import (
	"cmp"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"tofu/interface/cli"
	"tofu/internal/host"
	"tofu/internal/recall"
	"tofu/internal/session"
	settingspkg "tofu/internal/settings"
	"tofu/internal/turn"
)

const (
	contextNeverMeasured = "recorded before occupancy was kept"
	contextNoSession     = "no session recorded here"
	contextNoneReadable  = "no session here reads"
)

type (
	contextBandReport      = host.ContextBand
	contextOccupancyReport = host.ContextOccupancy
	contextForkCounts      = host.ContextForkCounts
	contextForkReport      = host.ContextFork
	contextReport          = host.ContextReport
)

func contextVerb(args []string, out, errOut io.Writer) int {
	handles, asJSON, err := verbArgs(args)
	o := verbOutput{verb: "context", usageLine: "tofu context [<name|id>] [--json]", asJSON: asJSON, out: out, errOut: errOut}
	if err == nil && len(handles) > 1 {
		err = fmt.Errorf("%d sessions, want one at most", len(handles))
	}
	if err != nil {
		return o.usage(err)
	}
	handle := ""
	if len(handles) == 1 {
		handle = handles[0]
	}
	dir, _ := os.Getwd()
	next, err := recall.BudgetUnder("", 0, settingInt(dir, settingspkg.ContextCeiling, nil))
	var store *session.Store
	if err == nil {
		store, err = session.Open()
	}
	var report contextReport
	if err == nil {
		report, err = contextOf(store, handle, recall.ShippedBands(), next.CeilingTokens)
	}
	if err != nil {
		return o.fail(err)
	}
	return o.done(true, report, func(page cli.Page) []string { return contextLines(page, report) })
}

func contextOf(store *session.Store, handle string, build recall.Bands, nextCeiling int) (contextReport, error) {
	var skipped []sessionSkip
	if handle == "" {
		listing, err := store.Listing()
		if err != nil {
			return contextReport{}, err
		}
		for _, skip := range listing.Skipped {
			skipped = append(skipped, sessionSkip{Session: skip.ID, Reason: skip.Reason.Error()})
		}
		if len(listing.Sessions) == 0 {
			absence := contextNoSession
			if len(skipped) > 0 {
				absence = contextNoneReadable
			}
			return contextReport{Unmeasured: absence, Skipped: skipped, Ceiling: nextCeiling}, nil
		}
		handle = listing.Sessions[0].ID
	}
	header, err := sessionHeader(store, handle)
	if err != nil {
		return contextReport{}, err
	}
	events, err := store.Body(header.ID)
	if err != nil {
		return contextReport{}, err
	}
	cfg, err := recall.LoadConfig()
	if err != nil {
		return contextReport{}, err
	}
	steps, err := contextSteps(events)
	if err != nil {
		return contextReport{}, err
	}
	conversation, err := turn.RecordedContext(events)
	if err != nil {
		return contextReport{}, err
	}
	report := contextReport{
		Session:                header.ID,
		Task:                   header.Task,
		Steps:                  len(steps),
		Unmeasured:             contextNeverMeasured,
		Ceiling:                cmp.Or(header.ContextCeiling, nextCeiling),
		BytesPerThousandTokens: cfg.BytesPerThousandTokens,
		Skipped:                skipped,
	}
	if header.Name != nil {
		report.Name = *header.Name
	}
	itemBands := build
	for _, step := range steps {
		if step.Occupancy != nil {
			caps, recorded := build, step.Bands != nil
			if recorded {
				caps = *step.Bands
			}
			itemBands = caps
			measured := recall.Occupancy{
				Bands:      caps,
				Identity:   step.Occupancy.Identity,
				Facts:      step.Occupancy.Facts,
				WorkingSet: step.Occupancy.WorkingSet,
				Recent:     step.Occupancy.Recent,
			}
			report.Unmeasured = ""
			report.Occupancy = &contextOccupancyReport{
				Step:         step.Index,
				Identity:     contextBand(measured.Identity, caps.Identity),
				Facts:        contextBand(measured.Facts, caps.Facts),
				WorkingSet:   contextBand(measured.WorkingSet, caps.WorkingSet),
				Recent:       contextBand(measured.Recent, caps.Recent),
				Total:        measured.Total(),
				Mark:         step.Occupancy.Target,
				CapsRecorded: recorded,
			}
		}
		if step.Fork != nil {
			report.Fork = &contextForkReport{
				Into:   step.Fork.Into,
				Kind:   string(step.Fork.Kind),
				Counts: &contextForkCounts{TokensBefore: step.Fork.TokensBefore, TokensAfter: step.Fork.TokensAfter},
			}
		}
	}
	if report.Fork == nil && header.ForkedInto != "" {
		report.Fork = &contextForkReport{Into: header.ForkedInto}
	}
	report.Items = recall.Items(cfg.OnWire(header.Wire), itemBands, conversation)
	return report, nil
}

func contextBand(tokens, capacity int) contextBandReport {
	return contextBandReport{Tokens: tokens, Cap: capacity, Percent: recall.FillPercent(tokens, capacity)}
}

func contextSteps(events []session.Event) ([]turn.StepRow, error) {
	reading, err := session.ReadEvents(events)
	if err != nil {
		return nil, err
	}
	steps := make([]turn.StepRow, len(reading.Steps))
	for i, step := range reading.Steps {
		if err := json.Unmarshal(step.Raw, &steps[i]); err != nil {
			return nil, fmt.Errorf("step %d does not read as a turn row: %w", i+1, err)
		}
	}
	return steps, nil
}

func contextLines(page cli.Page, r contextReport) []string {
	if r.Session == "" {
		mark := cli.Idle
		if len(r.Skipped) > 0 {
			mark = cli.Fail
		}
		ceiling := cli.Fact{Label: "ceiling", Text: strconv.Itoa(r.Ceiling) + " tokens, the most the next turn sends"}
		lines := append(page.Title("Context", nil, cli.Verdict{Mark: mark, Text: r.Unmeasured}), "")
		lines = append(lines, cli.Indent(page.Facts([]cli.Fact{ceiling})...)...)
		return append(lines, skippedLines(page, r.Skipped)...)
	}
	verdict := cli.Verdict{Mark: cli.Idle, Text: "not measured"}
	facts := []cli.Fact{{Label: "task", Text: oneLine(r.Task)}}
	if o := r.Occupancy; o != nil {
		verdict = cli.Verdict{Mark: cli.Done, Text: "step " + strconv.Itoa(o.Step)}
		caps := "recorded by the step"
		if !o.CapsRecorded {
			caps = "this build's, the step marked " + strconv.Itoa(o.Mark)
		}
		for _, band := range []struct {
			name string
			band contextBandReport
		}{
			{"identity", o.Identity},
			{"facts", o.Facts},
			{"working set", o.WorkingSet},
			{"recent", o.Recent},
			{"total", contextBand(o.Total, o.Identity.Cap+o.Facts.Cap+o.WorkingSet.Cap+o.Recent.Cap)},
			{"ceiling", contextBand(o.Total, r.Ceiling)},
		} {
			facts = append(facts, cli.Fact{Label: band.name, Text: page.Bar(float64(band.band.Percent)/100) + cli.Gap +
				page.Label(strconv.Itoa(band.band.Tokens)+" / "+strconv.Itoa(band.band.Cap))})
		}
		facts = append(facts, cli.Fact{Label: "caps", Text: caps})
	} else {
		facts = append(facts, cli.Fact{Label: "occupancy", Text: r.Unmeasured})
	}
	for _, item := range r.Items {
		if item.Band == recall.BandIdentity || item.Kind == recall.ItemMemory {
			facts = append(facts, cli.Fact{Label: item.Name, Text: strconv.Itoa(item.Tokens) + " tokens of " + strings.ReplaceAll(string(item.Band), "_", " ")})
		}
	}
	facts = append(facts,
		cli.Fact{Label: "bytes", Text: strconv.Itoa(r.BytesPerThousandTokens) + " per thousand tokens"},
		cli.Fact{Label: "fork", Text: forkFact(r.Fork)})
	lines := append(page.Title("Context", []string{sessionHandle(r.Session, r.Name), plural(r.Steps, "step")}, verdict), "")
	lines = append(lines, cli.Indent(page.Facts(facts)...)...)
	return append(lines, skippedLines(page, r.Skipped)...)
}

func forkFact(fork *contextForkReport) string {
	switch {
	case fork == nil:
		return ""
	case fork.Counts == nil:
		return "into " + fork.Into + " · counts not recorded"
	}
	return "into " + fork.Into + " · " + fork.Kind + " · " + strconv.Itoa(fork.Counts.TokensBefore) + " → " + strconv.Itoa(fork.Counts.TokensAfter) + " tokens"
}
