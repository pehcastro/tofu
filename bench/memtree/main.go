package main

import (
	"context"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"

	"tofu/internal/konst"
	"tofu/internal/memtree"
)

const items = 10_000

type arm struct {
	name             string
	store            *memtree.Store
	high, low        int
	previous         string
	unchanged, shown int
	lineInputs       int
}

func (a *arm) step() error {
	if err := a.store.Advance(a.high, a.low); err != nil {
		return err
	}
	view := a.store.View()
	shared := 0
	for shared < min(len(view), len(a.previous)) && view[shared] == a.previous[shared] {
		shared++
	}
	a.unchanged += shared
	a.shown += len(a.previous)
	a.lineInputs += strings.Count(view[strings.LastIndexByte(view[:shared], '\n')+1:], "\n")
	a.previous = view
	return nil
}

func fakeCompact(_ context.Context, _, left, right string) (string, error) {
	if right == "" {
		return "c(" + left[:min(len(left), 480)] + ")", nil
	}
	return "m(" + left[:min(len(left), 240)] + "|" + right[:min(len(right), 240)] + ")", nil
}

func run() error {
	dir, err := os.MkdirTemp("", "memtree-bench-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	budget := konst.MemtreeViewBytes
	arms := []*arm{
		{name: "batched, high mark to half", high: budget, low: budget / 2},
		{name: "merging at every append, held at 3/4", high: budget * 3 / 4, low: budget * 3 / 4},
	}
	for _, a := range arms {
		if a.store, err = memtree.Open(filepath.Join(dir, strings.Fields(a.name)[0]+".jsonl")); err != nil {
			return err
		}
		defer func() { _ = a.store.Close() }()
	}
	random := rand.New(rand.NewPCG(1218, 8))
	kinds := []string{"user", "lead", "work"}
	calls := 0
	for i := range items {
		size := 40 + random.IntN(360)
		if random.IntN(10) < 3 {
			size = 600 + random.IntN(2400)
		}
		item := memtree.Item{Kind: kinds[i%len(kinds)], Text: fmt.Sprintf("item %d ", i) + strings.Repeat("x", size)}
		for at, a := range arms {
			if err := a.store.Append(item); err != nil {
				return err
			}
			built, err := a.store.Build(context.Background(), fakeCompact)
			if err != nil {
				return err
			}
			if at == 0 {
				calls += built.Calls
			}
			if err := a.step(); err != nil {
				return err
			}
		}
	}
	fmt.Printf("memtree bench: %d synthetic items, view budget %d bytes, fake compaction function\n", items, budget)
	fmt.Printf("model calls per item: %.3f (%d calls)\n", float64(calls)/items, calls)
	for _, a := range arms {
		fmt.Printf("%s: prefix stability %.2f%% of view bytes unchanged between consecutive appends, %.2f line-inputs per item\n",
			a.name, 100*float64(a.unchanged)/float64(a.shown), float64(a.lineInputs)/items)
	}
	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "memtree bench:", err)
		os.Exit(1)
	}
}
