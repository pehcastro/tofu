package session

import (
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"tofu/interface/tui/markdown"
)

const resizeTurns = 14

type step struct {
	name string
	do   func(m *Model)
}

func talked(m *Model) {
	for turn := range resizeTurns {
		number := strconv.Itoa(turn)
		m.Append(Entry{Kind: User, Body: "question " + number + " " + strings.Repeat("what changes when the width does ", 4)})
		m.Start()
		m.Returned()
		for call := range 3 {
			id := "toolu_" + number + "_" + strconv.Itoa(call)
			m.Append(Entry{Kind: Tool, ID: id, Head: "read", Body: "internal/turn/loop.go"})
			m.Finish(id, Result{Status: "84 lines"})
		}
		m.Append(Entry{Kind: Assistant, ID: "msg_" + number, Body: "Answer " + number + ".\n\n" +
			strings.Repeat("A paragraph long enough to wrap differently at every width the terminal takes. ", 4) + "\n\n- one\n- two"})
		m.Close("cooked for", "")
		m.Stop()
	}
}

func resizeSteps() []step {
	return []step{
		{"narrowed to 80", func(m *Model) { m.SetSize(80, 36) }},
		{"widened back to 120", func(m *Model) { m.SetSize(120, 36) }},
		{"a page up", func(m *Model) { m.Scroll("pgup") }},
		{"a wheel up", func(m *Model) { m.Scroll(WheelUp) }},
		{"narrowed to 61 while scrolled back", func(m *Model) { m.SetSize(61, 30) }},
		{"followed again", func(m *Model) { m.Follow() }},
		{"tools shown", func(m *Model) { m.ChatShowsTools = true }},
		{"tools folded", func(m *Model) { m.ChatShowsTools = false }},
		{"a turn started", func(m *Model) { m.Append(Entry{Kind: User, Body: "one more"}); m.Start() }},
		{"a message queued", func(m *Model) { m.Queue("after this", "after this", nil) }},
		{"a call to be gated", func(m *Model) { m.Append(Entry{Kind: Tool, ID: "toolu_gate", Head: "bash", Body: "git push --force"}) }},
		{"the gate asks", func(m *Model) { m.Decide(Decision{Tool: "bash", Verdict: Ask}) }},
		{"a call to fail", func(m *Model) { m.Append(Entry{Kind: Tool, ID: "toolu_fail", Head: "read", Body: "missing.go"}) }},
		{"the call fails", func(m *Model) { m.Finish("toolu_fail", Result{Status: "no such file", Failed: true}) }},
		{"streamed text", func(m *Model) { m.Stream("streamed "); m.Stream("answer\n\nwith a second paragraph") }},
		{"widened while streaming", func(m *Model) { m.SetSize(100, 36) }},
		{"the queue released", func(m *Model) { m.Release() }},
		{"stopped", func(m *Model) { m.Stop() }},
		{"the queued message withdrawn", func(m *Model) { m.Queue("withdrawn", "withdrawn", nil); m.Unqueue() }},
		{"the partial answer taken", func(m *Model) { m.Start(); m.Stream("half"); m.TakePartial() }},
	}
}

func built(steps []step) *Model {
	m := New(fixed(), new(markdown.Renderer).Lines)
	m.SetSize(120, 36)
	talked(&m)
	for _, s := range steps {
		s.do(&m)
	}
	return &m
}

func filled(m *Model) *Model {
	for m.Fill() {
	}
	return m
}

func withoutTrack(frame string) string {
	lines := strings.Split(ansi.Strip(frame), "\n")
	for index, line := range lines {
		lines[index] = ansi.Truncate(line, max(ansi.StringWidth(line)-1, 0), "")
	}
	return strings.Join(lines, "\n")
}

func TestAResizedTranscriptMatchesOneBuiltAtThatWidth(t *testing.T) {
	steps := resizeSteps()
	kept := built(nil)
	kept.View()
	for at, s := range steps {
		s.do(kept)
		fresh := filled(built(steps[:at+1]))
		want := fresh.View()
		if got := kept.View(); withoutTrack(got) != withoutTrack(want) {
			t.Fatalf("%s: the kept transcript shows a line the fresh one does not\nkept:\n%s\nfresh:\n%s", s.name, withoutTrack(got), withoutTrack(want))
		}
		filled(kept)
		if got := kept.View(); got != want {
			t.Fatalf("%s: once filled, the kept frame differs from the fresh one\nkept:\n%s\nfresh:\n%s", s.name, got, want)
		}
		if got, want := kept.Track(), fresh.Track(); got != want {
			t.Fatalf("%s: the kept track is %+v, the fresh one %+v", s.name, got, want)
		}
	}
}
