package search

import (
	"strings"
	"unicode"
)

var (
	defaultRefresh = 3
	stopWords      = map[string]bool{"a": true, "an": true, "and": true, "of": true, "the": true}
)

func tokens(text string) []string {
	fields := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	kept := fields[:0]
	for _, field := range fields {
		if !stopWords[field] {
			kept = append(kept, field)
		}
	}
	return kept
}

func (ix *Index) All(words ...string) []string {
	var result []string
	for i, word := range words {
		ids := ix.Lookup(word)
		if i == 0 {
			result = append([]string(nil), ids...)
			continue
		}
		result = intersect(result, ids)
	}
	return result
}

func intersect(a, b []string) []string {
	seen := map[string]bool{}
	for _, id := range b {
		seen[id] = true
	}
	var both []string
	for _, id := range a {
		if seen[id] {
			both = append(both, id)
		}
	}
	return both
}
