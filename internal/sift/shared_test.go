package sift

import (
	"testing"

	"tofu/internal/judge/thrift"
	"tofu/internal/judge/thrift/unit"
)

func TestBothBuildersNameTheSamePositions(t *testing.T) {
	parts := []Part{{Text: "a"}, {Text: "b"}, {Text: "c"}}
	for i, want := range []string{"opening", "middle", "closing"} {
		sifted := BuildState(parts, i, "find the bug").Position
		thrifted := thrift.BuildState(parts[i].Text, i, len(parts), "read", "read file.go", "find the bug").Position
		if sifted != want || thrifted != want {
			t.Fatalf("unit %d: sift says %q and thrift says %q, want %q from both", i, sifted, thrifted, want)
		}
	}
}

func TestBothPackagesAskOneQuestionUnderOneName(t *testing.T) {
	if NeededQuestion != unit.NeededQuestion || thrift.NeededQuestion != unit.NeededQuestion {
		t.Fatalf("sift asks %q and thrift asks %q, want %q from both", NeededQuestion, thrift.NeededQuestion, unit.NeededQuestion)
	}
}

func TestAThriftDecisionRendersThroughSiftWithNoConversion(t *testing.T) {
	kept, err := thrift.Decide(0, 0.9, true, 0.5)
	if err != nil {
		t.Fatalf("thrift.Decide: %v", err)
	}
	dropped, err := thrift.Decide(1, 0.1, true, 0.5)
	if err != nil {
		t.Fatalf("thrift.Decide: %v", err)
	}
	parts := []Part{{Text: "kept", Sep: "\n\n"}, {Text: "dropped", Sep: "\n"}}
	rendered := Render(parts, []Mark{kept, dropped})
	restored, err := Restore(rendered)
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if restored != Join(parts) {
		t.Fatalf("a thrift judgment rendered by sift did not restore: %q, want %q", restored, Join(parts))
	}
}
