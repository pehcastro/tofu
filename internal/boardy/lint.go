package boardy

import (
	"cmp"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"tofu/internal/subagent"
)

type Finding struct {
	Ticket string `json:"ticket"`
	File   string `json:"file"`
	What   string `json:"what"`
}

func (s Store) Lint(boards []Board) []Finding {
	var findings []Finding
	tickets := map[string]Ticket{}
	var live []Ticket
	for _, board := range boards {
		paths, err := s.TicketFiles(board.Key)
		if err != nil {
			findings = append(findings, Finding{File: board.Dir, What: err.Error()})
		}
		highest := 0
		for _, path := range paths {
			ticket, err := ReadTicket(path)
			if err != nil {
				findings = append(findings, Finding{Ticket: strings.TrimSuffix(filepath.Base(path), ".md"), File: path, What: err.Error()})
				continue
			}
			tickets[ticket.ID] = ticket
			highest = max(highest, ticketNumber(path))
			if ticket.Status.Live() {
				live = append(live, ticket)
			}
		}
		raw, _ := os.ReadFile(filepath.Join(board.Dir, counterFileName))
		if counter, _ := strconv.Atoi(strings.TrimSpace(string(raw))); counter < highest {
			findings = append(findings, Finding{File: filepath.Join(board.Dir, counterFileName), What: fmt.Sprintf("the counter of %s is %d, behind its highest ticket %d", board.Key, counter, highest)})
		}
	}
	for _, ticket := range tickets {
		findings = append(findings, s.lintTicket(ticket, tickets)...)
	}
	for i, a := range live {
		for _, b := range live[i+1:] {
			if glob, other, found := ownsMeet(a.Owns, b.Owns); found {
				findings = append(findings, Finding{Ticket: a.ID, File: s.path(a.ID), What: fmt.Sprintf("%s and %s are both live and their owns overlap: %q and %q", a.ID, b.ID, glob, other)})
			}
		}
	}
	slices.SortFunc(findings, func(a, b Finding) int { return cmp.Or(cmp.Compare(a.Ticket, b.Ticket), cmp.Compare(a.What, b.What)) })
	return findings
}

func (s Store) path(id string) string { path, _ := s.TicketPath(id); return path }

func (s Store) lintTicket(ticket Ticket, tickets map[string]Ticket) []Finding {
	var findings []Finding
	say := func(format string, args ...any) {
		findings = append(findings, Finding{Ticket: ticket.ID, File: s.path(ticket.ID), What: fmt.Sprintf(format, args...)})
	}
	if strings.TrimSpace(ticket.Title) == "" {
		say("%s has no title", ticket.ID)
	}
	if ticket.Status == Blocked && ticket.Reason == "" {
		say("%s is blocked with no reason", ticket.ID)
	}
	if ticket.Points < 0 {
		say("%s has negative points", ticket.ID)
	}
	references := map[string][]string{"depends": ticket.Depends, "blocks": ticket.Blocks, "relates": ticket.Relates, "duplicates": ticket.Duplicates}
	for _, field := range []string{"depends", "blocks", "relates", "duplicates"} {
		for _, id := range references[field] {
			if _, found := tickets[id]; !found {
				say("%s %s %s, which no board in this project holds", ticket.ID, field, id)
			}
		}
	}
	for _, glob := range ticket.Owns {
		if _, err := subagent.Matches(literalPrefix(glob), []string{glob}); err != nil {
			say("%s owns %q: %v", ticket.ID, glob, err)
		}
	}
	return findings
}

func ownsMeet(left, right []string) (string, string, bool) {
	for _, a := range left {
		for _, b := range right {
			atA, _ := subagent.Matches(literalPrefix(a), []string{b})
			atB, _ := subagent.Matches(literalPrefix(b), []string{a})
			if atA || atB {
				return a, b, true
			}
		}
	}
	return "", "", false
}

func literalPrefix(glob string) string {
	segments := strings.Split(strings.ReplaceAll(glob, `\`, "/"), "/")
	for i, segment := range segments {
		if strings.ContainsAny(segment, "*?[") {
			return cmp.Or(strings.Join(segments[:i], "/"), ".")
		}
	}
	return strings.TrimSuffix(glob, "/")
}
