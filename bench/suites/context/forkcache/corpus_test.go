package main

import (
	"slices"
	"testing"
)

func TestReadCorpusReplaysTheFourRecordedTurns(t *testing.T) {
	scripts, err := readCorpus()
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, played := range scripts {
		names = append(names, played.name)
		if len(played.steps) == 0 || played.taskBytes == 0 {
			t.Errorf("%s replays %d steps over a task of %d bytes, so the fork has nothing to cross", played.name, len(played.steps), played.taskBytes)
		}
	}
	want := []string{"turn-18d6a7961d933ee8", "turn-18d6a27c7dfb1644", "turn-18d69bfed2bf52a0", "turn-18d6a5df2caeac68"}
	if !slices.Equal(names, want) {
		t.Fatalf("read %v, want %v", names, want)
	}
	t.Logf("%d recorded turns read from internal/dummy/turns", len(scripts))
}
