package memtree

import (
	"cmp"
	"context"
	"slices"
	"strings"
	"unicode/utf8"

	"tofu/internal/konst"
)

type Compact func(ctx context.Context, before, left, right string) (string, error)

type Built struct {
	Calls  int
	Free   int
	Failed []error
}

type compacted struct {
	ref  Ref
	text string
	err  error
}

func (s *Store) Build(ctx context.Context, compact Compact) (Built, error) {
	var built Built
	var failed []Ref
	done := make(chan compacted, konst.MemtreeCompactionsAtOnce)
	running := 0
	for len(s.ready) > 0 || running > 0 {
		if len(s.ready) == 0 || running == konst.MemtreeCompactionsAtOnce {
			finished := <-done
			running--
			if finished.err != nil {
				built.Failed, failed = append(built.Failed, finished.err), append(failed, finished.ref)
				continue
			}
			if err := s.keep(finished.ref, clip(strings.TrimSpace(finished.text))); err != nil {
				return built, err
			}
			continue
		}
		ref := s.ready[0]
		s.ready = s.ready[1:]
		left, right := s.halves(ref)
		joined := left
		if ref.Level > 0 {
			joined += "\n" + right
		}
		if len(joined) <= konst.MemtreeLineBytes {
			built.Free++
			if err := s.keep(ref, joined); err != nil {
				return built, err
			}
			continue
		}
		built.Calls++
		running++
		before := s.before(ref)
		go func() {
			text, err := compact(ctx, before, left, right)
			done <- compacted{ref: ref, text: text, err: err}
		}()
	}
	s.ready = failed
	return built, nil
}

func (s *Store) halves(ref Ref) (string, string) {
	if ref.Level == 0 {
		return s.items[ref.Index].rendered(), ""
	}
	return s.nodes[Ref{ref.Level - 1, 2 * ref.Index}], s.nodes[Ref{ref.Level - 1, 2*ref.Index + 1}]
}

func (s *Store) queueUnbuilt() {
	for index := range s.items {
		if _, built := s.nodes[Ref{Index: index}]; !built {
			s.ready = append(s.ready, Ref{Index: index})
		}
	}
	for ref := range s.nodes {
		if ref.Index%2 == 0 {
			s.queueParent(ref)
		}
	}
	slices.SortFunc(s.ready, func(a, b Ref) int { return cmp.Or(cmp.Compare(a.Level, b.Level), cmp.Compare(a.Index, b.Index)) })
}

func (s *Store) queueParent(ref Ref) {
	parent := Ref{ref.Level + 1, ref.Index / 2}
	_, built := s.nodes[parent]
	_, sibling := s.nodes[Ref{ref.Level, ref.Index ^ 1}]
	if !built && sibling {
		s.ready = append(s.ready, parent)
	}
}

func (s *Store) before(ref Ref) string {
	refs := slices.Clone(s.view.Lines)
	for id := s.view.T; id < ref.ID(); id++ {
		refs = append(refs, Ref{Index: id})
	}
	var lines []string
	for _, line := range refs {
		if _, built := s.nodes[line]; !built || line.ID()+line.Span() > ref.ID() {
			break
		}
		lines = append(lines, s.line(line))
	}
	start, size := len(lines), 0
	for start > 0 && size+len(lines[start-1])+1 <= konst.MemtreeContextBytes {
		start--
		size += len(lines[start]) + 1
	}
	return strings.Join(lines[start:], "\n")
}

const emDash = string(rune(8212))

func (s *Store) keep(ref Ref, text string) error {
	text = strings.ReplaceAll(text, emDash, ", ")
	if err := appendLine(s.tree, node{Ref: ref, Text: text}); err != nil {
		return err
	}
	s.nodes[ref] = text
	s.queueParent(ref)
	return nil
}

func clip(text string) string {
	if len(text) <= konst.MemtreeLineBytes {
		return text
	}
	cut := konst.MemtreeLineBytes
	for !utf8.RuneStart(text[cut]) {
		cut--
	}
	return text[:cut]
}
