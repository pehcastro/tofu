package tools_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"tofu/internal/boardy"
	"tofu/internal/subagent"
	"tofu/internal/turn"
	"tofu/internal/turn/tools"
)

func ticketBoard(t *testing.T) boardy.Managed {
	t.Helper()
	board := boardy.Managed{Local: boardy.Local{Store: boardy.Store{Dir: filepath.Join(t.TempDir(), boardy.BoardsDirName)}}, Person: "person"}
	if _, err := board.Store.Init("DEMO", "demo"); err != nil {
		t.Fatal(err)
	}
	if _, err := board.AddManager("DEMO", "lead", "person"); err != nil {
		t.Fatal(err)
	}
	for _, ticket := range []boardy.Ticket{
		{Front: boardy.Front{Title: "no owns"}, Acceptance: []string{"- it parses"}},
		{Front: boardy.Front{Title: "parse the config", Owns: []string{"internal/a/**"}, Status: boardy.Todo}, Acceptance: []string{"- the old line", "- UTF-8 input parses\n- the test passes"}},
	} {
		if _, err := board.Create("DEMO", ticket, "lead"); err != nil {
			t.Fatal(err)
		}
	}
	return board
}

func subAgentTool(t *testing.T, board boardy.Managed, name string) turn.Tool {
	t.Helper()
	list, err := tools.TicketTools(board, "go-dev-1")
	if err != nil {
		t.Fatal(err)
	}
	at := slices.IndexFunc(list, func(tool turn.Tool) bool { return tool.Name() == name })
	if at < 0 {
		t.Fatalf("%s is not among a sub-agent's ticket tools", name)
	}
	return list[at]
}

func runTicket(tool turn.Tool, args string) (string, error) {
	result, err := tool.Run(context.Background(), json.RawMessage(args))
	return result.Content, err
}

func TestTicketContractReadsTheBoardKeyAndTheRevisionInForce(t *testing.T) {
	ticket, err := ticketBoard(t).Store.Get("DEMO-2")
	if err != nil {
		t.Fatal(err)
	}
	report := "UTF-8 is fine\n```json\n{\"claims\":[{\"line\":\"the test passes\",\"command\":\"go test\",\"output\":\"ok\",\"met\":true}]}\n```"
	contract := subagent.TicketContract(ticket.ID, ticket.Acceptance, report, false)
	lines := make([]string, len(contract.Claims))
	for i, claim := range contract.Claims {
		lines[i] = claim.Line
	}
	if contract.TicketID != "DEMO-2" || !slices.Equal(lines, []string{"UTF-8 input parses", "the test passes"}) || contract.Omissions() != 1 {
		t.Errorf("contract reads %s with lines %q and %d omissions, want DEMO-2, the revision in force and 1", contract.TicketID, lines, contract.Omissions())
	}
}

func TestTicketGrantAssignsMovesAndRefuses(t *testing.T) {
	board := ticketBoard(t)
	if _, err := tools.TicketGrant(board, "DEMO-1", "lead", "go-dev-1"); err == nil {
		t.Error("a ticket with no owns was granted")
	}
	var refused boardy.RefusedError
	if _, err := tools.TicketGrant(board, "DEMO-2", "someone", "go-dev-1"); !errors.As(err, &refused) {
		t.Errorf("a lead that manages nothing got %v, want a refusal", err)
	}
	owns, err := tools.TicketGrant(board, "DEMO-2", "lead", "go-dev-1")
	ticket, _ := board.Store.Get("DEMO-2")
	if err != nil || !slices.Equal(owns, []string{"internal/a/**"}) || ticket.Assignee != "go-dev-1" || ticket.Status != boardy.Doing {
		t.Fatalf("grant gave %q, %v; ticket is %s for %q", owns, err, ticket.Status, ticket.Assignee)
	}
}

func TestTicketToolsKeepASubAgentToItsOwnTicket(t *testing.T) {
	board := ticketBoard(t)
	if _, err := tools.TicketGrant(board, "DEMO-2", "lead", "go-dev-1"); err != nil {
		t.Fatal(err)
	}
	move := subAgentTool(t, board, "ticket_move")
	offered, _ := json.Marshal(move.Definition().Parameters)
	if strings.Contains(string(offered), `"done"`) {
		t.Errorf("a sub-agent's ticket_move offers done: %s", offered)
	}
	if _, err := runTicket(move, `{"id":"DEMO-2","to":"review"}`); err == nil {
		t.Error("a move to review with an empty Log succeeded")
	}
	if _, err := runTicket(subAgentTool(t, board, "ticket_log"), `{"id":"DEMO-1","text":"not mine"}`); err == nil {
		t.Error("a log line on a ticket assigned to someone else was written")
	}
	if _, err := runTicket(subAgentTool(t, board, "ticket_log"), `{"id":"DEMO-2","text":"go test ok"}`); err != nil {
		t.Fatal(err)
	}
	if _, err := runTicket(move, `{"id":"DEMO-2","to":"review"}`); err != nil {
		t.Fatal(err)
	}
	read, err := runTicket(subAgentTool(t, board, "ticket_read"), `{"id":"DEMO-2"}`)
	if err != nil || !strings.Contains(read, "status: review") || !strings.Contains(read, "go-dev-1: go test ok") {
		t.Errorf("ticket_read gave %v:\n%s", err, read)
	}
	if _, err := runTicket(subAgentTool(t, board, "ticket_request"), `{"board":"DEMO","title":"a follow-up","acceptance":"- it is filed"}`); err != nil {
		t.Fatal(err)
	}
	if requests, _ := board.Store.Requests("DEMO"); len(requests) != 1 {
		t.Errorf("a sub-agent's request left %d triage requests, want 1", len(requests))
	}
	events, _ := board.Store.Events("DEMO", 0)
	var seen []string
	for _, event := range events {
		seen = append(seen, string(event.Kind)+" "+event.Actor)
	}
	for _, want := range []string{"assigned lead", "moved lead", "commented go-dev-1", "moved go-dev-1", "requested go-dev-1"} {
		if !slices.Contains(seen, want) {
			t.Errorf("events.jsonl has no %q: %q", want, seen)
		}
	}
}

func TestTicketToolsWriteNoBoardWhenThereIsNone(t *testing.T) {
	dir := filepath.Join(t.TempDir(), boardy.BoardsDirName)
	list, err := tools.TicketTools(boardy.Managed{Local: boardy.Local{Store: boardy.Store{Dir: dir}}}, "lead")
	if _, statErr := os.Stat(dir); err != nil || !errors.Is(statErr, os.ErrNotExist) || len(list) == 0 {
		t.Errorf("building %d tools gave %v and the boards folder stats as %v", len(list), err, statErr)
	}
}
