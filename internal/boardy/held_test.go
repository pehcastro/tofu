package boardy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"tofu/internal/rule"
)

func holding(t *testing.T, m Managed, key, title string, status Status, owns ...string) Ticket {
	t.Helper()
	created, err := m.Create(key, ticketWith(title, owns...), manager)
	if err != nil {
		t.Fatal(err)
	}
	created, err = m.Store.Edit(created.ID, func(ticket *Ticket) error { ticket.Status = status; return nil })
	if err != nil {
		t.Fatal(err)
	}
	return created
}

func TestHeldAnswers(t *testing.T) {
	m := managedBoard(t, "DEMO", "OPS")
	holding(t, m, "DEMO", "parked", Backlog, "docs/**")
	holding(t, m, "DEMO", "waiting", Review, "library/**")
	doing := holding(t, m, "OPS", "cli", Doing, "internal/x/**", "cmd/tofu/**")
	broken, err := m.Store.TicketPath(doing.ID)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(broken)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(broken, append(raw, "\ntext after the Log\n## Unknown heading\n"...), 0o644); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		path, ticket, glob string
	}{
		{"cmd/tofu/x.go", doing.ID, "cmd/tofu/**"},
		{"cmd/tofu", doing.ID, "cmd/tofu/**"},
		{"docs/x.md", "", ""},
		{"library/a.md", "", ""},
		{"cmd/other/x.go", "", ""},
	}
	for _, c := range cases {
		hold, err := m.Store.Held(c.path)
		if err != nil {
			t.Fatalf("%s: %v", c.path, err)
		}
		if hold.Held != (c.ticket != "") || hold.Ticket != c.ticket || hold.Glob != c.glob {
			t.Errorf("%s: want held by %q through %q, got %+v", c.path, c.ticket, c.glob, hold)
		}
	}
}

func TestCheckMoveIsTheMoveWithoutWriting(t *testing.T) {
	m := managedBoard(t, "DEMO", "OPS")
	a := holding(t, m, "OPS", "a", Doing, "cmd/tofu/**")
	b := holding(t, m, "DEMO", "b", Todo, "cmd/tofu/boardy*.go")
	path, _ := m.Store.TicketPath(b.ID)
	before, _ := os.ReadFile(path)
	events, _ := m.Store.Events("DEMO", 0)

	_, checkErr := m.Check(rule.EventMove, b.ID, Doing, manager)
	refused(t, checkErr, a.ID)
	after, _ := os.ReadFile(path)
	still, _ := m.Store.Events("DEMO", 0)
	if string(before) != string(after) || len(still) != len(events) {
		t.Fatalf("a check wrote: ticket changed %v, events %d to %d", string(before) != string(after), len(events), len(still))
	}
	_, moveErr := m.Move(b.ID, Doing, "", manager)
	if moveErr == nil || moveErr.Error() != checkErr.Error() {
		t.Fatalf("the move and its check disagree: %v against %v", moveErr, checkErr)
	}

	free := holding(t, m, "DEMO", "free", Todo, "docs/**")
	if _, err := m.Check(rule.EventMove, free.ID, Doing, manager); err != nil {
		t.Fatalf("a free ticket checks clean: %v", err)
	}
	_, err := m.Check(rule.EventMove, free.ID, Doing, agent)
	refused(t, err, "agent")
}

func TestCheckDoesNotRecordAFiredRule(t *testing.T) {
	m := managedBoard(t, "DEMO")
	boardRule(t, m, "never@1.yaml", "id: never\ndomain: general\nkind: structural\nconcern: process_discipline\nchecker: command\ncommand: go nosuchverb {ticket}\nmode: enforced\ntext: never to doing\non: move\nto: doing\n")
	ticket := holding(t, m, "DEMO", "x", Todo)
	events, _ := m.Store.Events("DEMO", 0)
	fired, err := m.Check(rule.EventMove, ticket.ID, Doing, manager)
	refused(t, err, "never")
	if len(fired) != 1 || !fired[0].Blocked {
		t.Fatalf("the check reports the blocking rule, got %+v", fired)
	}
	if still, _ := m.Store.Events("DEMO", 0); len(still) != len(events) {
		t.Fatalf("a check recorded %d events", len(still)-len(events))
	}
	_, err = m.Move(ticket.ID, Doing, "", manager)
	refused(t, err, "never")
	if moved, _ := m.Store.Events("DEMO", 0); len(moved) != len(events)+1 {
		t.Fatalf("the real move records its fired rule, got %d new", len(moved)-len(events))
	}
}

