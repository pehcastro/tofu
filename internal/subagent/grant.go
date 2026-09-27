package subagent

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
)

type Boundary struct {
	Ticket string
	Owns   []string
	asked  []Question
}

func (b *Boundary) Write(path string) error {
	err := Allow(path, b.Owns)
	var denied DeniedError
	if err == nil || !errors.As(err, &denied) {
		return err
	}
	if !slices.ContainsFunc(b.asked, func(q Question) bool { return q.Kind == Grant && q.Where == path }) {
		b.asked = append(b.asked, Question{
			Ticket:  b.Ticket,
			Kind:    Grant,
			Where:   path,
			Ask:     "this ticket must write " + path + " and its owns does not hold it: widen the ticket or hand the path to whoever holds it",
			Default: "refused the write and left the path untouched",
		})
	}
	return err
}

func (b *Boundary) Ask(question Question) {
	question.Ticket = b.Ticket
	b.asked = append(b.asked, question)
}

func (b *Boundary) Asked() []Question { return b.asked }

type OwnsEditError struct {
	Ticket string
	From   []string
	To     []string
}

func (e OwnsEditError) Error() string {
	return fmt.Sprintf("%s may not rewrite its own owns (%s -> %s): a grant is the orchestrator's to make, so ask for it and stop", e.Ticket, strings.Join(e.From, ", "), strings.Join(e.To, ", "))
}

func frontMatterOwns(ticket []byte) []string {
	lines := strings.Split(strings.ReplaceAll(string(ticket), "\r\n", "\n"), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return nil
	}
	var owns []string
	reading := false
	for _, line := range lines[1:] {
		trimmed := strings.TrimSpace(line)
		switch {
		case trimmed == "---":
			return owns
		case strings.HasPrefix(line, "owns:"):
			reading = true
		case reading && strings.HasPrefix(trimmed, "- "):
			owns = append(owns, strings.TrimSpace(strings.TrimPrefix(trimmed, "- ")))
		case trimmed != "" && !strings.HasPrefix(line, " "):
			reading = false
		}
	}
	return owns
}

func (b *Boundary) EditTicket(file string, after []byte) error {
	before, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	from, to := frontMatterOwns(before), frontMatterOwns(after)
	if !slices.Equal(from, to) {
		return OwnsEditError{Ticket: b.Ticket, From: from, To: to}
	}
	return os.WriteFile(file, after, 0o600)
}
