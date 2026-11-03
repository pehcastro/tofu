package memtree

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
)

func TestTheCheckFindsTheNamesANoteDrops(t *testing.T) {
	for _, c := range []struct {
		name, source, line string
		want               []string
	}{
		{"error and file names dropped", "lead: fixed Remove in cart.go, returns ErrUnknownSKU and ErrInsufficient; added features_test.go",
			"lead: fixed Remove in cart.go", []string{"ErrUnknownSKU", "ErrInsufficient", "features_test.go"}},
		{"a name only inside a longer one is dropped", "lead: ErrInsufficient in my_cart.go and cart.go", "lead: ErrInsufficientStock in my_cart.go", []string{"ErrInsufficient", "cart.go"}},
		{"a path kept as its file name is kept", "lead: changed internal/memtree/build.go", "lead: changed build.go", nil},
		{"a qualified name kept as its last part is kept", "lead: memtree.Store opens it", "lead: Store opens it", nil},
		{"a call kept in backticks is kept", "user: make Remove() fail on an unknown sku", "user: `Remove` fails on unknown sku", nil},
		{"slash joined error names are two names", "lead: ErrUnknownSKU/ErrInsufficient", "lead: ErrUnknownSKU", []string{"ErrInsufficient"}},
		{"a header keeps its snake name", "lead: header sku,qty,price,line_total; see TOFU-1404", "lead: header written", []string{"line_total", "TOFU-1404"}},
		{"camel names and test names", "work: [Tester] TestSubtotal and compactLater and NewCart", "work: tests pass", []string{"TestSubtotal", "compactLater", "NewCart"}},
		{"numbers, versions, prose, acronyms and urls are not names", "lead: Added 0.350 to CSV per-node, 0.5.15-rc1 v2.0.7 ./... see https://docs.astral.sh/uv. Fixed it.",
			"lead: done", nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := missing(c.source, c.line); !slices.Equal(got, c.want) {
				t.Errorf("missing = %q, want %q", got, c.want)
			}
		})
	}
}

func bigStore(t *testing.T, items ...string) *Store {
	t.Helper()
	s, _ := openLog(t)
	for _, text := range items {
		if err := s.Append(Item{Kind: KindLead, Text: text}); err != nil {
			t.Fatal(err)
		}
	}
	s.view.Merging = true
	return s
}

func long(seed int, said string) string {
	return said + " " + strings.Repeat(string(rune('a'+seed)), 900)
}

func noTries(string, string) bool { return false }

