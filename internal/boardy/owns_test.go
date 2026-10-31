package boardy

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func ticketIn(t *testing.T, m Managed, status Status, owns ...string) Ticket {
	t.Helper()
	ticket := ticketWith(string(status), owns...)
	ticket.Status, ticket.Log = status, "- evidence"
	created, err := m.Local.Create("DEMO", ticket, "test")
	if err != nil {
		t.Fatal(err)
	}
	return created
}

func overlaps(t *testing.T, m Managed) []string {
	t.Helper()
	boards, err := m.Store.Boards()
	if err != nil {
		t.Fatal(err)
	}
	var found []string
	for _, finding := range m.Store.Lint(boards) {
		if strings.Contains(finding.What, "owns") {
			found = append(found, finding.What)
		}
	}
	return found
}

func TestOwnsHeldOnlyWhileDoing(t *testing.T) {
	m := managedBoard(t, "DEMO")
	first := ticketIn(t, m, Review, "x/**")
	second := ticketIn(t, m, Review, "x/**")
	if found := overlaps(t, m); len(found) != 0 {
		t.Fatalf("two review tickets owning x/** lint %q, want clean", found)
	}
	if _, err := m.Move(first.ID, Doing, "", manager); err != nil {
		t.Fatalf("the first into doing: %v", err)
	}
	_, err := m.Move(second.ID, Doing, "", manager)
	refused(t, err, first.ID)

	holder := ticketIn(t, m, Doing, "y/**")
	if _, err := m.Widen(second.ID, []string{"y/a.go"}, manager); err != nil {
		t.Errorf("widening a review ticket over a doing ticket's owns: %v", err)
	}
	_, err = m.Widen(first.ID, []string{"y/a.go"}, manager)
	refused(t, err, holder.ID)

	doing := ticketWith("doing", "x/a.go")
	doing.Status = Doing
	_, err = m.Create("DEMO", doing, manager)
	refused(t, err, first.ID)
	backlog := ticketWith("backlog", "x/a.go")
	if _, err := m.Create("DEMO", backlog, manager); err != nil {
		t.Errorf("a backlog ticket owning a doing ticket's path: %v", err)
	}
	if found := overlaps(t, m); len(found) != 0 {
		t.Errorf("one doing ticket per path lints %q, want clean", found)
	}
}

func TestOwnsAbsoluteOutsideTheProject(t *testing.T) {
	m := managedBoard(t, "DEMO")
	whole := ticketIn(t, m, Doing, "F:/localhost/boji-convert/**")
	ticketIn(t, m, Doing, "f/localhost/boji-convert/**", "/home/someone/repo/**")
	if found := overlaps(t, m); len(found) != 0 {
		t.Fatalf("absolute owns lint %q, want clean", found)
	}
	inside := ticketIn(t, m, Review, `F:\localhost\boji-convert\main.go`)
	_, err := m.Move(inside.ID, Doing, "", manager)
	refused(t, err, whole.ID)

	folded := ticketIn(t, m, Review, "f:/LOCALHOST/Boji-Convert/write.go")
	_, err = m.Move(folded.ID, Doing, "", manager)
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		refused(t, err, whole.ID)
	} else if err != nil {
		t.Errorf("a case-different path on %s: %v", runtime.GOOS, err)
	}
}

func TestOwnsHoldingStatusesFromBoardFile(t *testing.T) {
	m := managedBoard(t, "DEMO")
	path := filepath.Join(m.Store.BoardDir("DEMO"), BoardFileName)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `holds = ["doing"]`) {
		t.Errorf("a new board.toml does not name its holding statuses:\n%s", raw)
	}
	var older []string
	for line := range strings.Lines(string(raw)) {
		if !strings.HasPrefix(line, "holds") {
			older = append(older, line)
		}
	}
	if err := os.WriteFile(path, []byte(strings.Join(older, "")), 0o644); err != nil {
		t.Fatal(err)
	}
	if board, err := m.Store.Board("DEMO"); err != nil || !board.Holding(Doing) || board.Holding(Review) {
		t.Fatalf("a board.toml with no holds line: holds %v, err %v; want doing only", board.Holds, err)
	}
	ticketIn(t, m, Review, "z/**")
	ticketIn(t, m, Review, "z/**")
	if found := overlaps(t, m); len(found) != 0 {
		t.Fatalf("review tickets on a doing-only board lint %q", found)
	}
	if err := os.WriteFile(path, append([]byte(strings.Join(older, "")), `holds = ["doing", "review"]`+"\n"...), 0o644); err != nil {
		t.Fatal(err)
	}
	if found := overlaps(t, m); len(found) != 1 {
		t.Errorf("two review tickets on a board holding review lint %q, want one overlap", found)
	}
	if err := os.WriteFile(path, append([]byte(strings.Join(older, "")), `holds = ["doing", "reviewed"]`+"\n"...), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Store.Board("DEMO"); err == nil || !strings.Contains(err.Error(), "reviewed") {
		t.Errorf("an unknown holding status: %v, want an error naming it", err)
	}
}

