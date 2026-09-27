package session

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"tofu/interface/tui/markdown"
)

const frames = 20

func counted(calls *int) Prose {
	return func(source string, width int) []string {
		*calls++
		return []string{"prose at " + strconv.Itoa(width) + ": " + strings.TrimSpace(source)}
	}
}

func fixed() func() time.Time {
	at := time.Date(2026, 9, 19, 14, 32, 0, 0, time.UTC)
	return func() time.Time { return at }
}

func TestSessionDeclaresNoPackageLevelVariable(t *testing.T) {
	parsed, err := parser.ParseFile(token.NewFileSet(), "session.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, declared := range parsed.Decls {
		general, ok := declared.(*ast.GenDecl)
		if !ok || general.Tok != token.VAR {
			continue
		}
		for _, spec := range general.Specs {
			for _, name := range spec.(*ast.ValueSpec).Names {
				t.Errorf("session.go declares %s at package level, which any goroutine in the process can reassign", name.Name)
			}
		}
	}
}

func TestProseIsRenderedOncePerWidth(t *testing.T) {
	calls := 0
	model := New(fixed(), counted(&calls))
	model.SetSize(80, 24)
	model.Append(Entry{Kind: Assistant, Body: "**Tofu** reads `toolgate.go`"})
	if calls != 1 {
		t.Fatalf("appending one message called the renderer %d times, want 1", calls)
	}
	for range frames {
		model.View()
	}
	if calls != 1 {
		t.Errorf("%d frames called the renderer %d times, want 1", frames, calls)
	}
	model.SetSize(80, 24)
	model.View()
	if calls != 1 {
		t.Errorf("a resize to the same width called the renderer %d times, want 1", calls)
	}
	model.SetSize(100, 24)
	model.View()
	if calls != 2 {
		t.Errorf("a resize to a new width called the renderer %d times, want 2", calls)
	}
	if !strings.Contains(model.View(), "prose at 92") {
		t.Errorf("the frame does not carry the message rendered at the new width:\n%s", model.View())
	}
}

func TestStreamingRendersMarkdownWithNoParagraphBreakYet(t *testing.T) {
	calls := 0
	model := New(fixed(), counted(&calls))
	model.SetSize(80, 24)
	model.Stream("**Tofu** ")
	model.Stream("reads `toolgate.go`")
	streaming := model.View()
	if calls == 0 {
		t.Fatalf("the growing tail with no closed block never reached the renderer")
	}
	if !strings.Contains(streaming, "prose at 72: **Tofu** reads `toolgate.go`") {
		t.Errorf("a streaming message with no closed block is not rendered as markdown:\n%s", streaming)
	}
	model.Stop()
	complete := model.View()
	if !strings.Contains(complete, "prose at 72") {
		t.Errorf("the complete message is not rendered as markdown:\n%s", complete)
	}
}

func TestAHeadingRendersBeforeTheEntryStopsStreaming(t *testing.T) {
	calls := 0
	model := New(fixed(), counted(&calls))
	model.SetSize(80, 24)
	model.Stream("# Title\n\n")
	model.Stream("the body is still arriving")
	mid := model.View()
	if calls != 2 {
		t.Fatalf("the closed heading plus its growing tail called the renderer %d times while streaming, want 2", calls)
	}
	if !strings.Contains(mid, "prose at 72: # Title") {
		t.Errorf("the heading is not rendered while the entry streams:\n%s", mid)
	}
	if !strings.Contains(mid, "prose at 72: the body is still arriving") {
		t.Errorf("the growing tail is not rendered as markdown while it streams:\n%s", mid)
	}
}

