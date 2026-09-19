package transform

import (
	"fmt"
	"strings"
)

const diffContextLines = 3

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

func Hunks(before, after string) []Hunk {
	old, next := splitLines(before), splitLines(after)
	table := longestCommon(old, next)
	i, j := 0, 0
	aligned := func() bool { return i < len(old) && j < len(next) && old[i] == next[j] }
	var hunks []Hunk
	for i < len(old) || j < len(next) {
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
	for _, hunk := range hunks {
		head := max(hunk.BeforeStart-diffContextLines, 0)
		tail := min(hunk.BeforeStart+len(hunk.Removed)+diffContextLines, len(old))
		lead, trail := hunk.BeforeStart-head, tail-(hunk.BeforeStart+len(hunk.Removed))
		fmt.Fprintf(out, "@@ -%d,%d +%d,%d @@\n",
			head+1, tail-head,
			hunk.AfterStart-lead+1, lead+len(hunk.Added)+trail)
		for _, line := range old[head:hunk.BeforeStart] {
			writeDiffLine(out, ' ', line)
		}
		for _, line := range hunk.Removed {
			writeDiffLine(out, '-', line)
		}
		for _, line := range hunk.Added {
			writeDiffLine(out, '+', line)
		}
		for _, line := range old[hunk.BeforeStart+len(hunk.Removed) : tail] {
			writeDiffLine(out, ' ', line)
		}
	}
	return out.String()
}

func writeDiffLine(out *strings.Builder, sign byte, line string) {
	out.WriteByte(sign)
	out.WriteString(line)
	if !strings.HasSuffix(line, "\n") {
		out.WriteString("\n\\ no newline at end of file\n")
	}
}
