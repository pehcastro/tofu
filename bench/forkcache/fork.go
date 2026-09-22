package forkcache

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"tofu/bench/corpus"
	"tofu/internal/session"
)

type Request struct {
	Step        int `json:"step"`
	InputTokens int `json:"input_tokens"`
	CacheRead   int `json:"cache_read"`
	CacheWrite  int `json:"cache_write"`
}

type Pair struct {
	Parent     string  `json:"parent"`
	Child      string  `json:"child"`
	Wire       string  `json:"wire"`
	Model      string  `json:"model"`
	Day        string  `json:"day"`
	ForkKind   string  `json:"fork_kind"`
	ParentLast Request `json:"parent_last"`
	ChildFirst Request `json:"child_first"`
}

type PrefixFate string

const (
	FateRead    PrefixFate = "read"
	FateWritten PrefixFate = "written"
	FateBoth    PrefixFate = "read and written"
	FateNeither PrefixFate = "neither"
)

func (p Pair) Fate() PrefixFate {
	switch {
	case p.ChildFirst.CacheRead > 0 && p.ChildFirst.CacheWrite > 0:
		return FateBoth
	case p.ChildFirst.CacheRead > 0:
		return FateRead
	case p.ChildFirst.CacheWrite > 0:
		return FateWritten
	default:
		return FateNeither
	}
}

func (p Pair) PrefixIfContinued() int {
	return p.ParentLast.CacheRead + p.ParentLast.CacheWrite
}

const WireReportingCacheWrite = "anthropic"

func (p Pair) ReportsCacheWrite() bool { return p.Wire == WireReportingCacheWrite }

func PairsIn(store *session.Store) ([]Pair, []session.Skip, error) {
	listing, err := store.Listing()
	if err != nil {
		return nil, nil, err
	}
	byID := make(map[string]session.Header, len(listing.Sessions))
	for _, header := range listing.Sessions {
		byID[header.ID] = header
	}
	var pairs []Pair
	skipped := listing.Skipped
	for _, child := range listing.Sessions {
		parent, known := byID[child.Parent]
		if child.Parent == "" || !known {
			continue
		}
		parentSteps, err := stepsOf(store, parent.ID)
		if err != nil {
			skipped = append(skipped, session.Skip{ID: parent.ID, Reason: err})
			continue
		}
		childSteps, err := stepsOf(store, child.ID)
		if err != nil {
			skipped = append(skipped, session.Skip{ID: child.ID, Reason: err})
			continue
		}
		if len(parentSteps) == 0 || len(childSteps) == 0 {
			skipped = append(skipped, session.Skip{ID: child.ID, Reason: fmt.Errorf("forkcache: %d parent steps and %d child steps, so one side of the fork billed no request to read", len(parentSteps), len(childSteps))})
			continue
		}
		pairs = append(pairs, Pair{
			Parent:     parent.ID,
			Child:      child.ID,
			Wire:       child.Wire,
			Model:      child.Model,
			Day:        child.At.Format(time.DateOnly),
			ForkKind:   child.ForkKind,
			ParentLast: parentSteps[len(parentSteps)-1],
			ChildFirst: childSteps[0],
		})
	}
	slices.SortFunc(pairs, func(a, b Pair) int { return strings.Compare(a.Child, b.Child) })
	return pairs, skipped, nil
}

type recordedStep struct {
	Index            int `json:"index"`
	PromptTokens     int `json:"prompt_tokens"`
	CacheReadTokens  int `json:"cache_read_tokens"`
	CacheWriteTokens int `json:"cache_write_tokens"`
}

func stepsOf(store *session.Store, id string) ([]Request, error) {
	events, err := store.Body(id)
	if err != nil {
		return nil, err
	}
	var steps []Request
	for _, event := range events {
		if event.Kind != session.EventStep {
			continue
		}
		var step recordedStep
		if err := json.Unmarshal(event.Body, &step); err != nil {
			return nil, fmt.Errorf("%s: a recorded step does not parse: %w", id, err)
		}
		steps = append(steps, Request{
			Step:        step.Index,
			InputTokens: step.PromptTokens,
			CacheRead:   step.CacheReadTokens,
			CacheWrite:  step.CacheWriteTokens,
		})
	}
	return steps, nil
}

func ReadPairs(path string) ([]Pair, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var pairs []Pair
	for number, line := range strings.Split(string(body), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if leaks := corpus.LeaksIn(line); len(leaks) > 0 {
			return nil, fmt.Errorf("%s line %d carries %q, which came off the recording machine: re-extract it through corpus.Scrub", path, number+1, leaks)
		}
		var pair Pair
		if err := json.Unmarshal([]byte(line), &pair); err != nil {
			return nil, fmt.Errorf("%s line %d is not a fork pair: %w", path, number+1, err)
		}
		pairs = append(pairs, pair)
	}
	return pairs, nil
}

func WritePairs(path string, pairs []Pair) error {
	var out strings.Builder
	for _, pair := range pairs {
		line, err := json.Marshal(pair)
		if err != nil {
			return err
		}
		out.Write(line)
		out.WriteString("\n")
	}
	return os.WriteFile(path, []byte(out.String()), 0o600)
}

const pairRow = "%-26v %-10v %-11v %-13v %10v %10v %13v %9v %11v %12v  %v\n"

func Table(pairs []Pair) string {
	var report strings.Builder
	fmt.Fprintf(&report, pairRow,
		"child", "wire", "day", "fork kind", "kept read", "kept write", "if continued", "first in", "first read", "first write", "fate")
	for _, pair := range pairs {
		fmt.Fprintf(&report, pairRow,
			pair.Child, pair.Wire, pair.Day, pair.ForkKind,
			pair.ParentLast.CacheRead, pair.ParentLast.CacheWrite, pair.PrefixIfContinued(),
			pair.ChildFirst.InputTokens, pair.ChildFirst.CacheRead, pair.ChildFirst.CacheWrite, pair.Fate())
	}
	return report.String()
}
