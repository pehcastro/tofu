package subagent

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func boji015Fixture(t *testing.T) (*Boundary, string, []byte) {
	t.Helper()
	filed, err := os.ReadFile(filepath.Join("testdata", "counted", "BOJI-015.md"))
	if err != nil {
		t.Fatal(err)
	}
	ticket := filepath.Join(t.TempDir(), "BOJI-015.md")
	if err := os.WriteFile(ticket, filed, 0o600); err != nil {
		t.Fatal(err)
	}
	return &Boundary{Ticket: "BOJI-015", Owns: frontMatterOwns(filed)}, ticket, filed
}

func TestAWriteOutsideOwnsIsRefusedAndAsksForExactlyOneGrant(t *testing.T) {
	boundary := &Boundary{Ticket: "BOJI-210", Owns: []string{"internal/subagent/**"}}
	if err := boundary.Write("internal/subagent/grant.go"); err != nil {
		t.Fatalf("a write inside owns was refused: %v", err)
	}
	if len(boundary.Asked()) != 0 {
		t.Fatalf("a write inside owns asked for %d grants, want 0", len(boundary.Asked()))
	}

	var denied DeniedError
	if !errors.As(boundary.Write("internal/turn/loop.go"), &denied) {
		t.Fatal("a write outside owns was not refused")
	}
	if err := boundary.Write("internal/turn/loop.go"); err == nil {
		t.Fatal("the second write to the same path was allowed")
	}
	asked := boundary.Asked()
	if len(asked) != 1 {
		t.Fatalf("two refused writes to one path asked for %d grants, want 1", len(asked))
	}
	want := Question{
		Ticket:  "BOJI-210",
		Kind:    Grant,
		Where:   "internal/turn/loop.go",
		Ask:     asked[0].Ask,
		Default: "refused the write and left the path untouched",
	}
	if asked[0] != want {
		t.Fatalf("the grant row is %+v, want %+v", asked[0], want)
	}
	if !strings.Contains(asked[0].Ask, "internal/turn/loop.go") {
		t.Fatalf("the grant does not name the path it needs: %q", asked[0].Ask)
	}
}

func TestAShellCommandReachingOutsideOwnsAsksForTheSameGrantAWriteWouldHave(t *testing.T) {
	boundary := &Boundary{Ticket: "BOJI-210", Owns: []string{"internal/subagent/**"}}
	if err := boundary.Command("gofmt -w internal/turn/spawn.go"); err == nil {
		t.Fatal("a command naming a path outside owns was allowed")
	}
	asked := boundary.Asked()
	if len(asked) != 1 || asked[0].Kind != Grant || asked[0].Where != "internal/turn/spawn.go" {
		t.Fatalf("the boundary asked %+v, want one grant for internal/turn/spawn.go", asked)
	}
	if err := boundary.Command("go test ./internal/subagent/..."); err != nil {
		t.Fatalf("a command inside owns was refused: %v", err)
	}
	if len(boundary.Asked()) != 1 {
		t.Fatalf("a command inside owns asked for a grant: %+v", boundary.Asked())
	}
}

func TestBOJI015DeletingAKonstFieldBreaksAFileOutsideOwnsAndTheTicketIsUntouched(t *testing.T) {
	boundary, ticket, filed := boji015Fixture(t)
	if err := boundary.Write("internal/konst/konst.go"); err != nil {
		t.Fatalf("the ticket holds internal/konst/** and the write was refused: %v", err)
	}

	broken := "internal/judge/jev/client_test.go"
	var denied DeniedError
	if !errors.As(boundary.Write(broken), &denied) {
		t.Fatalf("writing %s was not refused", broken)
	}
	asked := boundary.Asked()
	if len(asked) != 1 || asked[0].Kind != Grant || asked[0].Where != broken {
		t.Fatalf("the boundary asked %+v, want one grant for %s", asked, broken)
	}

	after, err := os.ReadFile(ticket)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(filed, after) {
		t.Fatal("the refused write changed the ticket file")
	}
}

func TestAWorkerCannotResolveARefusedWriteByWideningItsOwnFrontMatter(t *testing.T) {
	boundary, ticket, filed := boji015Fixture(t)
	broken := "internal/judge/jev/client_test.go"
	if boundary.Write(broken) == nil {
		t.Fatal("the write was not refused")
	}

	widened := bytes.Replace(filed,
		[]byte("  - internal/judge/jev/caps.go\n"),
		[]byte("  - internal/judge/jev/caps.go\n  - "+broken+"\n"), 1)
	if bytes.Equal(widened, filed) {
		t.Fatal("the fixture no longer carries the line the widening edits")
	}

	var refused OwnsEditError
	if !errors.As(boundary.EditTicket(ticket, widened), &refused) {
		t.Fatal("the worker widened its own owns in the same turn")
	}
	if len(refused.To) != len(refused.From)+1 {
		t.Fatalf("the refusal does not carry both lists: %+v", refused)
	}

	after, err := os.ReadFile(ticket)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(filed, after) {
		t.Fatal("the refused edit reached the ticket file")
	}
	if err := boundary.Write(broken); err == nil {
		t.Fatal("the path became writable after the refused widening")
	}
}

func TestAnEditThatLeavesOwnsAloneIsWritten(t *testing.T) {
	boundary, ticket, filed := boji015Fixture(t)
	logged := []byte(string(filed) + "\n" + Question{
		Ticket: "BOJI-015", Kind: Deferred, Where: "internal/konst/konst.go",
		Ask: "should the ceiling move", Default: "kept the documented number",
	}.Block())
	if err := boundary.EditTicket(ticket, logged); err != nil {
		t.Fatalf("an edit that leaves owns alone was refused: %v", err)
	}
	after, err := os.ReadFile(ticket)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(logged, after) {
		t.Fatal("the allowed edit did not reach the ticket file")
	}
}

func TestTheOtherThreeKindsGoThroughTheSameChannelAndCarryTheTicket(t *testing.T) {
	boundary := &Boundary{Ticket: "BOJI-210", Owns: []string{"internal/subagent/**"}}
	boundary.Ask(Question{Kind: Deferred, Where: "internal/turn/loop.go", Ask: "who takes it", Default: "left it undone"})
	boundary.Ask(Question{Kind: Assumption, Ask: "the brief means the filed owns", Default: "read it that way"})
	asked := boundary.Asked()
	if len(asked) != 2 {
		t.Fatalf("the boundary holds %d rows, want 2", len(asked))
	}
	for _, question := range asked {
		if question.Ticket != "BOJI-210" {
			t.Fatalf("a row does not carry the ticket: %+v", question)
		}
	}
}
