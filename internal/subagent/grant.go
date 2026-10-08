package subagent

import (
	"errors"
	"fmt"
	"slices"
	"sync"
)

type Boundary struct {
	Ticket  string
	Scratch string
	mu      sync.Mutex
	owns    []string
	asked   []Question
}

func NewBoundary(ticket, scratch string, owns []string) *Boundary {
	return &Boundary{Ticket: ticket, Scratch: scratch, owns: slices.Clone(owns)}
}

func (b *Boundary) Owns() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return slices.Clone(b.owns)
}

func (b *Boundary) Regrant(owns []string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.owns = slices.Clone(owns)
	b.asked = slices.DeleteFunc(b.asked, func(q Question) bool { return q.Kind == Grant && Allow(q.Where, b.owns) == nil })
}

func (b *Boundary) Write(path string) error {
	if b.Scratched(path) {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	err := Allow(path, b.owns)
	var denied DeniedError
	if err == nil || !errors.As(err, &denied) {
		return err
	}
	if !slices.ContainsFunc(b.asked, func(q Question) bool { return q.Kind == Grant && q.Where == path }) {
		b.asked = append(b.asked, Question{
			Ticket:  b.Ticket,
			Kind:    Grant,
			Where:   path,
			Ask:     "this sub-agent must write " + path + " and its owns does not hold it: grant it with message, do grant and owns [" + path + "], or hand the path to whoever holds it",
			Default: "refused the write and left the path untouched",
		})
	}
	return err
}

type ShellSourceWriteError struct {
	Path string
}

func (e ShellSourceWriteError) Error() string {
	return fmt.Sprintf("%q is source: change it with edit or write, never through the shell, so the read check, the diagnostics and the diff all see the change", e.Path)
}

func (b *Boundary) Asked() []Question {
	b.mu.Lock()
	defer b.mu.Unlock()
	return slices.Clone(b.asked)
}
