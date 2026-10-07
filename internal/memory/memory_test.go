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

func TestALeadRuleIsOneLineOfAtMost160BytesNamingNoOne(t *testing.T) {
	at160 := strings.Repeat("a", 159) + "."
	cases := []struct {
		statement, rule, refusal string
	}{
		{"  Use the shared components; never build a parallel copy.  ", "Use the shared components; never build a parallel copy.", ""},
		{at160, at160, ""},
		{"  " + at160 + "\t", at160, ""},
		{at160 + "b", "", "161 bytes"},
		{strings.Repeat("€", 54), "", "162 bytes"},
		{"Use the shared components.\nAlso keep replies short.", "", "one line"},
		{"Use the shared components.\r\nAlso keep replies short.", "", "one line"},
		{"   ", "", "empty"},
		{"The user wants the shared components used.", "", "names or describes the person"},
		{"She prefers the shared components.", "", "names or describes the person"},
	}
	for _, c := range cases {
		rule, err := Rule(c.statement)
		switch {
		case c.refusal == "" && (err != nil || rule != c.rule):
			t.Errorf("Rule(%q) = %q, %v, want %q kept", c.statement, rule, err, c.rule)
		case c.refusal != "" && (err == nil || !strings.Contains(err.Error(), c.refusal)):
			t.Errorf("Rule(%q) = %q, %v, want a refusal saying %q", c.statement, rule, err, c.refusal)
		}
	}
}
