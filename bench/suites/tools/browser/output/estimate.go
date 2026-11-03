package main

import (
	"fmt"
	"slices"
	"strings"

	"tofu/internal/konst"
)

const (
	quoteMinBytes      = 16
	ompUnchangedAnswer = "unchanged since revision 1"
	ompContextLines    = 3
)

type ompStatus int

const (
	ompFull ompStatus = iota
	ompDelta
	ompUnchanged
	ompCount
)

var ompNames = [ompCount]string{"full", "delta", "unchanged"}

type baseline struct {
	url  string
	body []string
}

func blockBytes(found tree) int {
	return len(found.tabLine) + 1 + linesBytes(found.body)
}

func linesBytes(lines []string) int {
	total := 0
	for _, line := range lines {
		total += len(line) + 1
	}
	return total
}

func ompDiff(baselines map[string]baseline, key string, found tree) (ompStatus, int) {
	previous, seen := baselines[key]
	baselines[key] = baseline{found.url, found.body}
	switch {
	case !seen || previous.url != found.url:
		return ompFull, blockBytes(found)
	case slices.Equal(previous.body, found.body):
		return ompUnchanged, len(found.tabLine) + 1 + len(ompUnchangedAnswer) + 1
	}
	delta := unifiedBytes(previous.body, found.body)
	if delta >= linesBytes(found.body) {
		return ompFull, blockBytes(found)
	}
	return ompDelta, len(found.tabLine) + 1 + delta
}

func unifiedBytes(old, now []string) int {
	prefix := 0
	for prefix < len(old) && prefix < len(now) && old[prefix] == now[prefix] {
		prefix++
	}
	suffix := 0
	for suffix < len(old)-prefix && suffix < len(now)-prefix && old[len(old)-1-suffix] == now[len(now)-1-suffix] {
		suffix++
	}
	start := max(0, prefix-ompContextLines)
	tail := min(ompContextLines, suffix)
	oldEnd, nowEnd := len(old)-suffix, len(now)-suffix
	hunk := fmt.Sprintf("@@ -%d,%d +%d,%d @@\n", start+1, oldEnd+tail-start, start+1, nowEnd+tail-start)
	changed := slices.Concat(old[start:prefix], old[prefix:oldEnd], now[prefix:nowEnd], old[oldEnd:oldEnd+tail])
	return len(hunk) + linesBytes(changed) + len(changed)
}

func textSaved(found tree) int {
	shown := linesBytes(found.body)
	if found.cutBytes == 0 || shown == 0 {
		return found.textBytes
	}
	without := (shown + found.cutBytes) * (shown - found.textBytes) / shown
	return max(0, shown-min(without, konst.BrowserSnapshotMaxBytes))
}

type textWatch struct {
	row        string
	tab, url   string
	texts      []string
	blockBytes int
	used       bool
}

func (w *textWatch) read(said string) {
	if w == nil || w.used {
		return
	}
	w.used = slices.ContainsFunc(w.texts, func(text string) bool { return strings.Contains(said, text) })
}
