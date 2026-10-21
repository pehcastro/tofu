package boardy

import (
	"os"
	"path/filepath"
	"slices"

	"tofu/internal/sys"
)

type starterHook struct{ id, source string }

func starterHooks() []starterHook {
	return []starterHook{
		{"epic_suggest", `id: epic_suggest
when: before
on: ticket.create
do: set epic jev board_epic@1
if:
  epic: empty`},
		{"epic_at_create", `id: epic_at_create
when: before
on: ticket.create
do: field epic
then: refuse
text: every ticket belongs to an epic: name one from epic list, or make it first with epic new`},
		{"evidence_at_review", `id: evidence_at_review
when: before
on: ticket.move
do: contract
then: refuse
text: a ticket reaches review with evidence for each acceptance line, what was run and what it printed, in the last sub-agent report's contract or, with no report, a Log line for each
if:
  to: review
  actor: lead, sub-agent, hook`},
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
		path := filepath.Join(dir, h.id+".yaml")
		kept, err := sys.Exists(path)
		switch {
		case err != nil:
			return err
		case kept:
			continue
		}
		if err := writeAtomic(path, []byte(h.source+"\n")); err != nil {
			return err
		}
	}
	return nil
}

func StarterHookIDs() []string {
	hooks := starterHooks()
	ids := make([]string, len(hooks))
	for i, h := range hooks {
		ids[i] = h.id
	}
	return ids
}

func Unruled(boards []Board) []Board {
	return slices.DeleteFunc(slices.Clone(boards), func(board Board) bool {
		rules, _ := filepath.Glob(filepath.Join(board.Dir, RulesDirName, "*.yaml"))
		return len(rules) > 0
	})
}
