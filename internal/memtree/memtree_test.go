package memtree

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"
)

type pushEntry struct {
	state int
	keep  bool
}

func push(state int, list []pushEntry) []pushEntry {
	if len(list) == 0 {
		return []pushEntry{{state: state}}
	}
	if !list[0].keep {
		return append([]pushEntry{{state: list[0].state, keep: true}}, list[1:]...)
	}
	return append([]pushEntry{{state: state}}, push(list[0].state, list[1:])...)
}

func pushRefs(list []pushEntry, t int) []Ref {
	var refs []Ref
	end := t + 1
	for _, entry := range list {
		span := end - entry.state
		level := 0
		for 1<<level < span {
			level++
		}
		refs = append([]Ref{{Level: level, Index: entry.state >> level}}, refs...)
		end = entry.state
	}
	return refs
}

const pinnedLineBytes = 32

func pinnedStore(items int) *Store {
	s := &Store{nodes: map[Ref]string{}, items: make([]Item, items)}
	for level := 0; 1<<level <= items; level++ {
		for index := 0; (index+1)<<level <= items; index++ {
			ref := Ref{Level: level, Index: index}
			head := fmt.Sprintf("%d+%d|", ref.ID(), ref.Span())
			s.nodes[ref] = strings.Repeat("x", pinnedLineBytes-len(head)-1)
		}
	}
	return s
}

func TestDueCountsFromThePairsLastItemAndMatchesTaelinsPush(t *testing.T) {
	s := pinnedStore(10)
	lines := []Ref{{2, 0}, {2, 1}, {0, 8}, {0, 9}}
	got := s.shrink(lines, 10, 3*pinnedLineBytes)
	if want := []Ref{{2, 0}, {2, 1}, {1, 4}}; !slices.Equal(got, want) {
		t.Fatalf("at T=10 the batch merged %v, want %v: due measured from the first item merges old lines", got, want)
	}

	const steps = 2048
	s = pinnedStore(steps)
	var list []pushEntry
	lines = nil
	for step := range steps {
		list = push(step, list)
		lines = s.shrink(append(lines, Ref{Index: step}), step+1, len(list)*pinnedLineBytes)
		if want := pushRefs(list, step); !slices.Equal(lines, want) {
			t.Fatalf("at t=%d the view is %v, push keeps %v", step, lines, want)
		}
	}
}

func openLog(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "log.jsonl")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s, path
}

func sized(n int, seed int) Item {
	return Item{Kind: "user", Text: strings.Repeat(string(rune('a'+seed%26)), n)}
}

type counter struct {
	calls atomic.Int64
	fail  func(call int64) error
}

func (c *counter) compact(_ context.Context, _, left, right string) (string, error) {
	call := c.calls.Add(1)
	if c.fail != nil {
		if err := c.fail(call); err != nil {
			return "", err
		}
	}
	return fmt.Sprintf("merged %d: %.200s / %.200s", call, left, right), nil
}

func TestTheViewGrowsOnlyAtItsEndBetweenBatches(t *testing.T) {
	s, _ := openLog(t)
	var c counter
	const budget = 4096
	previous, batches := "", 0
	for i := range 600 {
		if err := s.Append(sized(40+(i*37)%400, i)); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Build(context.Background(), c.compact); err != nil {
			t.Fatal(err)
		}
		batchDue := s.view.Merging || len(previous)+len(s.line(Ref{Index: i}))+1 > budget
		if err := s.Advance(budget, budget/2); err != nil {
			t.Fatal(err)
		}
		view := s.View()
		if len(view) > budget {
			t.Fatalf("after item %d the view is %d bytes, over its budget of %d", i, len(view), budget)
		}
		if !strings.HasPrefix(view, previous) {
			if !batchDue {
				t.Fatalf("item %d rewrote the view below the high mark and with no batch open: %d bytes", i, len(view))
			}
			batches++
		}
		previous = view
	}
	if batches == 0 {
		t.Fatal("600 items never passed the high mark, so this proves nothing about batching")
	}
}

func TestAReopenedStoreKeepsItsViewAndCallsNothing(t *testing.T) {
	s, path := openLog(t)
	for i := range 200 {
		if err := s.Append(sized(100+(i*53)%700, i)); err != nil {
			t.Fatal(err)
		}
	}
	var c counter
	if _, err := s.Build(context.Background(), c.compact); err != nil {
		t.Fatal(err)
	}
	if err := s.Advance(4096, 2048); err != nil {
		t.Fatal(err)
	}
	before := s.View()
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	again, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = again.Close() })
	var second counter
	built, err := again.Build(context.Background(), second.compact)
	if err != nil {
		t.Fatal(err)
	}
	if err := again.Advance(4096, 2048); err != nil {
		t.Fatal(err)
	}
	if second.calls.Load() != 0 || built.Calls != 0 {
		t.Errorf("the reopened store made %d model calls with nothing new", second.calls.Load())
	}
	if again.View() != before {
		t.Errorf("the reopened view differs from the saved one:\n%s\nwant\n%s", again.View(), before)
	}
	if err := again.Advance(2048, 1024); err != nil {
		t.Fatal(err)
	}
	if size := len(again.View()); size > 2048 {
		t.Errorf("a smaller budget with nothing new left the view at %d bytes", size)
	}
}

