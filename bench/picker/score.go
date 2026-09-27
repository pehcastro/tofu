package picker

import (
	"bytes"
	"encoding/json"
	"maps"
	"os"
	"slices"
	"time"

	"tofu/internal/llm/quota"
)

const leastAccountsThatSeparate = 2

type ForkPrefix struct {
	SubAgent string
	Tokens   int
}

type Cost struct {
	SubAgent string
	Tokens   int
	Total    int
}

type Score struct {
	Arm   Arm
	Moves int
	Walls int
	Costs []Cost
}

type Result struct {
	Corpus      Corpus
	Snapshots   []Snapshot
	Scores      []Score
	FreshPrefix []ForkPrefix
	PrefixPath  string
	Separable   bool
	ReadAt      time.Time
}

type forkRow struct {
	SubAgent      string `json:"child"`
	Wire          string `json:"wire"`
	SubAgentFirst struct {
		CacheRead int `json:"cache_read"`
	} `json:"child_first"`
}

func FreshPrefixIn(path string) ([]ForkPrefix, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	prefixes := []ForkPrefix{}
	for _, line := range bytes.Split(body, []byte("\n")) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var row forkRow
		if err := json.Unmarshal(line, &row); err != nil {
			return nil, err
		}
		if row.Wire != "anthropic" {
			continue
		}
		prefixes = append(prefixes, ForkPrefix{SubAgent: row.SubAgent, Tokens: row.SubAgentFirst.CacheRead})
	}
	return prefixes, nil
}

func snapshotsOf(readings []Reading) []Snapshot {
	grouped := map[string]Snapshot{}
	for _, reading := range readings {
		provider, at := reading.Report.Provider, reading.Report.FetchedAt
		key := string(provider) + " " + at.UTC().Format(time.RFC3339)
		snap := Snapshot{Provider: provider, At: at, Candidates: grouped[key].Candidates}
		snap.Candidates = append(snap.Candidates, quota.Candidate{
			ID: reading.Row, Provider: provider, Report: reading.Report,
		})
		grouped[key] = snap
	}
	snapshots := make([]Snapshot, 0, len(grouped))
	for _, key := range slices.Sorted(maps.Keys(grouped)) {
		snapshots = append(snapshots, grouped[key])
	}
	return snapshots
}

func replay(arm Arm, snapshots []Snapshot, prefixes []ForkPrefix) Score {
	score := Score{Arm: arm}
	var held int64
	var holding quota.Provider
	for _, snap := range snapshots {
		if snap.Provider != holding {
			held, holding = 0, snap.Provider
		}
		chosen, found := choose(arm, snap, held)
		switch {
		case !found:
			score.Walls++
			held = 0
		case held == 0:
			held = chosen
		case chosen != held:
			score.Moves++
			held = chosen
		}
	}
	for _, prefix := range prefixes {
		score.Costs = append(score.Costs, Cost{
			SubAgent: prefix.SubAgent, Tokens: prefix.Tokens, Total: score.Moves * prefix.Tokens,
		})
	}
	return score
}

func Run(prefixPath string, roots ...string) (Result, error) {
	corpus, err := Gather(roots...)
	if err != nil {
		return Result{}, err
	}
	prefixes, err := FreshPrefixIn(prefixPath)
	if err != nil {
		return Result{}, err
	}
	snapshots := snapshotsOf(corpus.Readings)
	result := Result{
		Corpus:      corpus,
		Snapshots:   snapshots,
		FreshPrefix: prefixes,
		PrefixPath:  prefixPath,
		Separable:   corpus.Accounts >= leastAccountsThatSeparate && len(snapshots) > 0,
		ReadAt:      time.Now(),
	}
	for _, arm := range Arms {
		result.Scores = append(result.Scores, replay(arm, snapshots, prefixes))
	}
	return result, nil
}
