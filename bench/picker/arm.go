package picker

import (
	"cmp"
	"slices"
	"time"

	"tofu/internal/llm/quota"
)

type Arm string

const (
	FirstUsable Arm = "first usable"
	RoundRobin  Arm = "round robin"
	Headroom    Arm = "headroom"
)

var Arms = []Arm{FirstUsable, RoundRobin, Headroom}

type Snapshot struct {
	Provider   quota.Provider
	At         time.Time
	Candidates []quota.Candidate
}

func choose(arm Arm, snap Snapshot, held int64) (int64, bool) {
	usable := make([]quota.Candidate, 0, len(snap.Candidates))
	for _, candidate := range snap.Candidates {
		if !quota.Spent(candidate.Report, nil, snap.At) {
			usable = append(usable, candidate)
		}
	}
	if len(usable) == 0 {
		return 0, false
	}
	slices.SortFunc(usable, func(a, b quota.Candidate) int { return cmp.Compare(a.ID, b.ID) })
	switch arm {
	case FirstUsable:
		return usable[0].ID, true
	case RoundRobin:
		for _, candidate := range usable {
			if candidate.ID > held {
				return candidate.ID, true
			}
		}
		return usable[0].ID, true
	case Headroom:
		choice, found := quota.Pick(usable, snap.Provider, nil, snap.At)
		return choice.ID, found
	}
	panic("unknown arm " + string(arm))
}