func treeRefs(t *testing.T, path string) []Ref {
	t.Helper()
	file, err := os.Open(strings.TrimSuffix(path, ".jsonl") + ".tree.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()
	var refs []Ref
	scanner := bufio.NewScanner(file)
	scanner.Buffer(nil, 1<<20)
	for scanner.Scan() {
		var ref Ref
		if _, err := fmt.Sscanf(scanner.Text(), `{"l":%d,"i":%d`, &ref.Level, &ref.Index); err != nil {
			continue
		}
		refs = append(refs, ref)
	}
	return refs
}

func TestAKilledBuildResumesAndBuildsNoNodeTwice(t *testing.T) {
	s, path := openLog(t)
	for i := range 64 {
		if err := s.Append(sized(400, i)); err != nil {
			t.Fatal(err)
		}
	}
	first := counter{fail: func(call int64) error {
		if call > 10 {
			return errors.New("killed")
		}
		return nil
	}}
	built, err := s.Build(context.Background(), first.compact)
	if err != nil {
		t.Fatal(err)
	}
	if len(built.Failed) == 0 {
		t.Fatal("the first build failed nothing, so it was never cut short")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	tree := strings.TrimSuffix(path, ".jsonl") + ".tree.jsonl"
	torn, err := os.OpenFile(tree, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = torn.WriteString(`{"l":3,"i":`)
	_ = torn.Close()

	again, err := Open(path)
	if err != nil {
		t.Fatalf("a torn last line refused the reopen: %v", err)
	}
	t.Cleanup(func() { _ = again.Close() })
	var second counter
	if _, err := again.Build(context.Background(), second.compact); err != nil {
		t.Fatal(err)
	}
	refs := treeRefs(t, path)
	seen := map[Ref]bool{}
	for _, ref := range refs {
		if seen[ref] {
			t.Fatalf("node %d+%d is in the tree twice", ref.ID(), ref.Span())
		}
		seen[ref] = true
	}
	if len(seen) != 127 {
		t.Errorf("the tree holds %d nodes, want 127 for 64 items", len(seen))
	}
	if total := int64(built.Calls-len(built.Failed)) + second.calls.Load(); total != 63 {
		t.Errorf("%d successful calls over both runs, want 63 merges and no leaf", total)
	}
}

func TestASecondWriterIsRefused(t *testing.T) {
	_, path := openLog(t)
	if second, err := Open(path); err == nil {
		_ = second.Close()
		t.Fatal("a second Open on a held log succeeded")
	}
}

func TestLinesAreFreeUpTo512BytesAndClippedAtARuneAbove(t *testing.T) {
	s, _ := openLog(t)
	for _, item := range []Item{sized(506, 0), sized(507, 1)} {
		if err := s.Append(item); err != nil {
			t.Fatal(err)
		}
	}
	built, err := s.Build(context.Background(), func(context.Context, string, string, string) (string, error) {
		return strings.Repeat("é", 100) + string(rune(8212)) + strings.Repeat("é", 300), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if built.Calls != 2 || built.Free != 1 {
		t.Errorf("a 512-byte item and a 513-byte one made %d calls and %d free nodes, want 2 and 1", built.Calls, built.Free)
	}
	for ref, text := range s.nodes {
		if len(text) > 512 || !utf8.ValidString(text) || strings.ContainsRune(text, 8212) {
			t.Errorf("node %v is %d bytes, valid utf-8 %v, em dash %v", ref, len(text), utf8.ValidString(text), strings.ContainsRune(text, 8212))
		}
	}
}

func TestACompactionReadsTheBuiltViewBeforeItsNode(t *testing.T) {
	s, _ := openLog(t)
	var c counter
	for i := range 40 {
		if err := s.Append(sized(300, i)); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Build(context.Background(), c.compact); err != nil {
			t.Fatal(err)
		}
		if err := s.Advance(4096, 2048); err != nil {
			t.Fatal(err)
		}
	}
	view := s.View()
	if err := s.Append(sized(900, 40)); err != nil {
		t.Fatal(err)
	}
	if err := s.Append(sized(900, 41)); err != nil {
		t.Fatal(err)
	}
	seen := map[string]string{}
	var mu sync.Mutex
	_, err := s.Build(context.Background(), func(_ context.Context, before, left, right string) (string, error) {
		mu.Lock()
		seen[left+right] = before
		mu.Unlock()
		return "line", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	leaf := seen[sized(900, 40).Kind+": "+sized(900, 40).Text]
	if leaf+"\n" != view {
		t.Fatalf("the new item's compaction read\n%q\nwant the whole view before it\n%q", leaf, view)
	}
	for _, before := range seen {
		if !strings.HasPrefix(view, before) || strings.Contains(before, "not summarized") {
			t.Fatalf("a compaction read %q, which is not a built prefix of the view", before)
		}
	}

	bulk, _ := openLog(t)
	for i := range 5 {
		if err := bulk.Append(sized(100+800*(i/4), i)); err != nil {
			t.Fatal(err)
		}
	}
	var read string
	if _, err := bulk.Build(context.Background(), func(_ context.Context, before, _, _ string) (string, error) {
		read = before
		return "line", nil
	}); err != nil {
		t.Fatal(err)
	}
	if lines := strings.Split(read, "\n"); len(lines) != 4 || !strings.HasPrefix(lines[3], "3+1|user: ddd") {
		t.Fatalf("a log built before any view gave the fifth item the context %q, want the four items before it", read)
	}
}

func TestNoMoreThanEightCompactionsRunAtOnce(t *testing.T) {
	s, _ := openLog(t)
	for i := range 128 {
		if err := s.Append(sized(500, i)); err != nil {
			t.Fatal(err)
		}
	}
	var running, most atomic.Int64
	var mu sync.Mutex
	_, err := s.Build(context.Background(), func(context.Context, string, string, string) (string, error) {
		now := running.Add(1)
		mu.Lock()
		most.Store(max(most.Load(), now))
		mu.Unlock()
		time.Sleep(2 * time.Millisecond)
		running.Add(-1)
		return "line", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if most.Load() > 8 || most.Load() < 2 {
		t.Errorf("%d compactions ran at once, want between 2 and 8", most.Load())
	}
}

func TestZoomOpensALineDownToTheRawItem(t *testing.T) {
	s, _ := openLog(t)
	long := strings.Repeat("the long one ", 160)
	for i := range 8 {
		item := sized(300, i)
		if i == 7 {
			item.Text = long
		}
		if err := s.Append(item); err != nil {
			t.Fatal(err)
		}
	}
	var c counter
	if _, err := s.Build(context.Background(), c.compact); err != nil {
		t.Fatal(err)
	}
	for _, step := range []struct {
		id, n int
		want  []string
	}{{0, 8, []string{"0+4|", "4+4|"}}, {4, 4, []string{"4+2|", "6+2|"}}, {6, 2, []string{"6+1|", "7+1|"}}} {
		lines, err := s.Zoom(step.id, step.n)
		if err != nil || len(lines) != 2 || !strings.HasPrefix(lines[0], step.want[0]) || !strings.HasPrefix(lines[1], step.want[1]) {
			t.Fatalf("zoom(%d, %d) gave %q, %v", step.id, step.n, lines, err)
		}
	}
	raw, err := s.Zoom(7, 1)
	if err != nil || len(raw) != 1 || !strings.Contains(raw[0], long) {
		t.Fatalf("zoom(7, 1) did not give the item whole: %d lines, %v", len(raw), err)
	}
	for _, bad := range [][2]int{{3, 2}, {0, 3}, {8, 8}, {0, 16}, {-1, 1}, {0, 0}} {
		if _, err := s.Zoom(bad[0], bad[1]); err == nil {
			t.Errorf("zoom(%d, %d) was not refused", bad[0], bad[1])
		}
	}
}

func TestAFailedCompactionIsRetriedByTheNextBuild(t *testing.T) {
	s, _ := openLog(t)
	if err := s.Append(sized(900, 0)); err != nil {
		t.Fatal(err)
	}
	refusing := counter{fail: func(int64) error { return errors.New("refused") }}
	built, err := s.Build(context.Background(), refusing.compact)
	if err != nil || len(built.Failed) != 1 {
		t.Fatalf("a refused compaction gave %d failures, %v", len(built.Failed), err)
	}
	if err := s.Advance(4096, 2048); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(s.View(), "aaaa") || !strings.Contains(s.View(), "0+1|") {
		t.Fatalf("an unbuilt long item shows in the view as %q", s.View())
	}
	var c counter
	if _, err := s.Build(context.Background(), c.compact); err != nil || c.calls.Load() != 1 {
		t.Fatalf("the next build made %d calls, %v", c.calls.Load(), err)
	}
}

func TestAHandWrittenLogKeepsItsLastLineAndRefusesABrokenOne(t *testing.T) {
	path := filepath.Join(t.TempDir(), "log.jsonl")
	if err := os.WriteFile(path, []byte(`{"kind":"user","text":"one"}`+"\n"+`{"kind":"lead","text":"two"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Append(Item{Kind: "user", Text: "three"}); err != nil {
		t.Fatal(err)
	}
	found, err := s.Recall("t[wh]")
	if err != nil || len(found) != 2 {
		t.Fatalf("recall found %q, %v; want two and three", found, err)
	}
	if _, err := s.Recall("("); err == nil {
		t.Error("a broken pattern was not refused")
	}
	_ = s.Close()

	broken := filepath.Join(t.TempDir(), "log.jsonl")
	if err := os.WriteFile(broken, []byte(`{"kind":"user","text":"one"}`+"\nnot json\n"+`{"kind":"user","text":"three"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if s, err := Open(broken); err == nil {
		_ = s.Close()
		t.Fatal("a malformed line in the middle of the log was read")
	}
}