func TestThePlanAsksEveryLeafAndEveryMergeAboveThemInOneList(t *testing.T) {
	s := bigStore(t, long(0, "one"), long(1, "two"), long(2, "three"), long(3, "four"))
	if _, err := s.Build(context.Background(), func(context.Context, string, string, string) (string, error) { return "", errors.New("asked") }); err != nil {
		t.Fatal(err)
	}
	_, steps := s.plan(16, noTries)
	var got []string
	for _, step := range steps {
		got = append(got, fmt.Sprintf("%d+%d from %v known %t", step.ID(), step.Span(), step.From, step.Left != ""))
	}
	want := []string{"0+1 from [0 0] known true", "1+1 from [0 0] known true", "2+1 from [0 0] known true", "3+1 from [0 0] known true",
		"0+2 from [1 2] known false", "2+2 from [3 4] known false", "0+4 from [5 6] known false"}
	if !slices.Equal(got, want) {
		t.Fatalf("plan:\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if _, capped := s.plan(5, noTries); len(capped) != 5 {
		t.Errorf("a plan capped at 5 holds %d steps", len(capped))
	}
	_, skipped := s.plan(16, func(left, _ string) bool { return strings.HasPrefix(left, "lead: one") })
	if len(skipped) != 4 || skipped[0].ID() != 1 || skipped[3].ID() != 2 {
		t.Errorf("a tried first leaf left %v in the plan, want leaves 1 to 3 and the merge of 2 and 3 only", skipped)
	}
	s.view.Merging = false
	if _, quiet := s.plan(16, noTries); len(quiet) != 0 {
		t.Errorf("under the high mark the plan holds %d steps, want none", len(quiet))
	}
}

type replies struct {
	one, all atomic.Int64
	line     func(source string) string
	written  func(step Step, lines []string) string
	failAll  error
}

func (r *replies) compact(_ context.Context, _, left, right string) (string, error) {
	r.one.Add(1)
	return r.line(strings.TrimSuffix(left+"\n"+right, "\n")), nil
}

func (r *replies) compactAll(_ context.Context, _ string, steps []Step) ([]string, error) {
	r.all.Add(1)
	if r.failAll != nil {
		return nil, r.failAll
	}
	lines := make([]string, len(steps))
	for i, step := range steps {
		lines[i] = r.written(step, lines)
	}
	return lines, nil
}

func (r *replies) sameAsOne(step Step, lines []string) string {
	left, right := step.Left, step.Right
	if step.From[0] > 0 {
		left = lines[step.From[0]-1]
	}
	if step.From[1] > 0 {
		right = lines[step.From[1]-1]
	}
	return r.line(strings.TrimSuffix(left+"\n"+right, "\n"))
}

func shortLine(source string) string { return "sum " + source[:min(len(source), 300)] }

func held(s *Store) func(func(*Store) error) error {
	return func(building func(*Store) error) error { return building(s) }
}

func TestOneReplyBuildsTheTreeThePerNodeCallsBuild(t *testing.T) {
	items := []string{long(0, "one"), long(1, "two"), long(2, "three"), long(3, "four")}
	head := bigStore(t, items...)
	perNode := replies{line: shortLine}
	if _, err := head.Build(context.Background(), perNode.compact); err != nil {
		t.Fatal(err)
	}
	batched := bigStore(t, items...)
	r := replies{line: shortLine}
	r.written = r.sameAsOne
	built, err := BuildInReplies(context.Background(), held(batched), r.compact, r.compactAll)
	if err != nil || len(built.Failed) > 0 {
		t.Fatal(err, built.Failed)
	}
	if r.all.Load() != 1 || r.one.Load() != 0 {
		t.Errorf("%d replies and %d per-node calls, want 1 and 0", r.all.Load(), r.one.Load())
	}
	if !maps.Equal(head.nodes, batched.nodes) {
		t.Errorf("the batched tree differs from the per-node one:\n%v\nwant\n%v", batched.nodes, head.nodes)
	}

	failing := bigStore(t, items...)
	down := replies{line: shortLine, failAll: errors.New("down")}
	if _, err := BuildInReplies(context.Background(), held(failing), down.compact, down.compactAll); err != nil {
		t.Fatal(err)
	}
	if !maps.Equal(head.nodes, failing.nodes) || down.one.Load() != perNode.one.Load() || down.all.Load() != 1 {
		t.Errorf("a failed reply built %v by %d per-node calls after %d replies, want the per-node tree by %d after 1", failing.nodes, down.one.Load(), down.all.Load(), perNode.one.Load())
	}
}

func TestAThinNoteGetsItsNamesOrIsRedoneAndTheFullerLineIsKept(t *testing.T) {
	var names []string
	for i := range 60 {
		names = append(names, fmt.Sprintf("f%03d_test.go", i))
	}
	s := bigStore(t, long(0, "fixed cart.go with ErrUnknownSKU"), long(1, strings.Join(names, " ")))
	r := replies{line: func(string) string { return "sum f000_test.go" }}
	r.written = func(step Step, _ []string) string {
		if step.Level > 0 {
			return ""
		}
		return "sum " + step.Left[:12]
	}
	built, err := BuildInReplies(context.Background(), held(s), r.compact, r.compactAll)
	if err != nil || len(built.Failed) > 0 {
		t.Fatal(err, built.Failed)
	}
	if got := s.nodes[Ref{Index: 0}]; got != "sum lead: fixed; names: cart.go, ErrUnknownSKU" {
		t.Errorf("leaf 0 is %q, want its missing names appended", got)
	}
	leaf := s.nodes[Ref{Index: 1}]
	if !strings.HasPrefix(leaf, "sum lead: f000_t; names: f000_test.go, f001_test.go") || len(leaf) > 512 {
		t.Errorf("leaf 1 is %q, want the fuller batched line over the redo that keeps one name", leaf)
	}
	if r.one.Load() != 2 || r.all.Load() != 1 {
		t.Errorf("%d per-node calls and %d replies, want 1 reply, then the redo of leaf 1 and the merge alone, per node", r.one.Load(), r.all.Load())
	}
}
