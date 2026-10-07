package memory

import (
	"strings"
	"testing"
	"time"
)

func TestTheBlockSendsGlobalThenProjectEachWithItsIDAndNothingWhenEmpty(t *testing.T) {
	home, project := t.TempDir(), t.TempDir()
	shelves := Memory{Global: Shelf{Scope: Global, Dir: home}, Project: Shelf{Scope: Project, Dir: project}}
	if block := shelves.Block(); block != "" {
		t.Fatalf("empty memory sends %q, want nothing", block)
	}
	at := time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC)
	if _, err := shelves.Add(Entry{Scope: Project, Kind: KindProject, Text: "this project uses gpui-ce", At: at}, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := shelves.Add(Entry{Scope: Global, Kind: KindPerson, Text: "The lead decides the order", At: at.AddDate(0, 0, -1)}, ""); err != nil {
		t.Fatal(err)
	}
	read, err := Read(home, project)
	if err != nil {
		t.Fatal(err)
	}
	block := read.Block()
	global, local := strings.Index(block, "[memory#m2] 2026-10-06: The lead decides"), strings.Index(block, "[memory#m1] 2026-10-07: this project")
	if global < 0 || local < global || !strings.Contains(block, "outranks") {
		t.Fatalf("the block is %q, want the global entry, then the project entry, each with its id, and which one wins", block)
	}
}

func TestAnIDIsNeverGivenTwiceEvenAfterTheHighestIsRemoved(t *testing.T) {
	home := t.TempDir()
	first := Memory{Global: Shelf{Scope: Global, Dir: home}, Project: Shelf{Scope: Project, Dir: t.TempDir()}}
	at := time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC)
	added, err := first.Add(Entry{Scope: Project, Kind: KindProject, Text: "one", At: at}, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := first.Remove(Project, added.ID); err != nil {
		t.Fatal(err)
	}
	other := Memory{Global: Shelf{Scope: Global, Dir: home}, Project: Shelf{Scope: Project, Dir: t.TempDir()}}
	again, err := other.Add(Entry{Scope: Project, Kind: KindProject, Text: "two", At: at}, "")
	if err != nil {
		t.Fatal(err)
	}
	if again.ID == added.ID {
		t.Fatalf("%s was given again after it was removed, in another project of the same home", again.ID)
	}
}

func TestRememberIsOfferedAtTheStartOfASentenceOnly(t *testing.T) {
	cases := []struct {
		typed, text string
		offered     bool
	}{
		{"remember: never run cargo with more than 2 jobs", "never run cargo with more than 2 jobs", true},
		{"Remember that gpui and gpui-ce are different crates", "gpui and gpui-ce are different crates", true},
		{"also remember that the build is cargo build -j 2", "the build is cargo build -j 2", true},
		{"ok, and remember, you can explore alternatives", "you can explore alternatives", true},
		{"lets go fast. remember that gpus differ\nand so do drivers", "gpus differ and so do drivers", true},
		{"remembering the old flag, run it again", "", false},
		{"do you remember the cargo thing?", "", false},
		{"i remember you said that", "", false},
		{"remember", "", false},
		{"also remember:   ", "", false},
	}
	for _, c := range cases {
		offer, offered := OfferFor(c.typed)
		if offered != c.offered || offer.Text != c.text {
			t.Errorf("OfferFor(%q) = %+v, %v, want %q, %v", c.typed, offer, offered, c.text, c.offered)
		}
		if offered && (offer.Said != c.typed || offer.Scope != Global || offer.Kind != KindPerson) {
			t.Errorf("OfferFor(%q) = %+v, want his whole words kept and global for a person entry", c.typed, offer)
		}
	}
}

func TestSevenOfTheFirstTenAnswersAcceptedTrustsTheOffers(t *testing.T) {
	answer := func(home string, times int, a Answer) {
		for range times {
			if err := Record(home, a); err != nil {
				t.Fatal(err)
			}
		}
	}
	trusts := func(home string) bool {
		trusted, err := Trusts(home)
		if err != nil {
			t.Fatal(err)
		}
		return trusted
	}
	home := t.TempDir()
	answer(home, 7, AnswerGlobal)
	if trusts(home) {
		t.Fatal("7 answers trusted the offers before 10 were answered")
	}
	answer(home, 3, AnswerNo)
	if !trusts(home) {
		t.Fatal("7 of 10 accepted did not trust the offers")
	}
	home = t.TempDir()
	answer(home, 6, AnswerProject)
	answer(home, 4, AnswerNo)
	if trusts(home) {
		t.Fatal("6 of 10 accepted trusted the offers")
	}
	answer(home, 10, AnswerGlobal)
	if trusts(home) {
		t.Fatal("answers after the first 10 changed the decision")
	}
}
