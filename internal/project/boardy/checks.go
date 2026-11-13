package boardy

import (
	"cmp"
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"tofu/internal/librarian/rules"
	"tofu/internal/models/classifiers/core"
)

type checked struct {
	detail string
	failed bool
	done   *DoneGate
}

type namedCheck func(m Managed, h rules.Hook, s subject, arg string) (checked, error)

const (
	checkEvidenced     = "evidenced"
	checkCallersOwned  = "callers_owned"
	checkEpicReady     = "epic_ready"
	checkDoneStamp     = "done_stamp"
	everyLine          = "all"
	missingPlaceholder = "{missing}"
)

func checks() map[string]namedCheck {
	return map[string]namedCheck{
		"may_move":          Managed.mayMoveCheck,
		"not_possible_hold": Managed.notPossibleHold,
		"acceptance":        Managed.hasAcceptance,
		"log_entry":         Managed.hasLogEntry,
		"evidence_chars":    Managed.evidenceChars,
		"decidable":         Managed.decidableCheck,
		checkDoneStamp:      Managed.doneStamp,
		"walls_stamp":       Managed.wallsStamp,
		checkEvidenced:      Managed.evidenced,
		"before_captured":   Managed.beforeCaptured,
		"before_on_time":    Managed.beforeOnTime,
		"sub_tickets_done":  Managed.subTicketsDone,
		"scope_cites":       Managed.scopeCites,
		checkEpicReady:      Managed.epicReadyCheck,
		"owns_free":         Managed.ownsFreeCheck,
		"drafted":           Managed.draftedCheck,
		"feasibility":       Managed.feasibility,
		"done_reason":       Managed.doneReason,
		checkCallersOwned:   Managed.callersOwned,
	}
}

func checkOf(h rules.Hook) (string, string) {
	if h.Action.Do != rules.DoCheck {
		return checkEvidenced, ""
	}
	name, arg, _ := strings.Cut(h.Action.Arg, " ")
	return name, strings.TrimSpace(arg)
}

func runsCheck(h rules.Hook, name string) bool {
	called, _ := checkOf(h)
	return (h.Action.Do == rules.DoCheck || h.Action.Do == rules.DoContract) && called == name
}

func resolveCheck(h rules.Hook) (namedCheck, string, error) {
	name, arg := checkOf(h)
	known := checks()
	if run, found := known[name]; found {
		return run, arg, nil
	}
	return nil, "", fmt.Errorf("no check named %q, and the named checks are: %s", name, strings.Join(slices.Sorted(maps.Keys(known)), ", "))
}

func refusal(err error) (checked, error) {
	var refused RefusedError
	if errors.As(err, &refused) {
		return checked{detail: refused.Why, failed: true, done: refused.Done}, nil
	}
	return checked{}, err
}

func (m Managed) mayMoveCheck(_ rules.Hook, s subject, _ string) (checked, error) {
	board, err := m.Store.Board(s.key)
	if err != nil {
		return checked{}, err
	}
	return refusal(m.mayMove(m.role(board, s.actor), s.ticket, s.to, s.actor))
}

func (m Managed) notPossibleHold(_ rules.Hook, s subject, _ string) (checked, error) {
	board, err := m.Store.Board(s.key)
	if err != nil || m.role(board, s.actor) != RoleManager {
		return checked{}, err
	}
	return refusal(m.heldForThePerson(s.ticket, s.actor, "moves it"))
}

func (Managed) hasAcceptance(_ rules.Hook, s subject, _ string) (checked, error) {
	return checked{
		detail: fmt.Sprintf("it has no Acceptance, and a ticket reaches %s only with one", s.to),
		failed: !slices.ContainsFunc(s.ticket.Acceptance, func(lines string) bool { return strings.TrimSpace(lines) != "" }),
	}, nil
}

func (Managed) hasLogEntry(_ rules.Hook, s subject, _ string) (checked, error) {
	return checked{detail: fmt.Sprintf("its Log is empty, and a ticket reaches %s with at least one entry", s.to), failed: strings.TrimSpace(s.ticket.Log) == ""}, nil
}

func (m Managed) evidenceChars(_ rules.Hook, s subject, arg string) (checked, error) {
	board, err := m.Store.Board(s.key)
	least := board.LogToReview
	if least == 0 {
		least, _ = strconv.Atoi(arg)
	}
	if err != nil || least <= 0 {
		return checked{}, err
	}
	runes, err := m.evidenceRunes(s.ticket.ID)
	return checked{
		detail: fmt.Sprintf("its evidence entries have %d characters, and a ticket reaches %s on %s with at least %d: log them with ticket_log kind evidence or tofu boardy log --evidence", runes, s.to, s.key, least),
		failed: runes < least,
	}, err
}

