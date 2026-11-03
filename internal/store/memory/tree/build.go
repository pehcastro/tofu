package memtree

import (
	"cmp"
	"context"
	"errors"
	"maps"
	"regexp"
	"slices"
	"strings"
	"sync"
	"unicode/utf8"

	"tofu/internal/konst"
	"tofu/internal/safe"
	"tofu/internal/sys"
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
	summarizing := s.passesHighMark()
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
		if !summarizing {
			failed = append(failed, ref)
			continue
		}
		built.Calls++
		running++
		before := s.before(ref)
		report := sys.Crashes(".")
		safe.Go(report, "memtree compaction", func() {
			finished := compacted{ref: ref}
			finished.err = safe.Call(report, "memtree compaction", func() (err error) {
				finished.text, err = compact(ctx, before, left, right)
				return err
			})
			done <- finished
		})
	}
	s.ready = failed
	return built, nil
}

var errNotAnswered = errors.New("this summary is asked with the log closed and kept on the next pass")

type summary struct {
	line string
	err  error
}

func BuildInReplies(ctx context.Context, open func(func(*Store) error) error, one Compact, all CompactAll) (Built, error) {
	var mu sync.Mutex
	answers, thin, tried, asked := map[halvesAsked]summary{}, map[halvesAsked]string{}, map[halvesAsked]bool{}, map[halvesAsked]string{}
	answered := func(_ context.Context, before, left, right string) (string, error) {
		mu.Lock()
		defer mu.Unlock()
		if answer, known := answers[halvesAsked{left, right}]; known {
			return answer.line, answer.err
		}
		asked[halvesAsked{left, right}] = before
		return "", errNotAnswered
	}
	var total Built
	batching := true
	for {
		var built Built
		var before string
		var steps []Step
		if err := open(func(s *Store) (err error) {
			if built, err = s.Build(ctx, answered); err == nil && batching {
				before, steps = s.plan(konst.MemtreeCompactionsAtOnce, func(left, right string) bool { return tried[halvesAsked{left, right}] })
			}
			return err
		}); err != nil {
			return total, err
		}
		total.Free += built.Free
		if len(steps) == 1 {
			tried[halvesAsked{steps[0].Left, steps[0].Right}], steps = true, nil
		}
		redo := maps.Clone(asked)
		maps.DeleteFunc(redo, func(q halvesAsked, _ string) bool { return batching && !tried[q] })
		clear(asked)
		if len(steps) == 0 && len(redo) == 0 {
			total.Calls, total.Failed = total.Calls+built.Calls, built.Failed
			return total, nil
		}
		total.Calls += built.Calls - len(built.Failed)
		var asking sync.WaitGroup
		report := sys.Crashes(".")
		if len(steps) > 0 {
			asking.Go(func() {
				var lines []string
				err := safe.Call(report, "memory compaction", func() (err error) {
					lines, err = all(ctx, before, steps)
					return err
				})
				mu.Lock()
				defer mu.Unlock()
				batching = err == nil
				for _, got := range settle(steps, lines) {
					tried[got.asked] = true
					switch {
					case !got.thin:
						answers[got.asked] = summary{line: got.line}
					case got.line != "":
						thin[got.asked] = got.line
					}
				}
			})
		}
		slots := make(chan struct{}, konst.MemtreeCompactionsAtOnce)
		for q, before := range redo {
			slots <- struct{}{}
			asking.Go(func() {
				defer func() { <-slots }()
				var line string
				err := safe.Call(report, "memory compaction", func() (err error) {
					line, err = one(ctx, before, q.left, q.right)
					return err
				})
				mu.Lock()
				defer mu.Unlock()
				if candidate, kept := thin[q]; kept && (err != nil || len(missing(q.source(), candidate)) < len(missing(q.source(), line))) {
					line, err = candidate, nil
				}
				answers[q] = summary{line, err}
			})
		}
		asking.Wait()
	}
}

