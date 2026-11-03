package edits

import "testing"

const (
	screenWidth  = 120
	screenBody   = 33
	largeWidth   = 168
	largeBody    = 39
	largeHeight  = 42
	createdID    = "call-b7c4d1"
	deletionID   = "call-44f0aa"
	progressRows = 30
)

func BenchmarkProgressedScreenRender(b *testing.B) {
	b.Run("file_edits", func(b *testing.B) {
		m := reading(session(b, screenWidth, screenBody), createdID)
		b.ReportAllocs()
		for at := 0; b.Loop(); at++ {
			m.scroll = at % progressRows
			_ = m.View()
		}
	})
}

func BenchmarkSteadyScreenRender(b *testing.B) {
	b.Run("file_edits", func(b *testing.B) {
		m := reading(session(b, screenWidth, screenBody), createdID)
		m.scroll = 15
		_ = m.View()
		b.ReportAllocs()
		for b.Loop() {
			_ = m.View()
		}
	})
}

func BenchmarkWheelEventFrame(b *testing.B) {
	b.Run("file_edits", func(b *testing.B) {
		m := reading(session(b, screenWidth, screenBody), createdID)
		b.ReportAllocs()
		for at := 0; b.Loop(); at++ {
			m.scroll = at % progressRows
			m.Wheel(true)
			_ = m.View()
		}
	})
}

func BenchmarkLargeFileDiffRender(b *testing.B) {
	m := reading(session(b, largeWidth, largeBody), createdID)
	b.ReportAllocs()
	for at := 0; b.Loop(); at++ {
		m.scroll = at % 1000
		_ = m.View()
	}
}

func BenchmarkLargeFileDiffSteadyFrame(b *testing.B) {
	m := reading(session(b, largeWidth, largeBody), createdID)
	_ = m.View()
	b.ReportAllocs()
	for b.Loop() {
		_ = m.View()
	}
}

func BenchmarkLargeFileDiffFirstOpen(b *testing.B) {
	m := reading(session(b, largeWidth, largeBody), createdID)
	b.ReportAllocs()
	for b.Loop() {
		m.cache.rows = diffRowIndex{}
		_ = m.View()
	}
}

func BenchmarkLargeDiffDialogScroll(b *testing.B) {
	m := session(b, largeWidth, largeBody)
	b.ReportAllocs()
	for at := 0; b.Loop(); at++ {
		_ = m.Panel(largeWidth, largeHeight, deletionID, at%300)
	}
}
