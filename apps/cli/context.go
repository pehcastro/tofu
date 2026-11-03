package main

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strconv"
	"strings"

	"tofu/apps/cli/output"
	"tofu/apps/cli/page"
	"tofu/internal/agent/context/recall"
	"tofu/internal/agent/loop"
	"tofu/internal/host"
	"tofu/internal/store/session"
	settingspkg "tofu/internal/store/settings"
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
	handles, asJSON, err := output.VerbArgs(args)
	o := output.Verb{Name: "context", UsageLine: "tofu context [<name|id|head>] [--json]", AsJSON: asJSON, Out: out, ErrOut: errOut}
	if err == nil && len(handles) > 1 {
		err = fmt.Errorf("%d sessions, want one at most", len(handles))
	}
	if err != nil {
		return o.Usage(err)
	}
	handle := ""
	if len(handles) == 1 {
		handle = handles[0]
	}
	dir, _ := os.Getwd()
	next, err := recall.BudgetUnder("", 0, settingspkg.IntSaying(dir, settingspkg.ContextCeiling, nil))
	var store *session.Store
	if err == nil {
		store, err = session.Open()
	}
	var report contextReport
	if err == nil {
		report, err = contextOf(store, handle, recall.ShippedBands(), next.CeilingTokens)
	}
	if err != nil {
		return o.Fail(err)
	}
	return o.Done(true, report, func(page cli.Page) []string { return contextLines(page, report, store) })
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
	cfg = cfg.OnWire(header.Wire)
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
				Into:   cmp.Or(header.ForkedInto, step.Fork.Into),
				Kind:   string(step.Fork.Kind),
				Counts: &contextForkCounts{TokensBefore: step.Fork.TokensBefore, TokensAfter: step.Fork.TokensAfter},
			}
		}
	}
	if report.Fork == nil {
		report.Fork = headerFork(store, header)
	}
	if header.CarriedFrom != nil {
		recorded, err := store.Events(header.ID)
		if err != nil {
			return contextReport{}, err
		}
		part, err := store.Part(recorded, header.ID)
		if err != nil {
			return contextReport{}, err
		}
		given, err := turn.ConversationFrom(part)
		if err != nil {
			return contextReport{}, err
		}
		report.CarriedFrom = &host.CarriedFrom{Session: header.CarriedFrom.Session, Handle: handleOf(store, header.CarriedFrom.Session), Messages: len(given)}
		report.ForkKind = header.ForkKind
	}
	report.Items = recall.Items(cfg, itemBands, conversation)
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
	return stepRowsOf(reading)
}

func stepRowsOf(reading session.Reading) ([]turn.StepRow, error) {
	steps := make([]turn.StepRow, len(reading.Steps))
	for i, step := range reading.Steps {
		if err := json.Unmarshal(step.Raw, &steps[i]); err != nil {
			return nil, fmt.Errorf("step %d does not read as a turn row: %w", i+1, err)
		}
	}
	return steps, nil
}

func contextLines(page cli.Page, r contextReport, store *session.Store) []string {
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
	facts := []cli.Fact{{Label: "task", Text: output.OneLine(r.Task)}}
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
		cli.Fact{Label: "fork", Text: forkFact(store, r.Fork)})
	if from := r.CarriedFrom; from != nil {
		origin := output.Plural(from.Messages, "message") + " from " + from.Handle
		if r.ForkKind != "" {
			origin += " · " + r.ForkKind
		}
		facts = append(facts, cli.Fact{Label: "from", Text: origin})
	}
	lines := append(page.Title("Context", []string{sessionHandle(r.Session, r.Name), output.Plural(r.Steps, "step")}, verdict), "")
	lines = append(lines, cli.Indent(page.Facts(facts)...)...)
	return append(lines, skippedLines(page, r.Skipped)...)
}

func headerFork(store *session.Store, header session.Header) *contextForkReport {
	if header.ForkedInto == "" {
		return nil
	}
	fork := &contextForkReport{Into: header.ForkedInto}
	if into, err := store.Header(header.ForkedInto); err == nil {
		fork.Kind = into.ForkKind
	}
	if header.ForkTokensBefore > 0 {
		fork.Counts = &contextForkCounts{TokensBefore: header.ForkTokensBefore, TokensAfter: header.ForkTokensAfter}
	}
	return fork
}

func forkFact(store *session.Store, fork *contextForkReport) string {
	if fork == nil {
		return ""
	}
	parts := []string{"into " + handleOf(store, fork.Into)}
	if _, err := store.Header(fork.Into); errors.Is(err, fs.ErrNotExist) {
		parts[0] = "into " + fork.Into + ", not on disk"
	}
	if fork.Kind != "" {
		parts = append(parts, fork.Kind)
	}
	counts := "counts not recorded"
	if fork.Counts != nil {
		counts = strconv.Itoa(fork.Counts.TokensBefore) + " → " + strconv.Itoa(fork.Counts.TokensAfter) + " tokens"
	}
	return strings.Join(append(parts, counts), " · ")
}
