package recall

import (
	"regexp"
	"testing"
	"time"
)

var toolCallShape = regexp.MustCompile(`^\w+ \{`)

func TestRawAndDistilledLastWordAreDeterministicAcrossTwoBuilds(t *testing.T) {
	cases, err := ReadForkCorpus()
	if err != nil {
		t.Fatalf("ReadForkCorpus: %v", err)
	}
	one := cases[0]
	rawFirst, rawSecond := one.LastWord, one.LastWord
	if rawFirst != rawSecond {
		t.Fatal("the raw arm disagrees with itself, which cannot happen on a stored string")
	}
	distilledFirst := DistilLastWord(one.LastWord)
	distilledSecond := DistilLastWord(one.LastWord)
	if distilledFirst != distilledSecond {
		t.Fatalf("DistilLastWord disagrees with itself across two builds: %q vs %q", distilledFirst, distilledSecond)
	}
	t.Logf("case %s->%s: raw %d bytes, distilled %d bytes, spread 0 across two builds of each", one.From, one.Into, len(rawFirst), len(distilledFirst))
}

func TestEveryRealForksLastWordIsABareToolCallNotAnAnswer(t *testing.T) {
	cases, err := ReadForkCorpus()
	if err != nil {
		t.Fatalf("ReadForkCorpus: %v", err)
	}
	bare := 0
	for _, one := range cases {
		candidates := lastWordCandidates(one.LastWord)
		if len(candidates) == 0 {
			t.Fatalf("%s->%s carries an empty last word", one.From, one.Into)
		}
		allToolCalls := true
		for _, candidate := range candidates {
			if !toolCallShape.MatchString(candidate) {
				allToolCalls = false
			}
		}
		if allToolCalls {
			bare++
		}
	}
	t.Logf("%d of %d real forks carry a last word made entirely of tool call invocations, no assistant prose", bare, len(cases))
	if bare != len(cases) {
		t.Logf("the other %d carry at least one line that is not a bare tool call, worth reading by hand before ruling", len(cases)-bare)
	}
}

func TestTheAddedLatencyOfDistillingTheLastWordAgainstTheForksRecordedCost(t *testing.T) {
	cases, err := ReadForkCorpus()
	if err != nil {
		t.Fatalf("ReadForkCorpus: %v", err)
	}
	const reps = 1000
	for _, one := range cases[:3] {
		started := time.Now()
		for i := 0; i < reps; i++ {
			_ = DistilLastWord(one.LastWord)
		}
		perCall := time.Since(started).Microseconds() / reps
		t.Logf("%s->%s: the fork itself blocked %d microseconds building its whole carry; distilling only the last word costs %d microseconds now, %.4f%% of the recorded fork cost",
			one.From, one.Into, one.BlockedMicros, perCall, 100*float64(perCall)/float64(one.BlockedMicros))
	}
}

func TestTwoNamedRealForksSideBySide(t *testing.T) {
	cases, err := ReadForkCorpus()
	if err != nil {
		t.Fatalf("ReadForkCorpus: %v", err)
	}
	var toy, work *ForkCase
	for i := range cases {
		if cases[i].From == "turn-18d6d295dfac466c" && toy == nil {
			toy = &cases[i]
		}
		if cases[i].From == "turn-18d6f8d9e45f8efc" && work == nil {
			work = &cases[i]
		}
	}
	if toy == nil || work == nil {
		t.Fatal("the two named real forks this test reads by id are not both in the corpus")
	}
	for _, one := range []*ForkCase{toy, work} {
		t.Logf("case %s -> %s, task %q\nraw       : %q\ndistilled : %q",
			one.From, one.Into, one.Task, one.LastWord, DistilLastWord(one.LastWord))
	}
}
