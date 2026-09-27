package recall

import (
	"encoding/json"
	"sort"

	"tofu/bench/corpus"
	rc "tofu/internal/recall"
	"tofu/internal/session"
	"tofu/internal/turn"
)

type SessionPeak struct {
	ID     string
	Task   string
	Peak   int
	Target int
}

type ForkEvent struct {
	Session      string
	Step         int
	Into         string
	Kind         string
	TokensBefore int
	TokensAfter  int
	KnownSources int
	Refetches    int
}

type CorpusReach struct {
	Measured    []SessionPeak
	Skipped     []corpus.SkippedTurn
	Forks       []ForkEvent
	Compactions int
}

const (
	ReasonNoSteps            = "the session carries no steps"
	ReasonNoOccupancy        = "recorded before occupancy reached the step row"
	ReasonPreOccupancySchema = "the single-file schema this session predates: the occupancy field did not exist yet"
)

func WalkCorpusReach(sessionsDir string) (CorpusReach, error) {
	store := session.NewStore(sessionsDir)
	listing, err := store.Listing()
	if err != nil {
		return CorpusReach{}, err
	}
	var reach CorpusReach
	for _, skip := range listing.Skipped {
		reach.Skipped = append(reach.Skipped, corpus.SkippedTurn{Path: skip.ID, Reason: skip.Reason.Error()})
	}
	for _, header := range listing.Sessions {
		steps, err := readSteps(store, header.ID)
		if err != nil {
			reach.Skipped = append(reach.Skipped, corpus.SkippedTurn{Path: header.ID, Reason: err.Error()})
			continue
		}
		if len(steps) == 0 {
			reach.Skipped = append(reach.Skipped, corpus.SkippedTurn{Path: header.ID, Reason: ReasonNoSteps})
			continue
		}
		peak, target, ok := peakOccupancy(steps)
		if !ok {
			reason := ReasonNoOccupancy
			if store.Shape(header.ID) == session.ShapeSingleFile {
				reason = ReasonPreOccupancySchema
			}
			reach.Skipped = append(reach.Skipped, corpus.SkippedTurn{Path: header.ID, Reason: reason})
			continue
		}
		reach.Measured = append(reach.Measured, SessionPeak{ID: header.ID, Task: header.Task, Peak: peak, Target: target})
		for _, step := range steps {
			if step.Compaction != nil {
				reach.Compactions++
			}
			if step.Fork == nil {
				continue
			}
			refetches, err := refetchesInto(store, step.Fork.Into, step.Fork.Carry.Results)
			if err != nil {
				return CorpusReach{}, err
			}
			reach.Forks = append(reach.Forks, ForkEvent{
				Session:      header.ID,
				Step:         step.Fork.Step,
				Into:         step.Fork.Into,
				Kind:         string(step.Fork.Kind),
				TokensBefore: step.Fork.TokensBefore,
				TokensAfter:  step.Fork.TokensAfter,
				KnownSources: len(step.Fork.Carry.Results),
				Refetches:    refetches,
			})
		}
	}
	sort.Slice(reach.Measured, func(i, j int) bool { return reach.Measured[i].Peak > reach.Measured[j].Peak })
	return reach, nil
}

func readSteps(store *session.Store, id string) ([]turn.StepRow, error) {
	events, err := store.Body(id)
	if err != nil {
		return nil, err
	}
	var steps []turn.StepRow
	for _, event := range events {
		if event.Kind != session.EventStep {
			continue
		}
		var step turn.StepRow
		if err := json.Unmarshal(event.Body, &step); err != nil {
			return nil, err
		}
		steps = append(steps, step)
	}
	return steps, nil
}

func peakOccupancy(steps []turn.StepRow) (peak, target int, found bool) {
	for _, step := range steps {
		if step.Occupancy == nil {
			continue
		}
		found = true
		if total := step.Occupancy.Total(); total > peak {
			peak, target = total, step.Occupancy.Target
		}
	}
	return peak, target, found
}

func refetchesInto(store *session.Store, forkID string, known []rc.CarriedResult) (int, error) {
	knownKeys := make(map[string]bool, len(known))
	for _, result := range known {
		knownKeys[result.Key] = true
	}
	steps, err := readSteps(store, forkID)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, step := range steps {
		for _, call := range step.ToolCalls {
			if knownKeys[call.Tool+" "+string(call.Args)] {
				count++
			}
		}
	}
	return count, nil
}

func (r CorpusReach) Highest() int {
	if len(r.Measured) == 0 {
		return 0
	}
	return r.Measured[0].Peak
}

func (r CorpusReach) Median() int {
	n := len(r.Measured)
	if n == 0 {
		return 0
	}
	mid := n / 2
	if n%2 == 1 {
		return r.Measured[mid].Peak
	}
	return (r.Measured[mid-1].Peak + r.Measured[mid].Peak) / 2
}

func (r CorpusReach) WithinShareOfCeiling(ceiling int, share float64) int {
	count := 0
	for _, p := range r.Measured {
		if float64(p.Peak) >= float64(ceiling)*share {
			count++
		}
	}
	return count
}

func (r CorpusReach) Crossed() []SessionPeak {
	var crossed []SessionPeak
	for _, p := range r.Measured {
		if p.Target > 0 && p.Peak > p.Target {
			crossed = append(crossed, p)
		}
	}
	return crossed
}

func (r CorpusReach) TotalRefetches() int {
	total := 0
	for _, fork := range r.Forks {
		total += fork.Refetches
	}
	return total
}
