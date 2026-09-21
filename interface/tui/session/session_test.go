package session

import (
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
	if !strings.Contains(model.View(), "prose at 98") {
		t.Errorf("the frame does not carry the message rendered at the new width:\n%s", model.View())
	}
}

func TestStreamingStaysPlainWithNoParagraphBreakYet(t *testing.T) {
	calls := 0
	model := New(fixed(), counted(&calls))
	model.SetSize(80, 24)
	model.Stream("**Tofu** ")
	model.Stream("reads `toolgate.go`")
	streaming := model.View()
	if calls != 0 {
		t.Fatalf("nothing has closed yet, the renderer ran %d times, want 0", calls)
	}
	if !strings.Contains(streaming, "**Tofu** reads `toolgate.go`") {
		t.Errorf("a streaming message with no closed block is not shown as plain text:\n%s", streaming)
	}
	model.Stop()
	complete := model.View()
	if calls != 1 {
		t.Errorf("a message that stopped called the renderer %d times, want 1", calls)
	}
	if streaming == complete {
		t.Error("the frame is unchanged once the message is complete")
	}
	if !strings.Contains(complete, "prose at 78") {
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
	if calls != 1 {
		t.Fatalf("the closed heading called the renderer %d times while streaming, want 1", calls)
	}
	if !strings.Contains(mid, "prose at 78: # Title") {
		t.Errorf("the heading is not rendered while the entry streams:\n%s", mid)
	}
	if !strings.Contains(mid, "the body is still arriving") {
		t.Errorf("the growing tail is not shown plain while it streams:\n%s", mid)
	}
}

func TestAnUnterminatedFenceStaysPlainAndBecomesAFenceWhenItCloses(t *testing.T) {
	real := new(markdown.Renderer)
	model := New(fixed(), real.Lines)
	model.SetSize(80, 24)
	model.Stream("Before the fence\n\n```go\n")
	open := model.View()
	if !strings.Contains(open, "```go") {
		t.Fatalf("an open fence does not show its own literal backticks:\n%s", open)
	}
	model.Stream("line := 1\n")
	model.Stream("```\n\nafter")
	closed := model.View()
	if strings.Contains(closed, "```go") {
		t.Errorf("the fence still shows its raw marker once it closed:\n%s", closed)
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
	lines := strings.Split(model.View(), "\n")
	at := lineIndex(lines, "a second question")
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
	turnBreak := lineIndex(lines, "a second question")
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
	if !strings.Contains(lines[rows+1], "read internal/turn/loop.go") {
		t.Fatalf("the activity block does not follow the blank line: %q", lines[rows+1])
	}
}
