package boardy

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"strings"

	"tofu/internal/rule"
)

type namedCheck func(h rule.Hook, s subject, arg string) (detail string, failed bool, err error)

const checkEvidenced = "evidenced"

func (m Managed) checks() map[string]namedCheck {
	return map[string]namedCheck{
		checkEvidenced: func(h rule.Hook, s subject, _ string) (string, bool, error) {
			missing := m.unevidenced(s.ticket, s.leave)
			return cmp.Or(h.Text, "a ticket moves on with evidence for each acceptance line") + ": " + missing, missing != "", nil
		},
	}
}

func (m Managed) namedCheck(h rule.Hook) (namedCheck, string, error) {
	name, arg := checkEvidenced, ""
	if h.Action.Do == rule.DoCheck {
		name, arg, _ = strings.Cut(h.Action.Arg, " ")
	}
	checks := m.checks()
	if run, known := checks[name]; known {
		return run, strings.TrimSpace(arg), nil
	}
	return nil, "", fmt.Errorf("hook %q: no check named %q, and the named checks are: %s", h.ID, name, strings.Join(slices.Sorted(maps.Keys(checks)), ", "))
}

func (m Managed) unevidenced(ticket Ticket, leave []int) string {
	lines, err := m.Store.Acceptance(ticket, leave)
	switch {
	case err != nil:
		return "its evidence could not be read: " + err.Error()
	case len(lines) == 0:
		return ticket.ID + " has no acceptance lines to evidence"
	}
	return UnmetLines(lines)
}

func UnmetLines(lines []AcceptanceLine) string {
	var unmet []string
	for i, line := range lines {
		if line.State == LineUnmet {
			unmet = append(unmet, fmt.Sprintf("line %d, %s", i+1, line.Line))
		}
	}
	if len(unmet) == 0 {
		return ""
	}
	return fmt.Sprintf("%d of %d acceptance lines in force have no evidence entry naming them: %s", len(unmet), len(lines), strings.Join(unmet, "; "))
}
