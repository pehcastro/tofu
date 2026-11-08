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

func Hunks(before, after string) []Hunk {
	old, next := splitLines(before), splitLines(after)
	d := differ{old: old, next: next, removed: make([]bool, len(old)), added: make([]bool, len(next)), budget: konst.DiffTableMaxCells}
	aLo, aHi, bLo, bHi := d.trim(0, len(old), 0, len(next))
	size := pathVectorLen(aHi-aLo, bHi-bLo)
	d.forward, d.backward = make([]int, size), make([]int, size)
	d.compare(aLo, aHi, bLo, bHi)
	var hunks []Hunk
	i, j := 0, 0
	for i < len(old) || j < len(next) {
		if i < len(old) && j < len(next) && !d.removed[i] && !d.added[j] {
			i++
			j++
			continue
		}
		hunk := Hunk{BeforeStart: i, AfterStart: j}
		for i < len(old) && d.removed[i] {
			i++
		}
		for j < len(next) && d.added[j] {
			j++
		}
		hunk.Removed, hunk.Added = old[hunk.BeforeStart:i:i], next[hunk.AfterStart:j:j]
		hunks = append(hunks, hunk)
	}
	return hunks
}

type differ struct {
	old, next         []string
	removed, added    []bool
	forward, backward []int
	budget            int
}

func pathVectorLen(oldLines, nextLines int) int {
	return (oldLines+nextLines+1)/2*2 + 2
}

func (d *differ) trim(aLo, aHi, bLo, bHi int) (int, int, int, int) {
	for aLo < aHi && bLo < bHi && d.old[aLo] == d.next[bLo] {
		aLo++
		bLo++
	}
	for aLo < aHi && bLo < bHi && d.old[aHi-1] == d.next[bHi-1] {
		aHi--
		bHi--
	}
	return aLo, aHi, bLo, bHi
}

func (d *differ) mark(aLo, aHi, bLo, bHi int) {
	for i := aLo; i < aHi; i++ {
		d.removed[i] = true
	}
	for j := bLo; j < bHi; j++ {
		d.added[j] = true
	}
}

func (d *differ) compare(aLo, aHi, bLo, bHi int) {
	aLo, aHi, bLo, bHi = d.trim(aLo, aHi, bLo, bHi)
	if aLo < aHi && bLo < bHi {
		if x, y, found := d.middle(aLo, aHi, bLo, bHi); found {
			d.compare(aLo, x, bLo, y)
			d.compare(x, aHi, y, bHi)
			return
		}
	}
	d.mark(aLo, aHi, bLo, bHi)
}

func (d *differ) middle(aLo, aHi, bLo, bHi int) (int, int, bool) {
	n, m := aHi-aLo, bHi-bLo
	reach := (n + m + 1) / 2
	size := pathVectorLen(n, m)
	forward, backward := d.forward[:size], d.backward[:size]
	for i := range size {
		forward[i], backward[i] = -1, -1
	}
	forward[reach+1], backward[reach+1] = 0, 0
	delta := n - m
	odd := delta%2 != 0
	fStart, fEnd, bStart, bEnd := 0, 0, 0, 0
	for step := range reach {
		d.budget -= 2*step + 1
		if d.budget < 0 {
			return 0, 0, false
		}
		for k := -step + fStart; k <= step-fEnd; k += 2 {
			at := reach + k
			x := forward[at-1] + 1
			if k == -step || k != step && forward[at-1] < forward[at+1] {
				x = forward[at+1]
			}
			y := x - k
			for x < n && y < m && d.old[aLo+x] == d.next[bLo+y] {
				x++
				y++
			}
			forward[at] = x
			switch {
			case x > n:
				fEnd += 2
			case y > m:
				fStart += 2
			case odd:
				other := reach + delta - k
				if other >= 0 && other < size && backward[other] != -1 && x >= n-backward[other] {
					return aLo + x, bLo + y, true
				}
			}
		}
		for k := -step + bStart; k <= step-bEnd; k += 2 {
			at := reach + k
			x := backward[at-1] + 1
			if k == -step || k != step && backward[at-1] < backward[at+1] {
				x = backward[at+1]
			}
			y := x - k
			for x < n && y < m && d.old[aHi-1-x] == d.next[bHi-1-y] {
				x++
				y++
			}
			backward[at] = x
			switch {
			case x > n:
				bEnd += 2
			case y > m:
				bStart += 2
			case !odd:
				other := reach + delta - k
				if other >= 0 && other < size && forward[other] != -1 && forward[other] >= n-x {
					fx := forward[other]
					return aLo + fx, bLo + fx - (other - reach), true
				}
			}
		}
	}
	return 0, 0, false
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