func (Managed) decidableCheck(_ rules.Hook, s subject, _ string) (checked, error) {
	return refusal(decidable(s.ticket))
}

func (m Managed) doneStamp(_ rules.Hook, s subject, _ string) (checked, error) {
	gate := m.doneGate(s.ticket.Owns)
	return checked{detail: gate.Refusal(), failed: gate.Refusal() != "", done: &gate}, nil
}

func (m Managed) evidenced(h rules.Hook, s subject, arg string) (checked, error) {
	leave := s.leave
	if arg == everyLine {
		leave = []int{}
	}
	missing := m.unevidenced(s.ticket, leave)
	said := s.expand(cmp.Or(h.Text, "a ticket moves on with evidence for each acceptance line"))
	if !strings.Contains(said, missingPlaceholder) {
		said += ": " + missingPlaceholder
	}
	return checked{detail: strings.ReplaceAll(said, missingPlaceholder, missing), failed: missing != ""}, nil
}

func (m Managed) beforeCaptured(h rules.Hook, s subject, lateSays string) (checked, error) {
	events, err := m.Store.ticketEvents(s.ticket.ID)
	if err != nil || slices.ContainsFunc(events, func(e Event) bool { return e.Kind == Captured }) {
		return checked{}, err
	}
	if lateSays != "" && slices.ContainsFunc(events, func(e Event) bool { return e.Kind == LateBefore }) {
		said := slices.Contains(strings.FieldsFunc(strings.ToLower(s.reason), func(r rune) bool { return !unicode.IsLetter(r) }), lateSays)
		return checked{
			detail: fmt.Sprintf("its before is late, and a ticket with a late before reaches %s only with a reason that says %s: tofu boardy move %s %s --reason \"what was verified, before captured %s on the pre-change build\"", s.to, lateSays, s.ticket.ID, s.to, lateSays),
			failed: !s.dry && !said,
		}, nil
	}
	detail := fmt.Sprintf("it has no before capture, and a ticket reaches %s only with one, logged before its first evidence entry and its first commit: tofu boardy log %s --before \"what you ran and what it printed\", or ticket_log kind before", s.to, s.ticket.ID)
	if lateSays != "" {
		detail += fmt.Sprintf(". A before run on the pre-change build after that names it: tofu boardy log %s --before \"what it printed\" --late <commit or build>, and the done reason says %s", s.ticket.ID, lateSays)
	}
	if h.If.NotLabels != nil {
		detail += fmt.Sprintf(". A ticket labelled %s is not asked for one", strings.Join(h.If.NotLabels, " or "))
	}
	if !h.If.CreatedAfter.IsZero() {
		detail += fmt.Sprintf(", and a ticket made before %s closes without one and its done reason says so", utcStamp(h.If.CreatedAfter))
	}
	return checked{detail: detail, failed: true}, nil
}

func utcStamp(at time.Time) string { return at.UTC().Format(time.DateTime) + " UTC" }

func (m Managed) beforeOnTime(_ rules.Hook, s subject, _ string) (checked, error) {
	if s.entry != Captured {
		return checked{}, nil
	}
	events, err := m.Store.ticketEvents(s.ticket.ID)
	if err != nil {
		return checked{}, err
	}
	if slices.ContainsFunc(events, func(e Event) bool { return e.Kind == Evidence }) {
		return checked{detail: "its before is late: an evidence entry is already logged, so the change was proved before anything showed how things worked without it, and a before is captured before the first evidence entry and the ticket's first commit", failed: true}, nil
	}
	entered, first, err := m.firstCommit(s.ticket)
	return checked{
		detail: fmt.Sprintf("its before is late: commit %s touched its owns after it entered %s at %s, and a before is captured before the ticket's first commit, while it still shows how things work without the change", first, Doing, entered.UTC().Format(time.DateTime)),
		failed: first != "",
	}, err
}

func (m Managed) subTicketsDone(_ rules.Hook, s subject, _ string) (checked, error) {
	children, _ := filepath.Glob(filepath.Join(m.Store.BoardDir(s.key), TicketsDirName, s.ticket.ID, "*.md"))
	var live []string
	for _, path := range children {
		child, err := frontOf(path)
		if err != nil {
			return checked{}, err
		}
		if child.Status.Live() {
			live = append(live, child.ID)
		}
	}
	return checked{
		detail: fmt.Sprintf("its sub-tickets %s are still live, and a parent reaches %s only once they are done or dropped", strings.Join(live, ", "), s.to),
		failed: len(live) > 0,
	}, nil
}

