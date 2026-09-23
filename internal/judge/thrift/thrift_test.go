package thrift

import "testing"

func TestBuildStateNamesTheOpeningMiddleAndClosingParagraph(t *testing.T) {
	paragraphs := []string{"a", "b", "c"}
	positions := []string{"opening", "middle", "closing"}
	for i, want := range positions {
		state := BuildState(paragraphs[i], i, len(paragraphs), "read", "read file.go", "find the bug")
		if state.Position != want {
			t.Fatalf("paragraph %d: position %q, want %q", i, state.Position, want)
		}
		if state.Paragraph != paragraphs[i] || state.Tool != "read" || state.Command != "read file.go" || state.Task != "find the bug" {
			t.Fatalf("paragraph %d: state %+v does not carry what it was built from", i, state)
		}
	}
}
