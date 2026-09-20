package tools_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"tofu/internal/llm"
	"tofu/internal/turn"
	"tofu/internal/turn/tools"
)

type countingTool struct {
	tool turn.Tool
	runs int
}

func (c *countingTool) Name() string         { return c.tool.Name() }
func (c *countingTool) Definition() llm.Tool { return c.tool.Definition() }
func (c *countingTool) Run(ctx context.Context, raw json.RawMessage) (turn.Result, error) {
	c.runs++
	return c.tool.Run(ctx, raw)
}

func memoized(t *testing.T, root string) (*countingTool, turn.Tool, turn.Tool) {
	t.Helper()
	readTool, err := turn.NewReadTool(root)
	if err != nil {
		t.Fatalf("building read: %v", err)
	}
	writeTool, err := turn.NewWriteTool(root)
	if err != nil {
		t.Fatalf("building write: %v", err)
	}
	counted := &countingTool{tool: readTool}
	wrapped := tools.NewMemo().Wrap([]turn.Tool{counted, writeTool})
	return counted, wrapped[0], wrapped[1]
}

func run(t *testing.T, tool turn.Tool, args string) turn.Result {
	t.Helper()
	result, err := tool.Run(context.Background(), json.RawMessage(args))
	if err != nil {
		t.Fatalf("%s %s: %v", tool.Name(), args, err)
	}
	return result
}

func TestTheSameReadTwiceInOneTurnReachesTheFileSystemOnce(t *testing.T) {
	root := t.TempDir()
	seed(t, root, "a.txt", "first\n")
	counted, readTool, _ := memoized(t, root)

	first := run(t, readTool, `{"path":"a.txt"}`)
	if err := os.Remove(filepath.Join(root, "a.txt")); err != nil {
		t.Fatalf("removing the file behind the cache: %v", err)
	}

	second := run(t, readTool, `{"path":"./a.txt","start_line":0}`)
	if counted.runs != 1 {
		t.Fatalf("the second read reached the file system: the tool ran %d times", counted.runs)
	}
	if !strings.Contains(second.Content, "cached") {
		t.Fatalf("a cached answer must say it was cached:\n%s", second.Content)
	}
	if !strings.Contains(second.Content, first.Content) {
		t.Fatalf("the cached answer is not the first answer:\n%s", second.Content)
	}
}

func TestTheSameFetchTwiceInOneTurnReachesTheServerOnce(t *testing.T) {
	server, requests := servePage(t, "<html><body><h1>a page</h1><p>one paragraph worth reading</p></body></html>")
	counted := &countingTool{tool: tools.NewWeb(webLimits())[0]}
	fetchTool := tools.NewMemo().Wrap([]turn.Tool{counted})[0]
	args := `{"url":"` + server.URL + `/page"}`

	first := run(t, fetchTool, args)
	second := run(t, fetchTool, args)

	if *requests != 1 {
		t.Fatalf("the stub server answered %d requests, want one", *requests)
	}
	if counted.runs != 1 {
		t.Fatalf("the fetch tool ran %d times, so the client's own page cache answered the second call and not the memo", counted.runs)
	}
	if !strings.Contains(second.Content, "cached") || !strings.Contains(second.Content, first.Content) {
		t.Fatalf("the second answer is not the first one held by the memo:\n%s", second.Content)
	}
}

func TestAWriteInvalidatesTheAnswerForThatPath(t *testing.T) {
	root := t.TempDir()
	seed(t, root, "a.txt", "first\n")
	counted, readTool, writeTool := memoized(t, root)

	if got := run(t, readTool, `{"path":"a.txt"}`).Content; !strings.Contains(got, "first") {
		t.Fatalf("the first read did not read the file: %q", got)
	}
	if got := run(t, readTool, `{"path":"a.txt"}`).Content; !strings.Contains(got, "cached") {
		t.Fatalf("the second read was not held, so the invalidation proves nothing: %q", got)
	}
	run(t, writeTool, `{"path":"a.txt","content":"second\n"}`)

	after := run(t, readTool, `{"path":"a.txt"}`).Content
	if strings.Contains(after, "cached") || !strings.Contains(after, "second") {
		t.Fatalf("the read after the write was served stale:\n%s", after)
	}
	if counted.runs != 2 {
		t.Fatalf("the read after the write had to reach the file system: the tool ran %d times", counted.runs)
	}
}
