package session

import (
	"strings"
	"testing"
)

const generatedSample = 100

func TestASessionWrittenNowIsGivenANameAndKeepsItAcrossLaterWrites(t *testing.T) {
	store := NewStore(t.TempDir())
	header := Header{ID: "a", Root: "a", Task: "a task"}
	if err := store.Write(header, []Event{stepEvent(t, 1)}); err != nil {
		t.Fatalf("write a: %v", err)
	}

	first, err := store.Header("a")
	if err != nil {
		t.Fatalf("header of a: %v", err)
	}
	if first.Name == nil {
		t.Fatalf("a session written now has no name, and a person has only the id %s to type", first.ID)
	}
	t.Logf("the generated name is %q", *first.Name)

	if err := store.Write(header, nil); err != nil {
		t.Fatalf("write a again: %v", err)
	}
	again, err := store.Header("a")
	if err != nil {
		t.Fatalf("header of a after the second write: %v", err)
	}
	if again.Name == nil || *again.Name != *first.Name {
		t.Fatalf("the second write left the name %v, want the first name %q", again.Name, *first.Name)
	}
}

func TestASessionFromBeforeTheNameReadsAsAnAbsenceRatherThanAnEmptyName(t *testing.T) {
	store := NewStore("testdata")
	header, err := store.Header(schemaZeroID)
	if err != nil {
		t.Fatalf("header of %s: %v", schemaZeroID, err)
	}
	if header.Name != nil {
		t.Fatalf("a record written before the name reads as %q, want an absence", *header.Name)
	}
}

func TestEveryGeneratedNameIsTypeableWithNoCharacterAShellWouldQuote(t *testing.T) {
	seen := map[string]int{}
	for range generatedSample {
		name := newName()
		seen[name]++
		if name == "" {
			t.Fatal("a generated name is empty")
		}
		for _, letter := range name {
			typeable := letter >= 'a' && letter <= 'z' || letter >= '0' && letter <= '9' || string(letter) == nameSeparator
			if !typeable {
				t.Fatalf("the name %q carries %q, which a shell would need quoted", name, string(letter))
			}
		}
		if strings.Count(name, nameSeparator) != 2 {
			t.Fatalf("the name %q is not the three words a person can say back", name)
		}
	}
	t.Logf("%d names, %d of them distinct", generatedSample, len(seen))
}

func TestARenameIsSluggedIntoSomethingTypeable(t *testing.T) {
	for _, one := range []struct{ given, want string }{
		{"The Gate Work", "the-gate-work"},
		{"  spaced  out  ", "spaced-out"},
		{"rm -rf /; echo $HOME", "rm-rf-echo-home"},
		{"already-a-slug", "already-a-slug"},
	} {
		got, err := slugOf(one.given)
		if err != nil {
			t.Fatalf("slug of %q: %v", one.given, err)
		}
		if got != one.want {
			t.Errorf("slug of %q is %q, want %q", one.given, got, one.want)
		}
	}
	if _, err := slugOf("!!!"); err == nil {
		t.Error("a name with no letter and no digit was accepted, and there is nothing left to type")
	}
}
