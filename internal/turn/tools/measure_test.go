package tools_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"tofu/internal/turn"
	"tofu/internal/turn/tools"
)

func repository(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(".")
	if err != nil {
		t.Fatalf("locating the repository: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod above this package, so there is no repository to measure over")
		}
		dir = parent
	}
}

type measured struct {
	name    string
	millis  float64
	bytes   int
	content string
}

func measure(t *testing.T, name string, tool turn.Tool, args string) measured {
	t.Helper()
	started := time.Now()
	result, err := tool.Run(context.Background(), json.RawMessage(args))
	elapsed := float64(time.Since(started).Microseconds()) / 1000
	content := result.Content
	if err != nil {
		content = err.Error()
	}
	return measured{name: name, millis: elapsed, bytes: len(content), content: content}
}

func report(t *testing.T, headline string, arms ...measured) {
	t.Helper()
	var out strings.Builder
	out.WriteString(headline + "\n")
	for _, arm := range arms {
		fmt.Fprintf(&out, "%s: %.3f ms, %d bytes to the model\n", arm.name, arm.millis, arm.bytes)
	}
	for _, arm := range arms {
		fmt.Fprintf(&out, "\n%s returned:\n%s\n", arm.name, arm.content)
	}
	t.Log("\n" + out.String())
}

func TestSymbolsAgainstGrepOnTheSameQuestionInThisRepository(t *testing.T) {
	root := repository(t)
	grepTool, _ := tools.NewGrep(root)
	symbolsTool, _ := tools.NewSymbols(root)

	grepped := measure(t, "grep NewRoot under internal/turn", grepTool, `{"pattern":"NewRoot","path":"internal/turn"}`)
	resolved := measure(t, "symbols NewRoot under internal/turn", symbolsTool, `{"name":"NewRoot","path":"internal/turn"}`)
	report(t, "one question, where is NewRoot declared and who calls it", grepped, resolved)

	if grepped.bytes == 0 || resolved.bytes == 0 {
		t.Fatal("one of the two arms answered nothing, so the comparison says nothing")
	}
}

func TestTheMemoAgainstRunningTheSameCallTwiceInThisRepository(t *testing.T) {
	root := repository(t)
	grepTool, _ := tools.NewGrep(root)
	held := tools.NewMemo().Wrap([]turn.Tool{grepTool})[0]
	args := `{"pattern":"func NewRoot"}`

	first := measure(t, "grep over the whole tree, first call", grepTool, args)
	again := measure(t, "grep over the whole tree, second call, no memo", grepTool, args)
	measure(t, "grep over the whole tree, first call through the memo", held, args)
	cached := measure(t, "grep over the whole tree, second call through the memo", held, args)
	report(t, "the same grep twice over this repository", first, again, cached)

	if cached.millis > again.millis {
		t.Fatalf("the memo answered slower than running the tool again: %.3f ms against %.3f ms", cached.millis, again.millis)
	}
}

func TestTheRefusalAgainstTheReadItSavesOnARealFile(t *testing.T) {
	root := repository(t)
	body, err := os.ReadFile(filepath.Join(root, "internal", "search", "find.go"))
	if err != nil {
		t.Fatalf("reading a real file to copy: %v", err)
	}
	scratch := t.TempDir()
	seed(t, scratch, "find.go", string(body))
	editTool, _ := tools.NewEdit(scratch)
	readTool, err := turn.NewReadTool(scratch)
	if err != nil {
		t.Fatalf("building read: %v", err)
	}

	refused := measure(t, "edit, refused with every occurrence named", editTool,
		`{"path":"find.go","old_string":"\treturn out.String()","new_string":"\treturn built.String()"}`)
	read := measure(t, "read of the whole file, the move the bare refusal forced", readTool, `{"path":"find.go"}`)
	report(t, "an ambiguous anchor in a real file", refused, read)

	if !strings.Contains(refused.content, "every occurrence") {
		t.Fatalf("the refused arm is not the refusal being measured:\n%s", refused.content)
	}
	if refused.bytes >= read.bytes {
		t.Fatalf("the refusal returned %d bytes and the read it replaces returned %d, so it saves nothing", refused.bytes, read.bytes)
	}
}

func TestTheRepairedPathAgainstTheGlobItSaves(t *testing.T) {
	root := repository(t)
	globTool, _ := tools.NewGlob(root)

	repaired := measure(t, "glob under konst, a path that does not exist", globTool, `{"pattern":"*.go","path":"konst"}`)
	found := measure(t, "glob for the same name across the tree, the move the bare error forced", globTool, `{"pattern":"konst/*.go"}`)
	report(t, "a path relative to the wrong root", repaired, found)

	if !strings.Contains(repaired.content, "repaired") {
		t.Fatalf("the repaired arm did not repair:\n%s", repaired.content)
	}
}