func (m Managed) scopeCites(_ rules.Hook, s subject, arg string) (checked, error) {
	board, err := m.Store.Board(s.key)
	cites := board.ScopeCites
	if len(cites) == 0 {
		cites = slices.DeleteFunc(strings.Split(arg, ","), func(cite string) bool { return strings.TrimSpace(cite) == "" })
	}
	return checked{
		detail: fmt.Sprintf("its Scope names no source, and %s wants one of: %s", s.key, strings.Join(cites, ", ")),
		failed: len(cites) > 0 && !slices.ContainsFunc(cites, func(cite string) bool { return strings.Contains(s.ticket.Scope, strings.TrimSpace(cite)) }),
	}, err
}

func (m Managed) epicReadyCheck(_ rules.Hook, s subject, _ string) (checked, error) {
	return refusal(m.Store.epicReady(s.key, s.ticket.Front))
}

func (m Managed) ownsFreeCheck(_ rules.Hook, s subject, _ string) (checked, error) {
	board, err := m.Store.Board(s.key)
	if err != nil || (s.event == rules.HookTicketMove || s.event == rules.HookTicketClose) && board.Holding(s.from) {
		return checked{}, err
	}
	return refusal(m.ownsFree(s.key, s.ticket.Front, s.owns, s.to, s.replaces))
}

func (s subject) entering() bool {
	return s.from == "" || s.from == Triage && s.to != Dropped
}

func (Managed) draftedCheck(_ rules.Hook, s subject, _ string) (checked, error) {
	missing := s.ticket.undrafted()
	if !s.entering() || len(missing) == 0 {
		return checked{}, nil
	}
	names, says := make([]string, len(missing)), make([]string, len(missing))
	for i, drafted := range missing {
		names[i], says[i] = drafted.Heading, drafted.Heading+": "+drafted.Says
	}
	return checked{detail: fmt.Sprintf("%q has no %s, and a ticket is made with each of them. %s", s.ticket.Title, strings.Join(names, ", "), strings.Join(says, "; ")), failed: true}, nil
}

func (m Managed) feasibility(_ rules.Hook, s subject, _ string) (checked, error) {
	if !s.entering() || s.dry || strings.TrimSpace(s.ticket.Feasibility) != "" {
		return checked{}, nil
	}
	ruled := m.judged(feasibilityRef, feasibilityDecides, struct {
		Ticket jevTicket `json:"ticket"`
	}{jevTicketOf(s.ticket)}, nil)
	if ruled.unread != "" {
		return checked{}, m.heldForPerson(s, feasibilityRef, ruled.unread)
	}
	platform := ruled.answers[feasibilityDecides].Noul
	return checked{
		detail: fmt.Sprintf("Jev judged that %q asks for a platform or visual effect (%s, %s at %.2f against %.2f), and such a ticket is made with a %s section: %s", s.ticket.Title, feasibilityRef, feasibilityDecides, platform, ruled.threshold, FeasibilityHeading, feasibilitySays),
		failed: platform >= ruled.threshold,
	}, nil
}

func (m Managed) doneReason(_ rules.Hook, s subject, _ string) (checked, error) {
	parts := reasonParts(s.reason)
	switch {
	case s.dry:
		return checked{}, nil
	case len(parts) == 0:
		return checked{detail: fmt.Sprintf("its reason is empty, and a ticket reaches %s with a reason that says what was verified and that nothing is open: tofu boardy move %s %s --reason <what was verified>", s.to, s.ticket.ID, s.to), failed: true}, nil
	}
	options := make([]core.Option, len(parts))
	for i, part := range parts {
		options[i] = core.Option{Name: reasonPartPrefix + strconv.Itoa(i+1), Criteria: part}
	}
	ruled := m.judged(doneReasonRef, doneReasonDecides, struct {
		Ticket jevTicket `json:"ticket"`
		Reason string    `json:"reason"`
		Parts  []string  `json:"parts"`
	}{jevTicketOf(s.ticket), s.reason, parts}, options)
	if ruled.unread != "" {
		return checked{}, m.heldForPerson(s, doneReasonRef, ruled.unread)
	}
	quoted, open := openPart(ruled.answers, s.reason, parts, ruled.threshold)
	return checked{
		detail: fmt.Sprintf("its reason says %q is still open (Jev, %s, %s at %.2f against %.2f), and a ticket reaches %s only when nothing it asked for is open: make the open part its own ticket and keep %s open, or close it with a reason that names nothing open", quoted, doneReasonRef, doneReasonDecides, ruled.answers[doneReasonDecides].Noul, ruled.threshold, s.to, s.ticket.ID),
		failed: open,
	}, nil
}

func (m Managed) callersOwned(_ rules.Hook, s subject, _ string) (checked, error) {
	scan, err := m.callersOutside(s.ticket)
	return checked{detail: scan.refusal(s.ticket.ID), failed: len(scan.sites) > 0}, err
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
