package session

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func opened(t *testing.T, store *Store, header Header, events ...Event) Header {
	t.Helper()
	log, err := store.Open(header)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range events {
		if _, err := log.Append(event, nil); err != nil {
			t.Fatal(err)
		}
	}
	written := log.Header()
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}
	return written
}

func succeeded(t *testing.T, store *Store, from Header, kind string, at time.Time, events ...Event) Header {
	t.Helper()
	next := opened(t, store, Header{ID: NewEventID(), At: at, ForkKind: kind, Root: from.ID, CarriedFrom: &Carried{Session: from.ID}}, events...)
	log, err := store.Open(Header{ID: from.ID})
	if err != nil {
		t.Fatal(err)
	}
	if err := errors.Join(log.Edit(func(header *Header) { header.ForkedInto = next.ID }), log.Close()); err != nil {
		t.Fatal(err)
	}
	return next
}

func writtenBefore(t *testing.T, store *Store, header map[string]any) {
	t.Helper()
	raw, err := json.Marshal(header)
	if err != nil {
		t.Fatal(err)
	}
	dir := store.Dir(header["id"].(string))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := errors.Join(os.WriteFile(filepath.Join(dir, headerName), raw, 0o644), os.WriteFile(filepath.Join(dir, eventsName), nil, 0o644)); err != nil {
		t.Fatal(err)
	}
}

func called(id, agent, tool, args string, at time.Time) Event {
	return Event{ID: id, Agent: agent, Turn: "turn-a", Call: id, At: at, Kind: EventToolCall, Body: json.RawMessage(`{"tool":"` + tool + `","args":` + args + `}`)}
}

type family struct {
	store             *Store
	one, two, three   Header
	before            [3]string
	sameName, another Header
	start             time.Time
}

func aFamily(t *testing.T) family {
	t.Helper()
	store := NewStore(t.TempDir())
	start := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	name := "tidy-moss-vole"
	one := opened(t, store, Header{ID: NewEventID(), Name: &name, At: start},
		called("call-vet", "", "bash", `{"command":"go vet ./internal/x"}`, start.Add(time.Minute)),
		Event{Kind: EventToolResult, Call: "call-vet", At: start.Add(2 * time.Minute), Body: json.RawMessage(`{"content":"ok","result_bytes":2}`)})
	two := succeeded(t, store, one, "continuation", start.Add(time.Hour),
		Event{Kind: EventMessage, At: start.Add(61 * time.Minute), Body: json.RawMessage(`{"role":"user","content":"find the needle in the hay"}`)},
		called("call-edit", "", "edit", `{"path":"src/a.go","old":"x","new":"y"}`, start.Add(62*time.Minute)),
		called("call-scout", "scout-f2", "bash", `{"command":"ls src"}`, start.Add(63*time.Minute)))
	three := succeeded(t, store, two, "compact", start.Add(2*time.Hour))
	before := [3]string{"amber-ashen-crane", "brisk-birch-crow", "calm-cedar-dove"}
	writtenBefore(t, store, map[string]any{"id": "turn-old", "schema": 4, "name": before[0], "started_at": start.Add(-48 * time.Hour), "root": "turn-old", "forked_into": "turn-old-f2"})
	writtenBefore(t, store, map[string]any{"id": "turn-old-f2", "schema": 4, "name": before[1], "started_at": start.Add(-47 * time.Hour), "root": "turn-old",
		"carried_from": map[string]string{"session": "turn-old"}, "fork_kind": "continuation", "forked_into": "turn-old-f3"})
	writtenBefore(t, store, map[string]any{"id": "turn-old-f3", "schema": 4, "name": before[2], "started_at": start.Add(-46 * time.Hour), "root": "turn-old-f3",
		"carried_from": map[string]string{"session": "turn-old-f2"}, "fork_kind": "continuation"})
	twin := "quiet-sage-wren"
	sameName := opened(t, store, Header{ID: NewEventID(), Name: &twin, At: start.Add(-time.Hour)})
	another := opened(t, store, Header{ID: NewEventID(), Name: &twin, At: start.Add(-2 * time.Hour)})
	return family{store: store, one: one, two: two, three: three, before: before, sameName: sameName, another: another, start: start}
}

func TestASuccessionKeepsTheFamilyNameAndTheGenerationRises(t *testing.T) {
	f := aFamily(t)
	for i, header := range []Header{f.one, f.two, f.three} {
		identity, err := f.store.Identity(header.ID)
		if err != nil {
			t.Fatal(err)
		}
		want := Identity{Session: header.ID, Family: f.one.ID, Tag: FamilyTag(f.one.ID), Name: "tidy-moss-vole", Generation: i + 1, Started: f.start}
		if identity != want {
			t.Errorf("generation %d reads as %+v, want %+v", i+1, identity, want)
		}
		if header.Named() != "tidy-moss-vole" {
			t.Errorf("generation %d was written as %q", i+1, header.Named())
		}
	}
	if handle := (Identity{Name: "tidy-moss-vole", Tag: "12cp3", Generation: 4}).Handle(); handle != "tidy-moss-vole#12cp3.4" {
		t.Errorf("the handle is %q", handle)
	}
	old, err := f.store.Identity("turn-old-f3")
	if err != nil {
		t.Fatal(err)
	}
	if old.Family != "turn-old" || old.Generation != 3 || old.Name != f.before[0] {
		t.Errorf("a fork written before this change, its root its own id, reads as %+v", old)
	}
}

