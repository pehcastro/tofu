package memory

import (
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync/atomic"

	"tofu/apps/cli/output"
	"tofu/apps/cli/wiring"
	"tofu/internal/agent/loop"
	"tofu/internal/konst"
	"tofu/internal/models/llm/core"
	"tofu/internal/store/memory/tree"
)

const TreeUsage = `tofu memory tree <log.jsonl> [--budget BYTES], tofu memory zoom <log.jsonl> <id> <n>, tofu memory recall <log.jsonl> <regex>`

func Verb(open wiring.Opener) func(ctx context.Context, args []string, out, errOut io.Writer) int {
	return func(ctx context.Context, args []string, out, errOut io.Writer) int {
		if len(args) == 0 || (args[0] != "tree" && args[0] != "zoom" && args[0] != "recall") {
			return shelvesVerb(ctx, args, out, errOut)
		}
		verb, args := args[0], args[1:]
		return treeVerb(ctx, output.Verb{Name: "memory " + verb, UsageLine: TreeUsage, AsJSON: output.JSONAsked(args), Out: out, ErrOut: errOut}, open, verb, args)
	}
}

func treeVerb(ctx context.Context, o output.Verb, open wiring.Opener, verb string, args []string) int {
	budget := konst.MemtreeViewBytes
	var rest []string
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--budget" && verb == "tree" && i+1 < len(args):
			i++
			parsed, err := strconv.Atoi(args[i])
			if err != nil || parsed < 1 {
				return o.Usage(fmt.Errorf("--budget takes a byte count above zero, not %q", args[i]))
			}
			budget = parsed
		case strings.HasPrefix(args[i], "-"):
			return o.Usage(fmt.Errorf("unknown argument %q", args[i]))
		default:
			rest = append(rest, args[i])
		}
	}
	if want := map[string]int{"tree": 1, "zoom": 3, "recall": 2}[verb]; len(rest) != want {
		return o.Usage(fmt.Errorf("memory %s takes %d arguments, not %d", verb, want, len(rest)))
	}
	_, err := os.Stat(rest[0])
	var store *tree.Store
	if err == nil {
		store, err = tree.Open(rest[0])
	}
	if err != nil {
		return o.Fail(err)
	}
	defer func() { _ = store.Close() }()
	var lines []string
	switch verb {
	case "tree":
		return buildTree(ctx, o, open, store, budget)
	case "zoom":
		id, idErr := strconv.Atoi(rest[1])
		n, nErr := strconv.Atoi(rest[2])
		if idErr != nil || nErr != nil {
			return o.Usage(fmt.Errorf("id and n are whole numbers, not %q and %q", rest[1], rest[2]))
		}
		lines, err = store.Zoom(id, n)
	case "recall":
		lines, err = store.Recall(rest[1])
	}
	if err != nil {
		return o.Fail(err)
	}
	_, _ = io.WriteString(o.Out, strings.Join(append(lines, ""), "\n"))
	return output.ExitOK
}

func buildTree(ctx context.Context, o output.Verb, open wiring.Opener, store *tree.Store, budget int) int {
	dir, err := os.Getwd()
	if err != nil {
		return o.Fail(err)
	}
	var asked, hits, fresh, read, written, writtenForAnHour atomic.Int64
	session, err := wiring.ChooseModel(wiring.OnTheBoundWire(wiring.RunOpts{Dir: dir}))
	if err != nil {
		return o.Fail(err)
	}
	memoryModel := wiring.OpenMemoryModel(dir, session, open)
	defer memoryModel.Release()
	one, all := memoryModel.Compactors(func(ctx context.Context, _, _ string, model loop.Model, request core.Request) (core.Decision, error) {
		asked.Add(1)
		decision, err := model.Ask(ctx, request)
		if decision.CacheReadTokens > 0 {
			hits.Add(1)
		}
		fresh.Add(int64(decision.Usage.InputTokens))
		read.Add(int64(decision.CacheReadTokens))
		written.Add(int64(decision.CacheWriteTokens))
		writtenForAnHour.Add(int64(decision.CacheWrite1h))
		return decision, err
	})
	var built tree.Built
	err = store.Advance(budget, budget/2)
	if err == nil {
		built, err = tree.BuildInReplies(ctx, func(building func(*tree.Store) error) error { return building(store) }, one, all)
	}
	if err == nil {
		err = store.Advance(budget, budget/2)
	}
	if err != nil {
		return o.Fail(err)
	}
	view, compacted := store.View(), built.Calls-len(built.Failed)
	_, _ = io.WriteString(o.Out, view)
	_, _ = fmt.Fprintf(o.ErrOut, "%d nodes built now: %d by %d model calls on %s, %d free, %d failed; the view is %d lines, %d bytes of a %d byte budget\n",
		compacted+built.Free, compacted, asked.Load(), memoryModel.Slug, built.Free, len(built.Failed), strings.Count(view, "\n"), len(view), budget)
	_, _ = fmt.Fprintf(o.ErrOut, "input tokens: %d fresh, %d read from the cache by %d of %d calls, %d written to it, %d of them for an hour\n",
		fresh.Load(), read.Load(), hits.Load(), asked.Load(), written.Load(), writtenForAnHour.Load())
	if len(built.Failed) > 0 {
		return o.Fail(fmt.Errorf("%d compactions failed and are tried again on the next build, the first: %w", len(built.Failed), built.Failed[0]))
	}
	return output.ExitOK
}