func TestAnUnclosedFenceIsNotReflowedAndHidesItsBackticks(t *testing.T) {
	real := new(markdown.Renderer)
	model := New(fixed(), real.Lines)
	model.SetSize(80, 24)
	model.Stream("Before the fence\n\n```go\n")
	model.Stream("line := 1\n")
	model.Stream("line2 := 2\n")
	frame := ansi.Strip(model.View())
	if strings.Contains(frame, "```") {
		t.Fatalf("the open fence's backticks are visible as text:\n%s", frame)
	}
	if !strings.Contains(frame, "line := 1") || !strings.Contains(frame, "line2 := 2") {
		t.Fatalf("the fence content went missing while open:\n%s", frame)
	}
	rows := strings.Split(frame, "\n")
	first, second := lineIndex(rows, "line := 1"), lineIndex(rows, "line2 := 2")
	if second != first+1 {
		t.Fatalf("the fence content was reflowed across lines: %d then %d\n%s", first, second, frame)
	}
}

func TestAStreamedListNeverShowsARawBacktickOrDash(t *testing.T) {
	real := new(markdown.Renderer)
	model := New(fixed(), real.Lines)
	model.SetSize(80, 24)
	lines := []string{
		"- `cmd/tofu/` (121 files): the verbs\n",
		"- `internal/turn/` (40 files): the loop\n",
		"- `interface/tui/` (30 files): the screen\n",
	}
	for _, line := range lines {
		model.Stream(line)
		frame := ansi.Strip(model.View())
		if strings.Contains(frame, "`") {
			t.Fatalf("a backtick is visible mid-stream:\n%s", frame)
		}
		if strings.Contains(frame, "\n- ") {
			t.Fatalf("a raw leading dash is visible mid-stream:\n%s", frame)
		}
	}
}

func TestAHalfTypedInlineSpanReadsAsTextThenBecomesASpan(t *testing.T) {
	real := new(markdown.Renderer)
	model := New(fixed(), real.Lines)
	model.SetSize(80, 24)
	model.Stream("see the file `cmd/tofu")
	before := ansi.Strip(model.View())
	if !strings.Contains(before, "`cmd/tofu") {
		t.Fatalf("an unclosed span does not read as literal text:\n%s", before)
	}
	model.Stream("/run.go` for the entry point")
	after := ansi.Strip(model.View())
	if strings.Contains(after, "`") {
		t.Fatalf("the closed span still shows its backticks:\n%s", after)
	}
	if !strings.Contains(after, "cmd/tofu/run.go") {
		t.Fatalf("the span text went missing once closed:\n%s", after)
	}
}

func headingLine(frame string) (string, bool) {
	for _, line := range strings.Split(frame, "\n") {
		if strings.Contains(line, "Title") {
			return line, true
		}
	}
	return "", false
}

func TestALineAlreadyDrawnDoesNotChangeShapeAcrossADelta(t *testing.T) {
	real := new(markdown.Renderer)
	model := New(fixed(), real.Lines)
	model.SetSize(80, 24)
	model.Stream("# Title\n\nGrowing paragraph one")
	before, found := headingLine(model.View())
	if !found {
		t.Fatal("the heading is not drawn at all once its blank line closed")
	}
	model.Stream(" word two word three")
	after, found := headingLine(model.View())
	if !found {
		t.Fatal("the heading is not drawn after a delta that only grows the paragraph below it")
	}
	if before != after {
		t.Fatalf("the heading changed shape across a delta that did not close anything new\nbefore: %q\nafter:  %q", before, after)
	}
}

func lineIndex(lines []string, want string) int {
	for index, line := range lines {
		if strings.Contains(line, want) {
			return index
		}
	}
	return -1
}

func TestABlankLineSeparatesAPersonsMessageFromTheAnswerAbove(t *testing.T) {
	model := New(fixed(), counted(new(int)))
	model.SetSize(80, 24)
	model.Append(Entry{Kind: Assistant, Body: "the first reply."})
	model.Append(Entry{Kind: User, Body: "a second question"})
	lines := strings.Split(ansi.Strip(model.View()), "\n")
	at := lineIndex(lines, you)
	if at <= 0 || strings.TrimSpace(lines[at-1]) != "" {
		t.Fatalf("no blank line separates the person's message from the answer above it\n%s", model.View())
	}
}

