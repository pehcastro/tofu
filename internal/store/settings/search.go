package settings

import (
	"sort"
	"strings"
)

type Match struct {
	Spec  Spec
	Score int
}

func (s *Store) Search(query string) []Match {
	if strings.TrimSpace(query) == "" {
		matches := make([]Match, len(s.table))
		for i, spec := range s.table {
			matches[i] = Match{Spec: spec}
		}
		return matches
	}
	lowered := strings.ToLower(query)
	var matches []Match
	for _, spec := range s.table {
		if score, hit := fuzzyScore(strings.ToLower(spec.Label), lowered); hit {
			matches = append(matches, Match{Spec: spec, Score: score})
		}
	}
	sort.SliceStable(matches, func(i, j int) bool { return matches[i].Score > matches[j].Score })
	return matches
}

func fuzzyScore(text, query string) (int, bool) {
	score, at := 0, 0
	for _, r := range query {
		found := strings.IndexRune(text[at:], r)
		if found < 0 {
			return 0, false
		}
		if found == 0 {
			score += 2
		} else {
			score++
		}
		at += found + len(string(r))
	}
	return score, true
}
