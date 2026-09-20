package shortlist

import (
	"math"
	"regexp"
	"sort"
	"strings"
)

type Ranked struct {
	Path  string
	Score float64
}

var tokenPattern = regexp.MustCompile(`[A-Za-z0-9]+`)

func tokenize(text string) []string {
	return tokenPattern.FindAllString(strings.ToLower(text), -1)
}

const (
	bm25K1 = 1.5
	bm25B  = 0.75
)

func BM25Rank(task string, files []File) []Ranked {
	query := tokenize(task)
	docTokens := make([][]string, len(files))
	docFreq := map[string]int{}
	var totalLen int
	for i, f := range files {
		toks := tokenize(f.Path + " " + f.Content)
		docTokens[i] = toks
		totalLen += len(toks)
		seen := map[string]bool{}
		for _, t := range toks {
			if !seen[t] {
				docFreq[t]++
				seen[t] = true
			}
		}
	}
	n := len(files)
	avgLen := 1.0
	if n > 0 {
		avgLen = float64(totalLen) / float64(n)
	}

	ranked := make([]Ranked, n)
	for i, f := range files {
		termFreq := map[string]int{}
		for _, t := range docTokens[i] {
			termFreq[t]++
		}
		var score float64
		docLen := float64(len(docTokens[i]))
		for _, term := range query {
			freq := float64(termFreq[term])
			if freq == 0 {
				continue
			}
			df := float64(docFreq[term])
			idf := math.Log(1 + (float64(n)-df+0.5)/(df+0.5))
			denom := freq + bm25K1*(1-bm25B+bm25B*docLen/avgLen)
			score += idf * (freq * (bm25K1 + 1)) / denom
		}
		ranked[i] = Ranked{Path: f.Path, Score: score}
	}
	sortRanked(ranked)
	return ranked
}

func GrepRank(task string, files []File) []Ranked {
	terms := tokenize(task)
	ranked := make([]Ranked, len(files))
	for i, f := range files {
		haystack := strings.ToLower(f.Path + "\n" + f.Content)
		var count float64
		for _, term := range terms {
			count += float64(strings.Count(haystack, term))
		}
		ranked[i] = Ranked{Path: f.Path, Score: count}
	}
	sortRanked(ranked)
	return ranked
}

func sortRanked(ranked []Ranked) {
	sort.SliceStable(ranked, func(i, j int) bool { return ranked[i].Score > ranked[j].Score })
}

func HitAt(ranked []Ranked, label []string, k int) bool {
	if k > len(ranked) {
		k = len(ranked)
	}
	for i := 0; i < k; i++ {
		for _, want := range label {
			if ranked[i].Path == want {
				return true
			}
		}
	}
	return false
}
