package session

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"tofu/interface/tui/frametime"
	"tofu/interface/tui/markdown"
)

func longStreamedAnswer() []string {
	var deltas []string
	push := func(text string) {
		for len(text) > 0 {
			cut := min(24, len(text))
			deltas = append(deltas, text[:cut])
			text = text[cut:]
		}
	}
	push("# The turn is a loop\n\n")
	for paragraph := range 30 {
		push(strings.Repeat("word ", 40) + "paragraph " + string(rune('a'+paragraph%26)) + ".\n\n")
	}
	push("```go\n")
	for line := range 80 {
		push("line " + string(rune('a'+line%26)) + " := compute(x)\n")
	}
	push("```\n\n")
	push("closing paragraph after the fence.\n")
	return deltas
}

func TestFrameBudgetWithMarkdownOnEveryDelta(t *testing.T) {
	deltas := longStreamedAnswer()
	frametime.Samples(t, "markdown-on-every-delta, "+strconv.Itoa(len(deltas))+" deltas", func() []time.Duration {
		renderer := new(markdown.Renderer)
		model := New(fixed(), renderer.Lines)
		model.SetSize(80, 24)
		taken := make([]time.Duration, 0, len(deltas))
		for _, delta := range deltas {
			model.Stream(delta)
			start := time.Now()
			model.View()
			taken = append(taken, time.Since(start))
		}
		return taken
	})
}

func TestOneFlushCostAtTheScreenshotWidth(t *testing.T) {
	deltas := longStreamedAnswer()
	renderer := new(markdown.Renderer)
	model := New(fixed(), renderer.Lines)
	model.SetSize(80, 24)
	for _, delta := range deltas[:len(deltas)-1] {
		model.Stream(delta)
	}
	last := deltas[len(deltas)-1]
	start := time.Now()
	model.Stream(last)
	model.View()
	took := time.Since(start)
	entry := model.entries[len(model.entries)-1]
	total := len(entry.Body)
	tail := total - entry.stable
	t.Logf("one flush (stream + view) at 80 columns over a %d line, %d byte answer: %v, the tail rendered %d of %d bytes (%.1f%%)",
		strings.Count(entry.Body, "\n"), total, took, tail, total, 100*float64(tail)/float64(total))
}

func TestBoundaryScanCostAloneAcrossManyDeltas(t *testing.T) {
	deltas := longStreamedAnswer()
	body := ""
	start := time.Now()
	for _, delta := range deltas {
		body += delta
		markdown.Boundary(body)
	}
	t.Logf("boundary scan alone: %d deltas over %d bytes, total %v", len(deltas), len(body), time.Since(start))
}