func TestTheTurnBreakIsLargerThanAParagraphBreakInsideOneAnswer(t *testing.T) {
	real := new(markdown.Renderer)
	model := New(fixed(), real.Lines)
	model.SetSize(80, 24)
	model.Append(Entry{Kind: Assistant, Body: "paragraph one\n\nparagraph two"})
	model.Append(Entry{Kind: User, Body: "a second question"})
	lines := strings.Split(ansi.Strip(model.View()), "\n")
	insideAnswer := lineIndex(lines, "paragraph two")
	turnBreak := lineIndex(lines, you)
	if insideAnswer < 0 || turnBreak < 0 {
		t.Fatal("could not find both markers")
	}
	paragraphGap, turnGap := blankRunBefore(lines, insideAnswer), blankRunBefore(lines, turnBreak)
	if turnGap <= paragraphGap {
		t.Fatalf("the turn break (%d) is not larger than the break inside one answer (%d)\n%s", turnGap, paragraphGap, model.View())
	}
}

func blankRunBefore(lines []string, index int) int {
	count := 0
	for i := index - 1; i >= 0 && strings.TrimSpace(lines[i]) == ""; i-- {
		count++
	}
	return count
}

func TestAShellCallCarriesItsOwnColourNotThePlainToolColour(t *testing.T) {
	model := New(fixed(), counted(new(int)))
	model.SetSize(80, 24)
	model.ChatShowsTools = true
	model.Append(Entry{Kind: Tool, ID: "c1", Head: "read", Body: "internal/turn/loop.go", Status: "84 lines, 2.1 KB"})
	model.Append(Entry{Kind: Tool, ID: "c2", Head: "bash", Body: "go test ./internal/turn/...", Status: "ok 0.4s"})
	var readRow, bashRow string
	for _, line := range strings.Split(model.View(), "\n") {
		switch {
		case strings.Contains(line, "internal/turn/loop.go"):
			readRow = line
		case strings.Contains(line, "go test ./internal/turn/..."):
			bashRow = line
		}
	}
	if readRow == "" || bashRow == "" {
		t.Fatalf("both calls did not draw a row\nread: %q\nbash: %q", readRow, bashRow)
	}
	readOpen, _, _ := strings.Cut(readRow, "⟩")
	bashOpen, _, _ := strings.Cut(bashRow, "⟩")
	if readOpen == bashOpen {
		t.Fatalf("a shell call opens with the same colour as a plain tool call: %q", readOpen)
	}
}

func TestAResultForACallThatIsNoLongerRunningIsKeptAsItsOwnLine(t *testing.T) {
	model := New(fixed(), counted(new(int)))
	model.SetSize(80, 24)
	model.ChatShowsTools = true
	model.Append(Entry{Kind: Tool, ID: "c1", Head: "spawn", Body: "write mine/half.txt"})
	model.Stop()
	if !strings.Contains(model.View(), noResult) {
		t.Fatalf("a call left running when the turn stopped is not stamped, so the late result has nothing to miss:\n%s", model.View())
	}

	model.Finish("c1", Result{Status: "the sub-agent is parked and what it wrote stands"})

	drawn := model.View()
	if !strings.Contains(drawn, "the sub-agent is parked and what it wrote stands") {
		t.Fatalf("a result that arrived after the call was stamped is dropped instead of kept:\n%s", drawn)
	}
	if entry := model.entries[len(model.entries)-1]; entry.Kind != Note {
		t.Fatalf("the late result was kept as a %v rather than a note of its own", entry.Kind)
	}
}

func TestABlankLineSitsAboveTheActivityBlock(t *testing.T) {
	model := New(fixed(), counted(new(int)))
	model.SetSize(80, 24)
	model.Busy = true
	model.Append(Entry{Kind: Tool, ID: "c1", Head: "read", Body: "internal/turn/loop.go"})
	lines := strings.Split(model.View(), "\n")
	rows := model.transcriptRows()
	if strings.TrimSpace(lines[rows]) != "" {
		t.Fatalf("no blank line sits above the activity block, row %d is %q", rows, lines[rows])
	}
	if !strings.Contains(lines[rows+1], "working") {
		t.Fatalf("the activity block does not follow the blank line: %q", lines[rows+1])
	}
}
