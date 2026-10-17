package boardy

import (
	"os"
	"path/filepath"
)

type starterHook struct{ id, source string }

func starterHooks() []starterHook {
	return []starterHook{
		{"epic_at_create", `id: epic_at_create
when: before
on: ticket.create
do: field epic
then: refuse
text: every ticket belongs to an epic: name one from epic list, or make it first with epic new`},
		{"evidence_at_review", `id: evidence_at_review
when: before
on: ticket.move
do: field log
then: refuse
text: a ticket reaches review with the evidence for each acceptance line in force in its Log: what was run and what it printed
if:
  to: review`},
		{"report_to_review", `id: report_to_review
when: after
on: agent.report
do: move review
if:
  from: doing`},
		{"review_waits", `id: review_waits
when: after
on: schedule
every: 1h
do: notify {ticket} has waited in review over 2h: read its Log, run its checks yourself and move it
if:
  to: review
  in_status_for: 2h`},
	}
}

func (s Store) WriteStarterHooks(key string) error {
	dir := filepath.Join(s.BoardDir(key), RulesDirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for _, h := range starterHooks() {
		if err := writeAtomic(filepath.Join(dir, h.id+".yaml"), []byte(h.source+"\n")); err != nil {
			return err
		}
	}
	return nil
}