func boardSetting(t *testing.T, m Managed, key string, change func(*Board)) {
	t.Helper()
	board, err := m.Store.Board(key)
	if err != nil {
		t.Fatal(err)
	}
	change(&board)
	if err := m.Store.SaveBoard(board); err != nil {
		t.Fatal(err)
	}
}

func TestBoardRulesLogAndSource(t *testing.T) {
	m := managedBoard(t, "DEMO")
	ticket := holding(t, m, "DEMO", "x", Doing)
	if _, err := m.Comment(ticket.ID, strings.Repeat("said ", 50), manager); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Check(rule.EventMove, ticket.ID, Review, manager); err != nil {
		t.Fatalf("with no floor any Log reaches review: %v", err)
	}

	boardSetting(t, m, "DEMO", func(b *Board) {
		b.LogToReview, b.ScopeCites = 200, []string{".local/sources/", "nothing in .local/sources"}
	})
	_, err := m.Move(ticket.ID, Review, "", manager)
	refused(t, err, "200")
	if _, err := m.Evidence(ticket.ID, strings.Repeat("evidence ", 25), manager, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Check(rule.EventMove, ticket.ID, Review, manager); err != nil {
		t.Fatalf("200 characters of evidence reach review: %v", err)
	}

	_, err = m.Create("DEMO", ticketWith("no source"), manager)
	refused(t, err, "nothing in .local/sources")
	cited := ticketWith("cited")
	cited.Scope = "read .local/sources/hermes-agent first"
	if _, err := m.Create("DEMO", cited, manager); err != nil {
		t.Fatalf("a Scope naming a source creates: %v", err)
	}
	_, err = m.Widen(ticket.ID, []string{"more/**"}, manager)
	refused(t, err, "source")
}

func TestBoardFileKeepsOldShape(t *testing.T) {
	m := managedBoard(t, "DEMO")
	raw, err := os.ReadFile(filepath.Join(m.Store.BoardDir("DEMO"), BoardFileName))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "log_to_review") || strings.Contains(string(raw), "scope_cites") {
		t.Fatalf("an unset board rule is not written, so an older tofu still reads the board:\n%s", raw)
	}
	boardSetting(t, m, "DEMO", func(b *Board) { b.LogToReview, b.ScopeCites = 120, []string{"x"} })
	board, err := m.Store.Board("DEMO")
	if err != nil || board.LogToReview != 120 || len(board.ScopeCites) != 1 {
		t.Fatalf("the board rules round trip, got %d %v %v", board.LogToReview, board.ScopeCites, err)
	}
}

func TestSpecAndSizeRoundTrip(t *testing.T) {
	ticket := ticketWith("x")
	ticket.ID, ticket.Type, ticket.Status, ticket.Priority = "DEMO-1", Task, Todo, DefaultPriority
	plain, err := ParseTicket(ticket.Markdown())
	if err != nil || strings.Contains(string(ticket.Markdown()), "spec:") || strings.Contains(string(ticket.Markdown()), "size:") {
		t.Fatalf("a ticket with no spec or size writes neither, so an older tofu still reads it: %v\n%s", err, ticket.Markdown())
	}
	ticket.Spec, ticket.Size = ".local/plan.md", SizeM
	parsed, err := ParseTicket(ticket.Markdown())
	if err != nil || parsed.Spec != ticket.Spec || parsed.Size != SizeM || plain.Size != "" {
		t.Fatalf("spec and size round trip, got %q %q %v", parsed.Spec, parsed.Size, err)
	}
	if _, err := ParseTicket([]byte(strings.Replace(string(ticket.Markdown()), "size: M", "size: XXL", 1))); err == nil {
		t.Fatal("a size outside S, M, L is refused")
	}
}
