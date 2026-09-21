package work

import (
	"strings"
	"testing"
)

func TestAnEmptyWorkViewSaysSoRatherThanDrawingAnEmptyFrame(t *testing.T) {
	m := New()
	m.SetSize(60, 10)
	view := m.View()
	if want := "nothing has run yet"; !strings.Contains(view, want) {
		t.Fatalf("the empty view does not say %q\n%s", want, view)
	}
}

func TestAppendedCallsDrawWhole(t *testing.T) {
	m := New()
	m.SetSize(80, 20)
	m.Append(Entry{ID: "call-1", Head: "read a.go", Args: `{"path":"a.go"}`})
	m.Finish("call-1", "12 lines", 240, false)
	view := m.View()
	for _, want := range []string{"read a.go", `{"path":"a.go"}`, "12 lines", "240 bytes", "#call-1"} {
		if !strings.Contains(view, want) {
			t.Errorf("the call did not draw whole, missing %q\n%s", want, view)
		}
	}
}

func TestFinishOnAFailedCallUsesTheFailStyle(t *testing.T) {
	m := New()
	m.SetSize(80, 20)
	m.Append(Entry{ID: "call-1", Head: "bash go test"})
	m.Finish("call-1", "FAIL", 4, true)
	entry, ok := m.Picked()
	if !ok || !entry.Failed {
		t.Fatalf("Picked() = %+v, %v, want a failed entry", entry, ok)
	}
}

func TestDecideAttachesTheVerdictToTheMatchingUndecidedCall(t *testing.T) {
	m := New()
	m.SetSize(80, 20)
	m.Append(Entry{ID: "call-1", Head: "bash go test"})
	m.Decide("bash", "ask")
	entry, ok := m.Picked()
	if !ok || entry.Verdict != "ask" {
		t.Fatalf("Picked() = %+v, %v, want the verdict ask", entry, ok)
	}
}

func TestJumpToFindsAnEntryByIDPrefix(t *testing.T) {
	m := New()
	m.Append(Entry{ID: "aaaa111"})
	m.Append(Entry{ID: "bbbb222"})
	if !m.JumpTo("bbbb") {
		t.Fatal("JumpTo did not find the matching entry")
	}
	entry, _ := m.Picked()
	if entry.ID != "bbbb222" {
		t.Fatalf("Picked() after JumpTo = %+v, want bbbb222", entry)
	}
	if m.JumpTo("zzzz") {
		t.Fatal("JumpTo found an entry that does not exist")
	}
}

func TestReferenceTruncatesALongOutputAndCarriesTheLabel(t *testing.T) {
	long := ""
	for range 400 {
		long += "x"
	}
	e := Entry{ID: "call-1", Head: "read big.txt", Output: long}
	ref := e.Reference()
	if len(ref) >= len(long) {
		t.Fatalf("Reference() did not truncate: %d runes", len(ref))
	}
	if !strings.Contains(ref, "read big.txt") {
		t.Fatalf("Reference() lost the label: %q", ref)
	}
}

func TestKeyMovesThePick(t *testing.T) {
	m := New()
	m.Append(Entry{ID: "a"})
	m.Append(Entry{ID: "b"})
	m.Key("down")
	entry, _ := m.Picked()
	if entry.ID != "b" {
		t.Fatalf("Key(down) picked %+v, want b", entry)
	}
	m.Key("up")
	entry, _ = m.Picked()
	if entry.ID != "a" {
		t.Fatalf("Key(up) picked %+v, want a", entry)
	}
}
