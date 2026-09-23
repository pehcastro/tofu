package llm

import (
	"strings"
	"testing"
)

func TestTheEffortVocabularyIsClosedAndTheRefusalNamesIt(t *testing.T) {
	for _, level := range Efforts() {
		parsed, err := ParseEffort(string(level))
		if err != nil || parsed != level {
			t.Fatalf("%q parsed as %q with %v", level, parsed, err)
		}
	}
	_, err := ParseEffort("ultra")
	if err == nil {
		t.Fatal("ultra is no level and it was accepted")
	}
	if !strings.Contains(err.Error(), EffortList(Efforts())) {
		t.Fatalf("the refusal does not carry the list: %v", err)
	}
}

func TestOnlyNoneAndTheUnsetLevelMeanNoThinking(t *testing.T) {
	for _, level := range Efforts() {
		if level.Thinks() != (level != EffortNone) {
			t.Fatalf("%q reports Thinks %v", level, level.Thinks())
		}
	}
	if Effort("").Thinks() {
		t.Fatal("an unset effort asks for thinking, so a caller that never set one starts paying for it")
	}
}
