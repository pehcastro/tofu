package boardy

import (
	"cmp"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"tofu/internal/subagent"
	"tofu/internal/sys"
)

type Finding struct {
	Ticket string `json:"ticket"`
	File   string `json:"file"`
	What   string `json:"what"`
}

func (s Store) Lint(boards []Board) []Finding {
	var findings []Finding
	tickets := map[string]Ticket{}
	var holding []Ticket
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
			highest = max(highest, fileRef(path).Number)
			if board.Holding(ticket.Status) {
				holding = append(holding, ticket)
			}
		}
		raw, _ := os.ReadFile(filepath.Join(board.Dir, counterFileName))
		if counter, _ := strconv.Atoi(strings.TrimSpace(string(raw))); counter < highest {
			findings = append(findings, Finding{File: filepath.Join(board.Dir, counterFileName), What: fmt.Sprintf("the counter of %s is %d, behind its highest ticket %d", board.Key, counter, highest)})
		}
	}
	agents := AgentTypes(s.Root)
	for _, ticket := range tickets {
		findings = append(findings, s.lintTicket(ticket, tickets, agents)...)
	}
	for i, a := range holding {
		for _, b := range holding[i+1:] {
			if glob, other, found := ownsMeet(a.Owns, b.Owns); found && !family(a.Front, b.Front) {
				findings = append(findings, Finding{Ticket: a.ID, File: s.path(a.ID), What: fmt.Sprintf("%s and %s both hold their owns (%s, %s) and they overlap: %q and %q", a.ID, b.ID, a.Status, b.Status, glob, other)})
			}
		}
	}
	slices.SortFunc(findings, func(a, b Finding) int { return cmp.Or(cmp.Compare(a.Ticket, b.Ticket), cmp.Compare(a.What, b.What)) })
	return findings
}

func (s Store) path(id string) string { path, _ := s.TicketPath(id); return path }

func (s Store) lintTicket(ticket Ticket, tickets map[string]Ticket, agents []string) []Finding {
	var findings []Finding
	say := func(format string, args ...any) {
		findings = append(findings, Finding{Ticket: ticket.ID, File: s.path(ticket.ID), What: fmt.Sprintf(format, args...)})
	}
	if strings.TrimSpace(ticket.Title) == "" {
		say("%s has no title", ticket.ID)
	}
	heldBySpawn := strings.HasSuffix(ticket.Assignee, "/"+ticket.ID)
	if ticket.Status.Live() && ticket.Assignee != "" && !heldBySpawn && !slices.ContainsFunc(agents, func(name string) bool { return strings.EqualFold(name, ticket.Assignee) }) {
		say("%s is assigned %s, which is not a registered agent type, so a spawn for it is refused: the board lead assigns one of %s", ticket.ID, ticket.Assignee, strings.Join(agents, ", "))
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
		if err := unreadableOwns(glob); err != nil {
			say("%s owns %q: %v", ticket.ID, glob, err)
		}
	}
	if ticket.Parent == "" {
		return findings
	}
	parent, found := tickets[ticket.Parent]
	if !found {
		say("%s is a sub-ticket of %s, which no board in this project holds", ticket.ID, ticket.Parent)
	} else if glob := outsideOwns(ticket.Owns, parent.Owns); glob != "" {
		say("%s owns %q, outside its parent %s (%s)", ticket.ID, glob, parent.ID, strings.Join(parent.Owns, ", "))
	}
	return findings
}

var agentName = sys.LazyRegexp(`(?im)^name:\s*(\S+)\s*$`)

func AgentTypes(root string) []string {
	known := []string{"orchestrator", "general-purpose"}
	files, _ := filepath.Glob(filepath.Join(root, ".claude", "agents", "*.md"))
	shipped, _ := filepath.Glob(filepath.Join(root, "library", "*", "agents", "*.md"))
	for _, file := range append(files, shipped...) {
		data, _ := os.ReadFile(file)
		if name := agentName().FindStringSubmatch(string(data)); name != nil {
			known = append(known, name[1])
		}
	}
	slices.SortFunc(known, func(a, b string) int { return strings.Compare(strings.ToLower(a), strings.ToLower(b)) })
	return slices.Compact(known)
}

func family(a, b Front) bool {
	return a.ID != "" && a.ID == b.Parent || b.ID != "" && b.ID == a.Parent
}

func outsideOwns(owns, ceiling []string) string {
	for _, glob := range owns {
		if !slices.ContainsFunc(ceiling, func(parent string) bool { return ownsInside(heldGlob(glob), heldGlob(parent)) }) {
			return glob
		}
	}
	return ""
}

func ownsInside(glob, parent string) bool {
	if slices.Contains(strings.Split(glob, "/"), "..") {
		return false
	}
	if !strings.Contains(glob, "*") {
		if path.Ext(glob) != "" {
			matched, err := subagent.Matches(glob, []string{parent})
			return matched && err == nil
		}
		glob = strings.TrimSuffix(glob, "/") + "/**"
	}
	tree, isTree := strings.CutSuffix(parent, "/**")
	if !isTree && !strings.Contains(parent, "*") && path.Ext(parent) == "" {
		tree, isTree = strings.TrimSuffix(parent, "/"), true
	}
	return glob == parent || parent == "**" || isTree && strings.HasPrefix(glob, tree+"/")
}

func unreadableOwns(glob string) error {
	held := heldGlob(glob)
	_, err := subagent.Matches(literalPrefix(held), []string{held})
	return err
}

func ownsMeet(left, right []string) (string, string, bool) {
	for _, a := range left {
		for _, b := range right {
			if subagent.Overlap(heldGlob(a), heldGlob(b)) {
				return a, b, true
			}
		}
	}
	return "", "", false
}

func heldGlob(glob string) string {
	slashed := strings.ReplaceAll(glob, `\`, "/")
	drive, rest, found := strings.Cut(slashed, ":")
	if !found || len(drive) != 1 || !unicode.IsLetter(rune(drive[0])) || rest != "" && rest[0] != '/' {
		return slashed
	}
	return "/" + strings.ToLower(drive) + rest
}

func literalPrefix(glob string) string {
	segments := strings.Split(glob, "/")
	for i, segment := range segments {
		if strings.ContainsAny(segment, "*?[") {
			return cmp.Or(strings.Join(segments[:i], "/"), ".")
		}
	}
	return strings.TrimSuffix(glob, "/")
}
