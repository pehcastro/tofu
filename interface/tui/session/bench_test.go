package session

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"tofu/interface/tui/markdown"
)

const (
	benchTurns   = 19
	benchScrolls = 30
	benchSteady  = 15
)

func progressedChat() (*Model, []anchor) {
	at := time.Date(2026, 9, 25, 16, 18, 0, 0, time.UTC)
	model := New(func() time.Time { return at }, new(markdown.Renderer).Lines)
	model.SetSize(120, 36)
	model.Focus()
	for turn := range benchTurns {
		number := strconv.Itoa(turn)
		model.Append(Entry{Kind: User, Body: "question " + number + ": " + strings.Repeat("keep the draft while the answer arrives and say what you would verify ", 2)})
		model.Start()
		at = at.Add(time.Second)
		model.Returned()
		last := ""
		for call := range 3 {
			last = "toolu_" + number + "_" + strconv.Itoa(call)
			model.Append(Entry{Kind: Tool, ID: last, Head: "read", Body: "internal/turn/loop.go"})
			at = at.Add(2 * time.Second)
			model.Finish(last, Result{Status: "84 lines"})
		}
		model.Append(Entry{Kind: Assistant, ID: "msg_" + number, Body: "The narrow fix is in the loader.\n\n" +
			strings.Repeat("Returning an empty config for every error would also hide malformed JSON and permission failures. ", 3) +
			"\n\n- missing file returns the default\n- malformed `config.json` still fails\n- callers keep their behaviour"})
		model.Close("cooked for", last)
		model.Stop()
	}
	model.Start()
	model.Append(Entry{Kind: Tool, ID: "toolu_live", Head: "bash", Body: "go test ./internal/turn/..."})
	tail, _ := model.tailAnchor(model.transcriptRows())
	anchors := make([]anchor, benchScrolls)
	for step := range anchors {
		anchors[step] = model.move(tail, -step)
	}
	model.View()
	return &model, anchors
}

func BenchmarkProgressedScreenRender(b *testing.B) {
	b.Run("chat", func(b *testing.B) {
		model, anchors := progressedChat()
		b.ReportAllocs()
		b.ResetTimer()
		for index := range b.N {
			model.top, model.following = anchors[index%benchScrolls], index%benchScrolls == 0
			_ = model.View()
		}
	})
}

func BenchmarkSteadyScreenRender(b *testing.B) {
	b.Run("chat", func(b *testing.B) {
		model, anchors := progressedChat()
		model.top, model.following = anchors[benchSteady], false
		_ = model.View()
		b.ReportAllocs()
		b.ResetTimer()
		for range b.N {
			_ = model.View()
		}
	})
}

func BenchmarkWheelEventFrame(b *testing.B) {
	b.Run("chat", func(b *testing.B) {
		model, anchors := progressedChat()
		b.ReportAllocs()
		b.ResetTimer()
		for index := range b.N {
			model.top, model.following = anchors[index%benchScrolls], index%benchScrolls == 0
			model.Scroll(WheelUp)
			_ = model.View()
		}
	})
}

func BenchmarkResizeFrame(b *testing.B) {
	b.Run("chat", func(b *testing.B) {
		model, _ := progressedChat()
		b.ReportAllocs()
		for at := 1; b.Loop(); at++ {
			model.SetSize(120-at%40, 36)
			_ = model.View()
		}
	})
}

func BenchmarkChatScrollbarMetrics(b *testing.B) {
	model, anchors := progressedChat()
	model.top, model.following = anchors[benchSteady], false
	rows := model.transcriptRows()
	_ = model.View()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		_, _, _ = model.scrollMetrics(rows)
	}
}
