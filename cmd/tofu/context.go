package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"

	"tofu/internal/konst"
	"tofu/internal/recall"
	"tofu/internal/session"
	"tofu/internal/turn"
)

const contextUsage = "usage: tofu context [<session-id>] [--json]"

const contextNeverMeasured = "no band was measured: this session was recorded before the occupancy reached the step row"

const contextCapsRecorded = "the caps above are the ones the step measured itself against, totalling"

const contextCapsUnrecorded = "this step recorded no caps, only a mark of"

const contextNoSession = "no session has been recorded in this directory, so there is no context to report: run tofu run here first"

const contextNoneReadable = "no session in this directory could be read, so there is no context to report"

type contextSkipReport struct {
	Session string `json:"session"`
	Reason  string `json:"reason"`
}

type contextBandReport struct {
	Tokens  int `json:"tokens"`
	Cap     int `json:"cap"`
	Percent int `json:"fill_percent"`
}

type contextOccupancyReport struct {
	Step         int               `json:"step"`
	Identity     contextBandReport `json:"identity"`
	Facts        contextBandReport `json:"facts"`
	WorkingSet   contextBandReport `json:"working_set"`
	Recent       contextBandReport `json:"recent"`
	Total        int               `json:"total"`
	Mark         int               `json:"mark"`
	CapsRecorded bool              `json:"caps_recorded"`
}

type contextForkCounts struct {
	TokensBefore int `json:"tokens_before"`
	TokensAfter  int `json:"tokens_after"`
}

type contextForkReport struct {
	Into   string             `json:"into"`
	Kind   string             `json:"kind,omitempty"`
	Counts *contextForkCounts `json:"counts,omitempty"`
}

type contextReport struct {
	Session                string                  `json:"session,omitempty"`
	Task                   string                  `json:"task,omitempty"`
	Steps                  int                     `json:"steps,omitempty"`
	Occupancy              *contextOccupancyReport `json:"occupancy,omitempty"`
	Unmeasured             string                  `json:"unmeasured,omitempty"`
	Fork                   *contextForkReport      `json:"fork,omitempty"`
	Skipped                []contextSkipReport     `json:"skipped,omitempty"`
	Ceiling                int                     `json:"ceiling,omitempty"`
	BytesPerThousandTokens int                     `json:"bytes_per_thousand_tokens,omitempty"`

	measured *recall.Occupancy
}

func contextVerb(args []string, out, errOut io.Writer) int {
	id, asJSON := "", false
	for _, arg := range args {
		switch {
		case arg == jsonFlag:
			asJSON = true
		case strings.HasPrefix(arg, "-") || id != "":
			_, _ = fmt.Fprintln(errOut, contextUsage)
			return exitUsage
		default:
			id = arg
		}
	}
	store, err := session.Open()
	if err != nil {
		_, _ = fmt.Fprintf(errOut, "tofu context: %v\n", err)
		return exitVerdict
	}
	report, err := contextOf(store, id, recall.ShippedBands())
	if err != nil {
		_, _ = fmt.Fprintf(errOut, "tofu context: %v\n", err)
		return exitVerdict
	}
	if !asJSON {
		_, _ = fmt.Fprint(out, contextText(report), contextSkipText(report.Skipped))
		return exitOK
	}
	if err := writeJSON(out, report); err != nil {
		_, _ = fmt.Fprintf(errOut, "tofu context: %v\n", err)
		return exitVerdict
	}
	return exitOK
}

