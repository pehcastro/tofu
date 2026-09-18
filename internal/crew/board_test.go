package crew

import (
	"path/filepath"
	"testing"
)

func TestReadBoardReadsEveryTicketAndAuditsDoingForOverlap(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	tickets, err := ReadBoard(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(tickets) == 0 {
		t.Fatal("ReadBoard read zero tickets")
	}
	t.Logf("read %d tickets from the board", len(tickets))

	pairs, err := AuditDoing(tickets)
	if err != nil {
		t.Fatal(err)
	}
	if len(pairs) != 0 {
		t.Fatalf("found %d overlapping pair(s) in doing/: %v", len(pairs), pairs)
	}
}
