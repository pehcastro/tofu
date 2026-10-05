package search

import (
	"cmp"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"tofu/internal/konst"
)

type region uint8

const (
	regionCode region = iota
	regionComment
	regionString
)

type block struct {
	kind      Kind
	symbol    string
	first     int
	last      int
	container bool
}

type outline struct {
	blocks     []block
	regions    []region
	lineStarts []int
}

func declaredUnits(rel, body string, lines []string, hits []hit) ([]Unit, bool) {
	var found outline
	var parsed bool
	switch filepath.Ext(rel) {
	case ".go":
		return goUnits(rel, body, lines, hits)
	case ".ts", ".tsx", ".js", ".jsx":
		found, parsed = braceOutline(body, scriptSyntax)
	case ".rs":
		found, parsed = braceOutline(body, rustSyntax)
	case ".py":
		found, parsed = pythonOutline(body)
	}
	if !parsed {
		return nil, false
	}
	slices.SortFunc(found.blocks, func(outer, inner block) int {
		return cmp.Or(cmp.Compare(outer.first, inner.first), cmp.Compare(inner.last, outer.last))
	})
	var units []Unit
	for _, where := range hits {
		placement := found.placementAt(where)
		decl, inside := found.around(where.line)
		if !inside {
			units = merge(units, frameUnit(rel, lines, where, placement, false), lines)
			continue
		}
		units = merge(units, Unit{
			Path:      rel,
			Kind:      decl.kind,
			Symbol:    decl.symbol,
			FirstLine: decl.first,
			LastLine:  decl.last,
			Matches:   []Match{{Line: where.line, Placement: placement}},
			Body:      strings.Join(lines[decl.first-1:decl.last], "\n"),
		}, lines)
	}
	for _, unit := range units {
		for _, line := range lines[unit.FirstLine-1 : unit.LastLine] {
			if len(line) > konst.SearchLineWidth {
				return nil, false
			}
		}
	}
	return units, true
}

func newOutline(body string) outline {
	starts := []int{0}
	for at := range len(body) {
		if body[at] == '\n' {
			starts = append(starts, at+1)
		}
	}
	return outline{regions: make([]region, len(body)), lineStarts: starts}
}

func (o outline) lineOf(offset int) int {
	return sort.SearchInts(o.lineStarts, offset+1)
}

func (o outline) placementAt(where hit) Placement {
	offset := o.lineStarts[where.line-1] + where.column
	if offset >= len(o.regions) {
		return InCode
	}
	switch o.regions[offset] {
	case regionComment:
		return InComment
	case regionString:
		return InString
	}
	return InCode
}

func (o outline) around(line int) (block, bool) {
	var chain []block
	for _, candidate := range o.blocks {
		if candidate.first <= line && line <= candidate.last {
			chain = append(chain, candidate)
		}
	}
	for index, decl := range chain {
		if decl.container {
			continue
		}
		if index > 0 && decl.kind == KindFunc {
			decl.kind, decl.symbol = KindMethod, chain[index-1].symbol+"."+decl.symbol
		}
		return decl, true
	}
	if len(chain) == 0 {
		return block{}, false
	}
	return chain[len(chain)-1], true
}

func (o outline) mark(from, to int, as region) {
	for at := from; at < to; at++ {
		o.regions[at] = as
	}
}
