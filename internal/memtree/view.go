package memtree

import (
	"encoding/json"
	"fmt"
	"math/bits"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

func (s *Store) line(ref Ref) string {
	text, built := s.nodes[ref]
	if !built {
		text = "(not summarized yet: zoom it)"
	}
	return strconv.Itoa(ref.ID()) + "+" + strconv.Itoa(ref.Span()) + "|" + strings.ReplaceAll(text, "\n", " ")
}

func (s *Store) size(lines []Ref) int {
	total := 0
	for _, ref := range lines {
		total += len(s.line(ref)) + 1
	}
	return total
}

func (s *Store) View() string {
	var view strings.Builder
	for _, ref := range s.view.Lines {
		view.WriteString(s.line(ref) + "\n")
	}
	return view.String()
}

func (s *Store) Advance(high, low int) error {
	v := s.view
	for {
		if size := s.size(v.Lines); size > high || v.Merging && size > low {
			v.Lines = s.shrink(v.Lines, v.T, low)
			v.Merging = s.size(v.Lines) > low
		}
		if v.T == len(s.items) {
			break
		}
		v.Lines = append(v.Lines, Ref{Index: v.T})
		v.T++
	}
	s.view = v
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if err := os.WriteFile(s.base+".view.json.tmp", raw, 0o644); err != nil {
		return err
	}
	return os.Rename(s.base+".view.json.tmp", s.base+".view.json")
}

func (s *Store) shrink(lines []Ref, t, target int) []Ref {
	for s.size(lines) > target {
		best := -1
		for k := 0; k+1 < len(lines); k++ {
			a, b := lines[k], lines[k+1]
			if a.Index%2 != 0 || b != (Ref{a.Level, a.Index + 1}) {
				continue
			}
			if _, built := s.nodes[Ref{a.Level + 1, a.Index / 2}]; !built {
				continue
			}
			if best < 0 || moreDue(t, a, lines[best]) {
				best = k
			}
		}
		if best < 0 {
			return lines
		}
		lines = slices.Replace(lines, best, best+2, Ref{lines[best].Level + 1, lines[best].Index / 2})
	}
	return lines
}

func moreDue(t int, a, b Ref) bool {
	sinceA := t - (a.ID() + 2*a.Span() - 1)
	sinceB := t - (b.ID() + 2*b.Span() - 1)
	return sinceA*b.Span() > sinceB*a.Span()
}

func (s *Store) Zoom(id, n int) ([]string, error) {
	if n < 1 || bits.OnesCount(uint(n)) != 1 || id < 0 || id%n != 0 || id+n > len(s.items) {
		return nil, fmt.Errorf("no line %d+%d: n is a power of two, id a multiple of n, and the log holds %d items", id, n, len(s.items))
	}
	if n == 1 {
		return []string{s.raw(id)}, nil
	}
	half := Ref{Level: bits.TrailingZeros(uint(n)) - 1, Index: 2 * id / n}
	return []string{s.line(half), s.line(Ref{half.Level, half.Index + 1})}, nil
}

func (s *Store) Recall(pattern string) ([]string, error) {
	match, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}
	var found []string
	for id, item := range s.items {
		if match.MatchString(item.rendered()) {
			found = append(found, s.raw(id))
		}
	}
	return found, nil
}

func (s *Store) raw(id int) string {
	return strconv.Itoa(id) + "+1|" + s.items[id].rendered()
}