func TestSubTicketsLiveUnderTheirParentsOwns(t *testing.T) {
	m := managedBoard(t, "DEMO", "OPS")
	parent := ticketIn(t, m, Doing, "internal/a/**", "cmd/a.go")
	sub := func(parent string, owns ...string) (Ticket, error) {
		child := ticketWith("child", owns...)
		child.Parent = parent
		return m.Create("DEMO", child, manager)
	}
	first, err := sub(parent.ID, "internal/a/one.go")
	if err != nil || first.ID != parent.ID+"-1" || first.Parent != parent.ID || first.Type != Subtask {
		t.Fatalf("the first child of %s: %s parent %q type %s, %v", parent.ID, first.ID, first.Parent, first.Type, err)
	}
	second, err := sub(parent.ID, "internal/a/two/**", "cmd/a.go")
	if err != nil || second.ID != parent.ID+"-2" {
		t.Fatalf("the second child: %s, %v", second.ID, err)
	}
	for _, outside := range [][]string{{"internal/b.go"}, {"internal/**"}, {"internal/a/../b.go"}, {"cmd/*.go"}} {
		_, err = sub(parent.ID, outside...)
		refused(t, err, parent.ID)
	}
	_, err = sub(first.ID, "internal/a/one.go")
	if err == nil || !strings.Contains(err.Error(), first.ID) {
		t.Errorf("a child of a child: %v, want a refusal naming %s", err, first.ID)
	}
	_, err = sub("OPS-9", "internal/a/one.go")
	if err == nil {
		t.Error("a child of a ticket on another board was made")
	}
	if counter, _ := m.Store.next("DEMO", ""); counter != "DEMO-2" {
		t.Errorf("children took the board's numbers: the next top-level id is %s, want DEMO-2", counter)
	}
	top, err := os.ReadDir(filepath.Join(m.Store.BoardDir("DEMO"), TicketsDirName))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range top {
		if !entry.IsDir() && strings.Count(entry.Name(), "-") > 1 {
			t.Errorf("%s sits beside the top-level tickets, where tofu 0.5.12 reads it and fails every board read", entry.Name())
		}
	}

	if _, err := m.Move(first.ID, Doing, "", manager); err != nil {
		t.Errorf("a child into doing under its parent in doing: %v", err)
	}
	if _, err := m.Move(second.ID, Doing, "", manager); err != nil {
		t.Errorf("a second child with disjoint owns into doing: %v", err)
	}
	overlapping, err := sub(parent.ID, "internal/a/one.go")
	if err != nil {
		t.Fatal(err)
	}
	_, err = m.Move(overlapping.ID, Doing, "", manager)
	refused(t, err, first.ID)
	if found := overlaps(t, m); len(found) != 0 {
		t.Errorf("a parent and its children in doing lint %q, want clean", found)
	}
	if hold, err := m.Store.Held("internal/a/two/x.go"); err != nil || !hold.Held {
		t.Errorf("a path under a child in doing: %+v, %v", hold, err)
	}
	if _, err := m.Move(parent.ID, Review, "", manager); err == nil || !strings.Contains(err.Error(), first.ID) {
		t.Errorf("the parent to review with live children: %v, want a refusal naming %s", err, first.ID)
	}
	for _, child := range []string{first.ID, second.ID, overlapping.ID} {
		proved(t, m, child)
		if _, err := m.Move(child, Done, "", manager); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := m.Move(parent.ID, Review, "", manager); err != nil {
		t.Errorf("the parent to review once every child is done: %v", err)
	}
	path, _ := m.Store.TicketPath(first.ID)
	if _, err := m.Store.Edit(first.ID, func(t *Ticket) error { t.Owns = []string{"elsewhere/**"}; return nil }); err != nil {
		t.Fatal(err)
	}
	found := m.Store.Lint([]Board{{Key: "DEMO", Dir: m.Store.BoardDir("DEMO")}})
	if len(found) != 1 || found[0].Ticket != first.ID || found[0].File != path || !strings.Contains(found[0].What, parent.ID) {
		t.Errorf("a child owning outside its parent lints %+v, want one finding on %s naming %s", found, first.ID, parent.ID)
	}
}

func TestMoveToDoingOwnsWhatItNames(t *testing.T) {
	m := managedBoard(t, "DEMO")
	m.Store.Root = t.TempDir()
	for name, body := range map[string]string{
		".gitignore":                 "target/\n",
		"go.mod":                     "module demo\n",
		"lib/models/load.go":         "package models\n\nfunc Layers() int { return 1 }\n",
		"ui/pick.go":                 "package ui\n\nimport library \"demo/lib/models\"\n\nvar picked = library.Layers()\n",
		"web/web.go":                 "package web\n\nfunc Layers() int { return sys.Layers() }\n",
		"cmd/main.go":                "package main\n\nimport (\n\t\"demo/lib/models\"\n\t\"demo/sys\"\n)\n\nvar layers, other = sys.Layers(), models.Other\n",
		"core/src/model/session.rs":  "pub enum PersonKind {\n    YesNo,\n}\n",
		"core/src/model/mod.rs":      "pub mod session;\n",
		"desk/chat.rs":               "// PersonKind::YesNo\nfn ask(kind: PersonKind) {}\n",
		"desk/feed.rs":               "fn f(k: PersonKind) -> u8 {\n    match k {\n        PersonKind::YesNo => 1,\n    }\n}\n",
		"desk/symbols.rs":            "enum PersonKind {\n    Name,\n}\nfn s() { PersonKind::Name; }\n",
		"vendor/gpui/src/person.rs":  "fn g() { PersonKind::YesNo; }\n",
		"desk/target/debug/build.rs": "fn h() { PersonKind::YesNo; }\n",
	} {
		full := filepath.Join(m.Store.Root, name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	gitIn(t, m.Store.Root, "init", "-q")
	ticket := func(scope string, owns ...string) Ticket {
		made := ticketWith(scope, owns...)
		made.Scope, made.Status = scope, Review
		created, err := m.Local.Create("DEMO", made, "test")
		if err != nil {
			t.Fatal(err)
		}
		return created
	}

	rust := ticket("add a variant to PersonKind", "core/src/model/session.rs", filepath.Join(m.Store.Root, "core/src/model/fresh.rs"))
	_, err := m.Move(rust.ID, Doing, "", manager)
	refused(t, err, "desk/feed.rs:3: PersonKind::YesNo")
	refused(t, err, "core/src/model/mod.rs:1")
	for _, quiet := range []string{"desk/chat.rs", "desk/symbols.rs", "vendor/", "target/"} {
		if strings.Contains(err.Error(), quiet) {
			t.Errorf("the refusal lists %s:\n%v", quiet, err)
		}
	}
	moved, err := m.Move(rust.ID, Doing, notOwnedOnPurpose, manager)
	if err != nil || !strings.Contains(moved.Reason, "desk/feed.rs:3") || !strings.Contains(moved.Reason, notOwnedOnPurpose) {
		t.Errorf("a move %s: reason %q, %v", notOwnedOnPurpose, moved.Reason, err)
	}

	goTicket := ticket("rename models.Layers", "lib/models/**")
	_, err = m.Move(goTicket.ID, Doing, "", manager)
	refused(t, err, "ui/pick.go:5")
	for _, quiet := range []string{"web/web.go", "cmd/main.go"} {
		if strings.Contains(err.Error(), quiet) {
			t.Errorf("a Layers of another package is listed in %s:\n%v", quiet, err)
		}
	}
	if _, err := m.Store.Edit(goTicket.ID, func(t *Ticket) error { t.Owns = append(t.Owns, "ui/pick.go"); return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Move(goTicket.ID, Doing, "", manager); err != nil {
		t.Errorf("the ticket owning its caller: %v", err)
	}
}

func TestThreadKeepsATicketsEventsInOrderAndNoChildsEvents(t *testing.T) {
	m := managedBoard(t, "DEMO")
	parent := ticketIn(t, m, Todo, "x/**")
	child := ticketWith("child", "x/a.go")
	child.Parent = parent.ID
	if _, err := m.Create("DEMO", child, manager); err != nil {
		t.Fatal(err)
	}
	for _, event := range []Event{
		{Ticket: parent.ID, Kind: Spawned, Session: "s1", Agent: "go-dev-1", Call: "call_1", Text: "the mission"},
		{Ticket: parent.ID + "-1", Kind: Commented, Text: "the child's line"},
		{Ticket: parent.ID, Kind: Asked, Actor: "s1/" + parent.ID, Text: "which file"},
		{Ticket: parent.ID, Kind: Reported, Agent: "go-dev-1", Files: []string{"x/b.go"}, Text: "done"},
	} {
		if err := m.Store.RecordTicket(event); err != nil {
			t.Fatal(err)
		}
	}
	thread, err := m.Store.Thread(parent.ID)
	if err != nil {
		t.Fatal(err)
	}
	var kinds []string
	for _, event := range thread {
		kinds = append(kinds, string(event.Kind))
	}
	if strings.Join(kinds, " ") != "created spawned asked reported" || thread[3].Files[0] != "x/b.go" || thread[1].Call != "call_1" {
		t.Errorf("the thread of %s reads %v, want created spawned asked reported with the call and the files", parent.ID, kinds)
	}
}
