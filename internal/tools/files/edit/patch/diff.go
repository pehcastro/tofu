package patch

import (
	"fmt"
	"strings"

	"tofu/internal/konst"
)

type Hunk struct {
	BeforeStart int
	AfterStart  int
	Removed     []string
	Added       []string
}

func splitLines(text string) []string {
	if text == "" {
		return nil
	}
	split := strings.SplitAfter(text, "\n")
	if split[len(split)-1] == "" {
		split = split[:len(split)-1]
	}
	return split
}

func oversizedForDiff(oldLines, nextLines int) bool {
	return oldLines*nextLines > konst.DiffTableMaxCells
}

func Hunks(before, after string) []Hunk {
	old, next := splitLines(before), splitLines(after)
	if oversizedForDiff(len(old), len(next)) {
		return []Hunk{{Removed: old, Added: next}}
	}
	table := longestCommon(old, next)
	i, j := 0, 0
	aligned := func() bool { return i < len(old) && j < len(next) && old[i] == next[j] }
	var hunks []Hunk
	for range len(old) + len(next) {
		if i >= len(old) && j >= len(next) {
			break
		}
		if aligned() {
			i++
			j++
			continue
		}
		hunk := Hunk{BeforeStart: i, AfterStart: j}
		for (i < len(old) || j < len(next)) && !aligned() {
			if j < len(next) && (i == len(old) || table[i][j+1] >= table[i+1][j]) {
				hunk.Added = append(hunk.Added, next[j])
				j++
				continue
			}
			hunk.Removed = append(hunk.Removed, old[i])
			i++
		}
		hunks = append(hunks, hunk)
	}
	return hunks
}

func longestCommon(old, next []string) [][]int {
	table := make([][]int, len(old)+1)
	for i := range table {
		table[i] = make([]int, len(next)+1)
	}
	for i := len(old) - 1; i >= 0; i-- {
		for j := len(next) - 1; j >= 0; j-- {
			if old[i] == next[j] {
				table[i][j] = table[i+1][j+1] + 1
				continue
			}
			table[i][j] = max(table[i+1][j], table[i][j+1])
		}
	}
	return table
}

func Unified(path, before, after string) string {
	hunks := Hunks(before, after)
	if len(hunks) == 0 {
		return ""
	}
	old := splitLines(before)
	out := &strings.Builder{}
	fmt.Fprintf(out, "--- %s\n+++ %s\n", path, path)
	for len(hunks) > 0 {
		group := 1
		for group < len(hunks) && hunks[group].BeforeStart-beforeEnd(hunks[group-1]) <= 2*konst.DiffContextLinesDefault {
			group++
		}
		writeGroup(out, old, hunks[:group])
		hunks = hunks[group:]
	}
	return out.String()
}

func beforeEnd(hunk Hunk) int {
	return hunk.BeforeStart + len(hunk.Removed)
}

func writeGroup(out *strings.Builder, old []string, group []Hunk) {
	first, last := group[0], group[len(group)-1]
	head := max(first.BeforeStart-konst.DiffContextLinesDefault, 0)
	tail := min(beforeEnd(last)+konst.DiffContextLinesDefault, len(old))
	afterLines := tail - head
	for _, hunk := range group {
		afterLines += len(hunk.Added) - len(hunk.Removed)
	}
	fmt.Fprintf(out, "@@ -%d,%d +%d,%d @@\n", head+1, tail-head, first.AfterStart-(first.BeforeStart-head)+1, afterLines)
	at := head
	for _, hunk := range group {
		for _, line := range old[at:hunk.BeforeStart] {
			writeDiffLine(out, ' ', line)
		}
		for _, line := range hunk.Removed {
			writeDiffLine(out, '-', line)
		}
		for _, line := range hunk.Added {
			writeDiffLine(out, '+', line)
		}
		at = beforeEnd(hunk)
	}
	for _, line := range old[at:tail] {
		writeDiffLine(out, ' ', line)
	}
}

func writeDiffLine(out *strings.Builder, sign byte, line string) {
	out.WriteByte(sign)
	out.WriteString(line)
	if !strings.HasSuffix(line, "\n") {
		out.WriteString("\n\\ no newline at end of file\n")
	}
}
