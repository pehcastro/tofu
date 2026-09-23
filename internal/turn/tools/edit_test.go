package tools_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"tofu/internal/session"
	"tofu/internal/sys"
	"tofu/internal/turn"
	"tofu/internal/turn/tools"
)

func editingCall(t *testing.T, root string, ledger *turn.ReadLedger, args string) (turn.Result, error) {
	t.Helper()
	tool, err := tools.NewEdit(root)
	if err != nil {
		t.Fatalf("building the tool: %v", err)
	}
	if ledger != nil {
		tool = tool.Reading(ledger)
	}
	return tool.Run(context.Background(), json.RawMessage(args))
}

func TestAnEditToAFileThisTurnHasNotReadIsRefused(t *testing.T) {
	root := t.TempDir()
	seed(t, root, "a.txt", "original line one\noriginal line two\n")

	_, err := editingCall(t, root, turn.NewReadLedger(), `{"path":"a.txt","old_string":"original line one","new_string":"changed"}`)
	refusal(t, err, "a.txt has not been read by this turn", "original line one", "original line two")
	if got := held(t, root, "a.txt"); strings.Contains(got, "changed") {
		t.Fatalf("a refused edit changed the file: %s", got)
	}
}

func TestAnEditToAFileThisTurnHasReadSucceeds(t *testing.T) {
	root := t.TempDir()
	seed(t, root, "a.txt", "original\n")
	ledger := turn.NewReadLedger()
	ledger.Mark("a.txt")

	if _, err := editingCall(t, root, ledger, `{"path":"a.txt","old_string":"original","new_string":"changed"}`); err != nil {
		t.Fatalf("a file this turn read must not be refused: %v", err)
	}
	if got := held(t, root, "a.txt"); !strings.Contains(got, "changed") {
		t.Fatalf("the permitted edit did not apply: %s", got)
	}
}

func TestASecondEditToTheSamePathNeedsNoSecondReadBecauseTheFirstEditMarkedIt(t *testing.T) {
	root := t.TempDir()
	seed(t, root, "a.txt", "one\n")
	ledger := turn.NewReadLedger()
	ledger.Mark("a.txt")

	if _, err := editingCall(t, root, ledger, `{"path":"a.txt","old_string":"one","new_string":"two"}`); err != nil {
		t.Fatalf("the first edit, after a read, must succeed: %v", err)
	}
	if _, err := editingCall(t, root, ledger, `{"path":"a.txt","old_string":"two","new_string":"three"}`); err != nil {
		t.Fatalf("the second edit, right after the first, must not need a fresh read: %v", err)
	}
	if got := held(t, root, "a.txt"); !strings.Contains(got, "three") {
		t.Fatalf("the second edit did not apply: %s", got)
	}
}

func TestAWriteMarksTheLedgerSoAFollowingEditNeedsNoSeparateRead(t *testing.T) {
	root := t.TempDir()
	writeTool, err := turn.NewWriteTool(root)
	if err != nil {
		t.Fatalf("building write: %v", err)
	}
	ledger := turn.NewReadLedger()
	writeTool = writeTool.Reading(ledger)

	args, err := json.Marshal(struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}{Path: "b.txt", Content: "fresh\n"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writeTool.Run(context.Background(), args); err != nil {
		t.Fatalf("creating a new file must never be refused: %v", err)
	}

	if _, err := editingCall(t, root, ledger, `{"path":"b.txt","old_string":"fresh","new_string":"edited"}`); err != nil {
		t.Fatalf("a file this turn just wrote must count as read: %v", err)
	}
}

func TestAnEditWithNoLedgerAttachedIsNeverRefusedForBeingUnread(t *testing.T) {
	root := t.TempDir()
	seed(t, root, "a.txt", "original\n")

	if _, err := editingCall(t, root, nil, `{"path":"a.txt","old_string":"original","new_string":"changed"}`); err != nil {
		t.Fatalf("a caller that attaches no ledger keeps the old, ungated behaviour: %v", err)
	}
}

type replayedTurn struct {
	session string
	edits   int
	blind   int
}

func replayReadBeforeWrite(said []session.Utterance) (edits, blind int) {
	seen := map[string]bool{}
	for _, utterance := range said {
		for _, call := range utterance.Calls {
			var args struct {
				Path string `json:"path"`
			}
			if err := json.Unmarshal(call.Args, &args); err != nil || args.Path == "" {
				continue
			}
			path := filepath.ToSlash(filepath.Clean(args.Path))
			switch call.Name {
			case "read":
				seen[path] = true
			case "edit", "write":
				if call.Name == "edit" {
					edits++
					if !seen[path] {
						blind++
					}
				}
				seen[path] = true
			}
		}
	}
	return edits, blind
}

func TestReplayOfRecordedTurnsCountsHowManyRealEditsThisRuleWouldHaveRefused(t *testing.T) {
	dir := filepath.Join(sys.SourceRoot(), ".tofu", "sessions")
	if _, err := os.Stat(dir); err != nil {
		t.Skip("skip: this checkout has no .tofu/sessions, so there is nothing recorded to replay")
	}
	store := session.NewStore(dir)
	listing, err := store.Listing()
	if err != nil {
		t.Fatalf("listing the recorded sessions: %v", err)
	}
	if len(listing.Sessions) == 0 {
		t.Skip("skip: the recorded sessions directory holds no session")
	}

	var totalEdits, totalBlind int
	var perTurn []replayedTurn
	for _, header := range listing.Sessions {
		conversation, err := store.Conversation(header.ID)
		if err != nil {
			t.Fatalf("reading %s: %v", header.ID, err)
		}
		edits, blind := replayReadBeforeWrite(conversation.Said)
		totalEdits += edits
		totalBlind += blind
		if edits > 0 {
			perTurn = append(perTurn, replayedTurn{session: header.ID, edits: edits, blind: blind})
		}
	}

	sort.Slice(perTurn, func(i, j int) bool { return perTurn[i].blind > perTurn[j].blind })
	for i, entry := range perTurn {
		if i >= 5 {
			break
		}
		t.Logf("  %s: %d of %d edits blind", entry.session, entry.blind, entry.edits)
	}
	if totalEdits == 0 {
		t.Skip("skip: no recorded turn called edit, so there is nothing to score the rule against")
	}
	fmt.Printf("read-before-write replay: %d edit calls across %d turns, %d would have been refused (%.1f%%)\n",
		totalEdits, len(listing.Sessions), totalBlind, 100*float64(totalBlind)/float64(totalEdits))
}
