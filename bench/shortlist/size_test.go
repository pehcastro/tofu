package shortlist

import "testing"

func TestNeededQuestionsOnTheObservedGap(t *testing.T) {
	got := NeededQuestions(4, 4, 1, 4)
	if got != 12 {
		t.Fatalf("NeededQuestions(4,4,1,4) = %d, want 12", got)
	}
}

func TestNeededQuestionsGrowsAsTheGapShrinks(t *testing.T) {
	wide := NeededQuestions(4, 4, 1, 4)
	narrow := NeededQuestions(3, 4, 2, 4)
	if narrow <= wide {
		t.Fatalf("a smaller observed gap needs at least as many questions: wide=%d narrow=%d", wide, narrow)
	}
}