func TestAHandleResolvesToOneGenerationOfOneFamily(t *testing.T) {
	f := aFamily(t)
	tag, oldTag := FamilyTag(f.one.ID), FamilyTag("turn-old")
	for _, c := range []struct {
		handle string
		want   []string
	}{
		{"tidy-moss-vole", []string{f.three.ID}},
		{"tidy-moss-vole#" + tag + ".1", []string{f.one.ID}},
		{"tidy-moss-vole#" + tag, []string{f.three.ID}},
		{"#" + tag + ".2", []string{f.two.ID}},
		{tag, []string{f.three.ID}},
		{f.before[1], []string{"turn-old-f2"}},
		{f.before[0], []string{"turn-old"}},
		{f.before[0] + "#" + oldTag + ".3", []string{"turn-old-f3"}},
		{"#" + f.two.ID[len(f.two.ID)-6:], []string{f.two.ID}},
		{f.two.ID, []string{f.two.ID}},
		{"quiet-sage-wren", []string{f.sameName.ID, f.another.ID}},
		{"tidy-moss-vole#" + tag + ".9", nil},
		{"nobody-here", nil},
	} {
		matched, err := f.store.Resolve(c.handle)
		var got []string
		for _, header := range matched {
			got = append(got, header.ID)
		}
		if c.want == nil {
			if !errors.Is(err, fs.ErrNotExist) {
				t.Errorf("%s resolved to %v, %v, want nothing", c.handle, got, err)
			}
			continue
		}
		if err != nil || len(got) != len(c.want) || got[0] != c.want[0] || got[len(got)-1] != c.want[len(c.want)-1] {
			t.Errorf("%s resolved to %v, %v, want %v", c.handle, got, err, c.want)
		}
	}
}

func TestABranchIsANewFamilyLinkedToItsPoint(t *testing.T) {
	f := aFamily(t)
	branch := opened(t, f.store, Header{ID: NewEventID(), At: f.start.Add(3 * time.Hour), BranchedFrom: &Carried{Session: f.two.ID, Event: "ev-point"}})
	identity, err := f.store.Identity(branch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if identity.Family != branch.ID || identity.Generation != 1 || identity.Name == "tidy-moss-vole" || identity.Name == "" {
		t.Errorf("a branch reads as %+v", identity)
	}
	listing, err := f.store.Listing()
	if err != nil {
		t.Fatal(err)
	}
	for _, found := range listing.Families() {
		switch found.Family {
		case f.one.ID:
			if len(found.Branches) != 1 || found.Branches[0] != branch.ID || len(found.Generations) != 3 {
				t.Errorf("the family branched from lists %d generations and branches %v", len(found.Generations), found.Branches)
			}
		case branch.ID:
			if found.BranchedFrom == nil || found.BranchedFrom.Session != f.two.ID || found.BranchedFrom.Event != "ev-point" {
				t.Errorf("the branch family carries %+v", found.BranchedFrom)
			}
		}
	}
}

func TestARenameNamesEveryGenerationAndTheNextFork(t *testing.T) {
	f := aFamily(t)
	if _, err := f.store.SetName(f.two.ID, "checkout redesign"); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{f.one.ID, f.two.ID, f.three.ID} {
		header, err := f.store.read(id)
		if err != nil || header.Named() != "checkout-redesign" {
			t.Errorf("%s is called %q after the rename, %v", id, header.Named(), err)
		}
	}
	if next := succeeded(t, f.store, f.three, "continuation", f.start.Add(4*time.Hour)); next.Named() != "checkout-redesign" {
		t.Errorf("the fork after a rename is called %q", next.Named())
	}
}

func TestFindWalksEveryGenerationOfTheFamily(t *testing.T) {
	f := aFamily(t)
	listing, err := f.store.Listing()
	if err != nil {
		t.Fatal(err)
	}
	var found Family
	for _, each := range listing.Families() {
		if each.Family == f.one.ID {
			found = each
		}
	}
	for _, c := range []struct {
		name  string
		query Query
		want  []string
	}{
		{"bash from the head", Query{Tool: "bash"}, []string{"call-vet", "call-scout"}},
		{"an agent and its forks", Query{Tool: "bash", Agent: "scout"}, []string{"call-scout"}},
		{"a command", Query{Command: "vet"}, []string{"call-vet"}},
		{"a file in any argument", Query{File: "a.go"}, []string{"call-edit"}},
		{"text said", Query{Text: "needle"}, nil},
		{"since cuts each event", Query{Tool: "bash", Since: f.start.Add(30 * time.Minute)}, []string{"call-scout"}},
		{"until cuts each event", Query{Until: f.start.Add(30 * time.Minute)}, []string{"call-vet"}},
	} {
		hits, err := f.store.Find(found, c.query)
		if err != nil {
			t.Fatal(err)
		}
		var calls []string
		for _, hit := range hits {
			if hit.Call != "" {
				calls = append(calls, hit.Call)
			}
		}
		if len(calls) != len(c.want) || (len(calls) > 0 && (calls[0] != c.want[0] || calls[len(calls)-1] != c.want[len(c.want)-1])) {
			t.Errorf("%s found %v, want %v", c.name, calls, c.want)
		}
		if c.query.Text != "" && (len(hits) != 1 || hits[0].Generation != 2 || hits[0].Role != RoleUser) {
			t.Errorf("%s found %+v", c.name, hits)
		}
		if c.query.Command != "" && (len(hits) != 1 || hits[0].Generation != 1 || hits[0].Command != "go vet ./internal/x" || hits[0].Ran == nil) {
			t.Errorf("%s found %+v", c.name, hits)
		}
	}
}