func (s *Store) passesHighMark() bool {
	size := s.size(s.view.Lines)
	for id := s.view.T; id < len(s.items); id++ {
		size += len(s.line(Ref{Index: id})) + 1
	}
	return s.view.Merging || size > cmp.Or(s.view.High, konst.MemtreeViewBytes)
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
	offsets := make([]int, len(lines)+1)
	for i, line := range lines {
		offsets[i+1] = offsets[i] + len(line) + 1
	}
	fits := 0
	for offsets[len(lines)]-offsets[fits] > konst.MemtreeContextBytes {
		fits++
	}
	startStep := konst.MemtreeCompactionsAtOnce * konst.MemtreeLineBytes
	alignedOffset := (offsets[fits] + startStep - 1) / startStep * startStep
	start := fits
	for start < len(lines) && offsets[start] < alignedOffset {
		start++
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

type Step struct {
	Ref
	Left, Right string
	From        [2]int
}

type CompactAll func(ctx context.Context, before string, steps []Step) ([]string, error)

func (s *Store) plan(most int, tried func(left, right string) bool) (string, []Step) {
	if !s.passesHighMark() {
		return "", nil
	}
	sim := &Store{items: s.items, nodes: maps.Clone(s.nodes), ready: slices.Clone(s.ready)}
	slices.SortFunc(sim.ready, func(a, b Ref) int { return cmp.Or(cmp.Compare(a.Level, b.Level), cmp.Compare(a.Index, b.Index)) })
	task, elsewhere := map[Ref]int{}, map[Ref]bool{}
	var steps []Step
	for len(sim.ready) > 0 && len(steps) < most {
		ref := sim.ready[0]
		sim.ready = sim.ready[1:]
		step := Step{Ref: ref}
		step.Left, step.Right = sim.halves(ref)
		waits := false
		if ref.Level > 0 {
			for i, child := range [2]Ref{{ref.Level - 1, 2 * ref.Index}, {ref.Level - 1, 2*ref.Index + 1}} {
				step.From[i] = task[child]
				waits = waits || elsewhere[child]
			}
		}
		sim.nodes[ref] = strings.Repeat("-", konst.MemtreeLineBytes)
		sim.queueParent(ref)
		if waits || step.From == [2]int{} && tried(step.Left, step.Right) {
			elsewhere[ref] = true
			continue
		}
		if step.From[0] > 0 {
			step.Left = ""
		}
		if step.From[1] > 0 {
			step.Right = ""
		}
		steps = append(steps, step)
		task[ref] = len(steps)
	}
	if len(steps) == 0 {
		return "", nil
	}
	return s.before(steps[0].Ref), steps
}

type halvesAsked struct{ left, right string }

func (h halvesAsked) source() string { return strings.TrimSuffix(h.left+"\n"+h.right, "\n") }

type settled struct {
	asked halvesAsked
	line  string
	thin  bool
}

func settle(steps []Step, lines []string) []settled {
	kept := make([]string, len(steps)+1)
	var all []settled
	for i, step := range steps {
		left, right := cmp.Or(kept[step.From[0]], step.Left), cmp.Or(kept[step.From[1]], step.Right)
		if left == "" || step.Level > 0 && right == "" {
			continue
		}
		got := settled{asked: halvesAsked{left, right}, thin: true}
		if i < len(lines) && strings.TrimSpace(lines[i]) != "" {
			got.line, got.thin = named(got.asked.source(), clip(strings.ReplaceAll(strings.TrimSpace(lines[i]), emDash, ", ")))
		}
		if !got.thin {
			kept[i+1] = got.line
		}
		all = append(all, got)
	}
	return all
}

func named(source, line string) (string, bool) {
	sep := "; names: "
	for _, name := range missing(source, line) {
		if len(line)+len(sep)+len(name) > konst.MemtreeLineBytes {
			return line, true
		}
		line, sep = line+sep+name, ", "
	}
	return line, false
}

var (
	nameWord  = regexp.MustCompile(`[A-Za-z0-9_.\-/\\]+`)
	fileName  = regexp.MustCompile(`^[A-Za-z0-9_\-]*[A-Za-z][A-Za-z0-9_\-]*(\.[A-Za-z0-9_\-]+)*\.(go|mod|sum|txt|md|json|jsonl|yaml|yml|toml|csv|rs|ts|tsx|js|py|sh|ps1|html|css|sql|c|h|java|rb|lock|log|golden|patch|xml|ini|env|bat|exe)$`)
	qualified = regexp.MustCompile(`^[a-z][A-Za-z0-9_]*\.[A-Z][A-Za-z0-9_]*$`)
	humped    = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]*[a-z0-9][A-Z][A-Za-z0-9]*$`)
	snake     = regexp.MustCompile(`^[a-z][a-z0-9]*(_[a-z0-9]+)+$`)
	ticketID  = regexp.MustCompile(`^[A-Z]{2,}-[0-9]+$`)
	titled    = regexp.MustCompile(`^[A-Z][A-Za-z0-9_]*[a-z][A-Za-z0-9_]*$`)
)

func pathParts(word string) []string {
	return strings.FieldsFunc(word, func(r rune) bool { return r == '/' || r == '\\' })
}

func Names(text string) []string {
	var found []string
	seen := map[string]bool{}
	add := func(name string) {
		if !seen[name] {
			seen[name], found = true, append(found, name)
		}
	}
	for _, at := range nameWord.FindAllStringIndex(text, -1) {
		start, end := at[0], at[1]
		word := strings.TrimRight(text[start:end], ".-/\\")
		parts := pathParts(word)
		switch {
		case start > 0 && text[start-1] == ':' || len(parts) == 0:
		case len(parts) > 1 && fileName.MatchString(parts[len(parts)-1]):
			add(word)
		default:
			marked := end < len(text) && text[end] == '(' || start > 0 && text[start-1] == '`' && end < len(text) && text[end] == '`'
			for _, part := range parts {
				if fileName.MatchString(part) || qualified.MatchString(part) || humped.MatchString(part) || snake.MatchString(part) || ticketID.MatchString(part) || marked && titled.MatchString(part) {
					add(part)
				}
			}
		}
	}
	return found
}

func missing(source, line string) []string {
	present := map[string]bool{}
	for _, word := range nameWord.FindAllString(line, -1) {
		word = strings.TrimRight(word, ".-/\\")
		present[word] = true
		for _, part := range pathParts(word) {
			present[part] = true
		}
	}
	var dropped []string
	for _, name := range Names(source) {
		parts := pathParts(name)
		short := parts[len(parts)-1]
		if qualified.MatchString(name) {
			short = name[strings.IndexByte(name, '.')+1:]
		}
		if !present[name] && !present[short] {
			dropped = append(dropped, name)
		}
	}
	return dropped
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