func contextOf(store *session.Store, id string, build recall.Bands) (contextReport, error) {
	var skipped []contextSkipReport
	if id == "" {
		listing, err := store.Listing()
		if err != nil {
			return contextReport{}, err
		}
		for _, skip := range listing.Skipped {
			skipped = append(skipped, contextSkipReport{Session: skip.ID, Reason: skip.Reason.Error()})
		}
		if len(listing.Sessions) == 0 {
			absence := contextNoSession
			if len(skipped) > 0 {
				absence = contextNoneReadable
			}
			return contextReport{Unmeasured: absence, Skipped: skipped}, nil
		}
		id = listing.Sessions[0].ID
	}
	header, err := store.Header(id)
	if err != nil {
		return contextReport{}, err
	}
	events, err := store.Body(id)
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
	report := contextReport{
		Session:                header.ID,
		Task:                   header.Task,
		Steps:                  len(steps),
		Unmeasured:             contextNeverMeasured,
		Ceiling:                konst.ContextCeilingTokens,
		BytesPerThousandTokens: cfg.BytesPerThousandTokens,
		Skipped:                skipped,
	}
	for _, step := range steps {
		if step.Occupancy != nil {
			caps, recorded := build, step.Bands != nil
			if recorded {
				caps = *step.Bands
			}
			measured := recall.Occupancy{
				Bands:      caps,
				Identity:   step.Occupancy.Identity,
				Facts:      step.Occupancy.Facts,
				WorkingSet: step.Occupancy.WorkingSet,
				Recent:     step.Occupancy.Recent,
			}
			report.measured, report.Unmeasured = &measured, ""
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
	return report, nil
}

func contextSkipText(skipped []contextSkipReport) string {
	if len(skipped) == 0 {
		return ""
	}
	ids := make([]string, len(skipped))
	for i, skip := range skipped {
		ids[i] = skip.Session
	}
	noun := " session that could not be read: "
	if len(ids) > 1 {
		noun = " sessions that could not be read: "
	}
	return "\nstepped over " + strconv.Itoa(len(ids)) + noun + strings.Join(ids, ", ") +
		"\nrun tofu context " + ids[0] + " to see why\n"
}

func contextBand(tokens, capacity int) contextBandReport {
	return contextBandReport{Tokens: tokens, Cap: capacity, Percent: recall.FillPercent(tokens, capacity)}
}

func contextSteps(events []session.Event) ([]turn.StepRow, error) {
	var steps []turn.StepRow
	for i, event := range events {
		switch event.Kind {
		case session.EventStep:
			var step turn.StepRow
			if err := json.Unmarshal(event.Body, &step); err != nil {
				return nil, fmt.Errorf("event %d does not read as a step: %w", i+1, err)
			}
			steps = append(steps, step)
		case session.EventOutcome, session.EventMessage, session.EventRead:
		default:
			return nil, fmt.Errorf("event %d is of unknown kind %q", i+1, event.Kind)
		}
	}
	return steps, nil
}

func contextText(r contextReport) string {
	if r.Session == "" {
		return r.Unmeasured + "\n"
	}
	var out strings.Builder
	fmt.Fprintf(&out, "session %s, %d steps\n", r.Session, r.Steps)
	if r.Task != "" {
		fmt.Fprintf(&out, "task: %s\n", r.Task)
	}
	if r.measured == nil {
		out.WriteString(r.Unmeasured + "\n")
	} else {
		fmt.Fprintf(&out, "measured at step %d\n\n%s", r.Occupancy.Step, recall.OccupancyTable(*r.measured))
		if r.Occupancy.CapsRecorded {
			fmt.Fprintf(&out, "\n%s %d\n", contextCapsRecorded, r.measured.Bands.Target())
		} else {
			fmt.Fprintf(&out, "\n%s %d, so the caps above are this build's, totalling %d\n",
				contextCapsUnrecorded, r.Occupancy.Mark, r.measured.Bands.Target())
		}
	}
	fmt.Fprintf(&out, "%d bytes per thousand tokens, from the recall library\n", r.BytesPerThousandTokens)
	if r.Fork == nil {
		return out.String()
	}
	if r.Fork.Counts == nil {
		fmt.Fprintf(&out, "forked into %s, and no step in this session recorded the counts\n", r.Fork.Into)
		return out.String()
	}
	fmt.Fprintf(&out, "forked into %s as a %s at %d tokens, which began at %d\n",
		r.Fork.Into, r.Fork.Kind, r.Fork.Counts.TokensBefore, r.Fork.Counts.TokensAfter)
	return out.String()
}
