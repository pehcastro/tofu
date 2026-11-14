package boardy

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"tofu/internal/agent/subagents"
	"tofu/internal/judge/ledger"
	question "tofu/internal/judge/questions"
	"tofu/internal/librarian/rules"
	"tofu/internal/models/classifiers"
	"tofu/internal/models/classifiers/core"
	"tofu/internal/platform/fs"
	"tofu/internal/platform/project"
	"tofu/library/questions"
)

func (m Managed) Create(key string, ticket Ticket, actor string) (Ticket, error) {
	board, err := m.Store.Board(key)
	if err != nil {
		return Ticket{}, err
	}
	if ticket.Status == Triage || m.role(board, actor) != RoleManager {
		return m.Request(key, ticket, actor)
	}
	if err := m.Store.planned(key, ticket.Front); err != nil {
		return Ticket{}, err
	}
	ticket.Acceptance = bulleted(ticket.Acceptance)
	return m.create(key, ticket, actor, "")
}

func (m Managed) Request(key string, ticket Ticket, actor string) (Ticket, error) {
	if _, err := m.Store.Board(key); err != nil {
		return Ticket{}, err
	}
	if err := m.Store.planned(key, ticket.Front); err != nil {
		return Ticket{}, err
	}
	ticket.Acceptance = bulleted(ticket.Acceptance)
	return m.request(key, ticket, actor)
}

func bulleted(revisions []string) []string {
	var out []string
	for _, revision := range revisions {
		lines := slices.DeleteFunc(strings.Split(revision, "\n"), func(line string) bool { return strings.TrimSpace(line) == "" })
		if !slices.ContainsFunc(lines, func(line string) bool { return strings.HasPrefix(line, "- ") }) {
			for i, line := range lines {
				lines[i] = "- " + strings.TrimSpace(line)
			}
			revision = strings.Join(lines, "\n")
		}
		out = append(out, revision)
	}
	return out
}

func (m Managed) create(key string, ticket Ticket, actor, replaces string) (Ticket, error) {
	return m.created(key, ticket, actor, replaces, 0)
}

func (m Managed) created(key string, ticket Ticket, actor, replaces string, depth int) (Ticket, error) {
	if _, err := m.Store.Board(key); err != nil {
		return Ticket{}, err
	}
	if err := m.referencesResolve(ticket); err != nil {
		return Ticket{}, err
	}
	preview, err := os.CreateTemp(m.Store.BoardDir(key), ".tofu-create-*.md")
	if err != nil {
		return Ticket{}, err
	}
	_, err = preview.Write(ticket.Markdown())
	err = errors.Join(err, preview.Close())
	s := subject{event: rules.HookTicketCreate, key: key, ticket: ticket, path: preview.Name(), to: cmp.Or(ticket.Status, Backlog), actor: actor, depth: depth,
		owns: ticket.Owns, fields: filled(ticket), replaces: replaces}
	if err == nil {
		err = m.gate(&s)
	}
	if err = errors.Join(err, os.Remove(preview.Name())); err != nil {
		return Ticket{}, err
	}
	made, err := m.Local.Create(key, s.ticket, actor)
	if err != nil {
		return made, err
	}
	s.ticket = made
	s.path, _ = m.Store.TicketPath(made.ID)
	return made, m.after(s)
}

func (m Managed) Move(id string, to Status, reason, actor string) (Ticket, error) {
	return m.MoveLeaving(id, to, reason, actor, nil)
}

func (m Managed) MoveLeaving(id string, to Status, reason, actor string, leave []int) (Ticket, error) {
	ticket, board, err := m.ticketOnBoard(id)
	if err != nil || ticket.Status == to {
		return ticket, err
	}
	return m.move(board, ticket, to, reason, actor, 0, leave)
}

func (m Managed) move(board Board, ticket Ticket, to Status, reason, actor string, depth int, leave []int) (Ticket, error) {
	if err := m.validMove(ticket, to); err != nil {
		return ticket, err
	}
	s := m.subjectOf(moveEvent(to), board.Key, ticket, to, actor, depth)
	s.leave, s.reason = leave, reason
	hooks, err := m.Store.hooks(board.Key)
	if err != nil {
		return ticket, err
	}
	if excused := m.excused(hooks, s); excused != "" {
		reason = strings.TrimSpace(reason + " (" + excused + ")")
	}
	if strings.HasPrefix(reason, notOwnedOnPurpose) {
		if reason, s.waived, err = m.waive(hooks, s, reason); err != nil {
			return ticket, err
		}
	}
	if err := m.gate(&s); err != nil {
		return ticket, err
	}
	moved, err := m.Local.Move(ticket.ID, to, reason, actor, leave)
	if err != nil {
		return moved, err
	}
	s.ticket = moved
	return moved, m.after(s)
}

func (m Managed) Check(event rules.Event, id string, to Status, actor string) ([]Fired, error) {
	ticket, board, err := m.ticketOnBoard(id)
	if err != nil {
		return nil, err
	}
	to = cmp.Or(to, ticket.Status)
	s := m.subjectOf(event.Hook(), board.Key, ticket, to, actor, 0)
	s.dry = true
	switch event {
	case rules.EventMove, rules.EventClose:
		s.event, err = moveEvent(to), m.validMove(ticket, to)
	case rules.EventCreate:
		s.from, s.fields, err = "", filled(ticket), m.referencesResolve(ticket)
	case rules.EventWiden:
		err = m.Manages(board.Key, actor, "widen owns")
	default:
		return nil, fmt.Errorf("%q is not a board event", event)
	}
	if err != nil {
		return nil, err
	}
	hooks, err := m.Store.hooks(board.Key)
	if err != nil {
		return nil, err
	}
	fired, _, err := m.weigh(hooks, rules.WhenBefore, s)
	return fired, err
}

func moveEvent(to Status) rules.HookEvent {
	if !to.Live() {
		return rules.HookTicketClose
	}
	return rules.HookTicketMove
}

func (m Managed) Comment(id, text, actor string) (Ticket, error) {
	return m.appended(id, Commented, text, actor)
}

func (m Managed) Evidence(id, text, actor string, line int) (Ticket, error) {
	ticket, board, err := m.mayAppend(id, actor)
	if err != nil {
		return ticket, err
	}
	if lines := len(subagents.InForce(ticket.Acceptance)); line < 0 || line > lines {
		return ticket, fmt.Errorf("line counts %s's acceptance lines from 1, and it has %d", id, lines)
	}
	return m.entry(board, ticket, Event{Kind: Evidence, Text: text, Line: line}, actor, 0)
}

func (m Managed) CaptureBefore(id, text, actor string) (Ticket, error) {
	return m.appended(id, Captured, text, actor)
}

func (m Managed) firstCommit(ticket Ticket) (time.Time, string, error) {
	events, err := m.Store.ticketEvents(ticket.ID)
	at := slices.IndexFunc(events, func(e Event) bool {
		return e.To == Doing && (e.Kind == Created || e.Kind == Moved || e.Kind == Accepted)
	})
	if err != nil || at < 0 || len(ticket.Owns) == 0 || m.Store.Root == "" {
		return time.Time{}, "", err
	}
	entered := events[at].At
	if _, noCommit := gitOut(m.waits(), m.Store.Root, "", "rev-parse", "--verify", "--quiet", "HEAD"); noCommit != nil {
		return entered, "", nil
	}
	args := []string{"log", "--format=%h %ct", "--since=@" + strconv.FormatInt(entered.Unix(), 10), "--"}
	for _, glob := range ticket.Owns {
		args = append(args, ":(glob,icase)"+glob)
	}
	out, err := gitOut(m.waits(), m.Store.Root, "", args...)
	first := ""
	for line := range strings.Lines(out) {
		hash, committed, _ := strings.Cut(strings.TrimSpace(line), " ")
		if unix, parseErr := strconv.ParseInt(committed, 10, 64); parseErr == nil && unix > entered.Unix() {
			first = hash + " at " + time.Unix(unix, 0).UTC().Format(time.DateTime)
		}
	}
	return entered, first, err
}

func (m Managed) appended(id string, kind EventKind, text, actor string) (Ticket, error) {
	ticket, board, err := m.mayAppend(id, actor)
	if err != nil {
		return ticket, err
	}
	return m.entry(board, ticket, Event{Kind: kind, Text: text}, actor, 0)
}

func (m Managed) evidenceRunes(id string) (int, error) {
	thread, err := m.Store.Thread(id)
	runes := 0
	for _, event := range thread {
		if event.Kind == Evidence {
			runes += utf8.RuneCountInString(event.Text)
		}
	}
	return runes, err
}

func (m Managed) comment(board Board, ticket Ticket, text, actor string, depth int) (Ticket, error) {
	return m.entry(board, ticket, Event{Kind: Commented, Text: text}, actor, depth)
}

func (m Managed) entry(board Board, ticket Ticket, event Event, actor string, depth int) (Ticket, error) {
	events := []rules.HookEvent{rules.HookTicketLog}
	switch event.Kind {
	case Evidence:
		events = []rules.HookEvent{rules.HookTicketEvidence, rules.HookTicketLog}
	case Commented:
		events = []rules.HookEvent{rules.HookTicketComment, rules.HookTicketLog}
	}
	subjects := make([]subject, len(events))
	for i, on := range events {
		subjects[i] = m.subjectOf(on, board.Key, ticket, ticket.Status, actor, depth)
		subjects[i].entry = event.Kind
		if err := m.gate(&subjects[i]); err != nil {
			return ticket, err
		}
	}
	text := event.Text
	if event.Kind == Captured {
		text = "before capture: " + text
	}
	logged, err := m.appendLog(ticket.ID, text, actor, event)
	for _, s := range subjects {
		if err != nil {
			break
		}
		s.ticket = logged
		err = m.after(s)
	}
	return logged, err
}

func (m Managed) AskInLog(id, question, actor string) (Ticket, error) {
	if ticket, _, err := m.mayAppend(id, actor); err != nil {
		return ticket, err
	}
	return m.appendLog(id, "question for a manager: "+question, actor, Event{Kind: Asked, Text: question})
}

func (m Managed) mayAppend(id, actor string) (Ticket, Board, error) {
	ticket, board, err := m.ticketOnBoard(id)
	if err == nil && m.role(board, actor) == RoleAgent && !heldBy(ticket, actor) {
		err = RefusedError{Ticket: id, Why: fmt.Sprintf("it is assigned to %q, and an agent appends only to its own ticket's Log", ticket.Assignee)}
	}
	return ticket, board, err
}

func (m Managed) Widen(id string, owns []string, actor string) (Ticket, error) {
	ticket, board, err := m.ticketOnBoard(id)
	if err != nil {
		return ticket, err
	}
	if err := m.Manages(board.Key, actor, "widen owns"); err != nil {
		return ticket, err
	}
	added := slices.DeleteFunc(slices.Clone(owns), func(glob string) bool { return slices.Contains(ticket.Owns, glob) })
	if len(added) == 0 {
		return ticket, fmt.Errorf("%s already owns %s", id, strings.Join(owns, ", "))
	}
	s := m.subjectOf(rules.HookTicketWiden, board.Key, ticket, ticket.Status, actor, 0)
	s.owns = added
	if err := m.gate(&s); err != nil {
		return ticket, err
	}
	widened, err := m.Store.Edit(id, func(t *Ticket) error {
		t.Owns = append(t.Owns, added...)
		return nil
	})
	if err == nil {
		err = m.Store.Record(board.Key, Event{Ticket: id, Kind: Widened, Actor: actor, Text: strings.Join(added, ", ")})
	}
	if err != nil {
		return widened, err
	}
	s.ticket = widened
	return widened, m.after(s)
}

func (m Managed) ticketOnBoard(id string) (Ticket, Board, error) {
	ticket, err := m.Store.Get(id)
	if err != nil {
		return ticket, Board{}, err
	}
	board, err := m.Store.Board(ticket.Board())
	return ticket, board, err
}

func (m Managed) mayMove(role Role, ticket Ticket, to Status, actor string) error {
	if ticket.Status == to {
		return nil
	}
	switch role {
	case RoleManager:
		return nil
	case RolePerson:
		if !to.Live() {
			return nil
		}
		return RefusedError{Ticket: ticket.ID, Why: fmt.Sprintf("%s is not a manager of %s, and %s only closes a ticket", actor, ticket.Board(), RolePerson)}
	case RoleAgent:
		if !heldBy(ticket, actor) {
			return RefusedError{Ticket: ticket.ID, Why: fmt.Sprintf("it is assigned to %q, and an agent moves only its own ticket", ticket.Assignee)}
		}
		if ticket.Status == Doing && to == Review {
			return nil
		}
		if ticket.Status == Doing && to == NeedsDecision {
			if _, _, reported, err := m.NotPossibleOpen(ticket); err != nil || reported {
				return err
			}
		}
		return RefusedError{Ticket: ticket.ID, Why: fmt.Sprintf("an agent moves its own ticket from %s to %s, or to %s by reporting it not possible, and only a manager moves it from %s to %s", Doing, Review, NeedsDecision, ticket.Status, to)}
	}
	panic("boardy: unknown role " + string(role))
}

func (m Managed) validMove(ticket Ticket, to Status) error {
	if to == Doing || (!ticket.Status.Live() && to.Live()) {
		return m.referencesResolve(ticket)
	}
	return nil
}

func (m Managed) doneGate(owns []string) DoneGate {
	if m.head == nil {
		return checkDone(m.waits(), m.Store.Root, owns)
	}
	return m.head.owning(m.waits(), m.Store.Root, owns)
}

func (m Managed) referencesResolve(ticket Ticket) error {
	for _, id := range slices.Concat(ticket.Depends, ticket.Blocks, ticket.Relates, ticket.Duplicates) {
		if _, err := m.Store.Get(id); err != nil {
			return RefusedError{Ticket: ticket.ID, Why: fmt.Sprintf("it names %s, which does not resolve: %v", id, err)}
		}
	}
	return nil
}

func (m Managed) ownsFree(key string, ticket Front, owns []string, status Status, replaces string) error {
	id := ticket.ID
	for _, glob := range owns {
		if err := unreadableOwns(glob); err != nil {
			return RefusedError{Ticket: id, Why: err.Error()}
		}
	}
	if ticket.Parent != "" {
		parent, err := m.Store.Get(ticket.Parent)
		if err != nil {
			return err
		}
		if len(owns) > 0 && len(parent.Owns) == 0 {
			return RefusedError{Ticket: id, Why: fmt.Sprintf("its parent %s owns no paths, and a parent's owns are the ceiling for its sub-tickets: create it without a parent, or widen %s first", parent.ID, parent.ID)}
		}
		if glob := outsideOwns(owns, parent.Owns); glob != "" {
			return RefusedError{Ticket: id, Why: fmt.Sprintf("%s is outside the owns of its parent %s (%s), and a parent's owns are the ceiling for its sub-tickets", glob, parent.ID, strings.Join(parent.Owns, ", "))}
		}
	}
	boards, err := m.Store.Boards()
	if err != nil {
		return err
	}
	if len(owns) == 0 || !slices.ContainsFunc(boards, func(b Board) bool { return b.Key == key && b.Holding(status) }) {
		return nil
	}
	for _, board := range boards {
		fronts, err := m.Store.Fronts(board.Key)
		if err != nil {
			return err
		}
		for _, held := range fronts {
			if held.ID == id || held.ID == replaces || family(ticket, held) || !board.Holding(held.Status) {
				continue
			}
			if glob, other, found := ownsMeet(owns, held.Owns); found {
				return RefusedError{Ticket: id, Why: fmt.Sprintf("%s overlaps %s, held by %s (%s)", glob, other, held.ID, held.Status)}
			}
		}
	}
	return nil
}

const (
	feasibilityRef     = "ticket_feasibility@1"
	feasibilityDecides = "platform"
	doneReasonRef      = "done_reason@1"
	doneReasonDecides  = "open"
	doneReasonPart     = "part"
	reasonPartPrefix   = "part_"
)

var reasonBreak = fs.LazyRegexp(`[,;!?\n]|\.(?:\s|$)`)

func openPart(answers map[string]core.Answer, reason string, parts []string, threshold float64) (string, bool) {
	if answers[doneReasonDecides].Noul < threshold {
		return "", false
	}
	if at, err := strconv.Atoi(strings.TrimPrefix(answers[doneReasonPart].Choice, reasonPartPrefix)); err == nil && at >= 1 && at <= len(parts) {
		return parts[at-1], true
	}
	return reason, true
}

func reasonParts(reason string) []string {
	var parts []string
	for _, part := range reasonBreak().Split(reason, -1) {
		if part = strings.TrimSpace(part); part != "" {
			parts = append(parts, part)
		}
	}
	return parts
}

func (m Managed) Redraft(id, field, body, actor string) (Ticket, error) {
	at, err := draftedSection(field, body)
	if err != nil {
		return Ticket{}, err
	}
	return m.revise(id, "set "+new(Ticket).Drafted()[at].Heading, actor, func(t *Ticket) error {
		*t.Drafted()[at].Body = strings.TrimSpace(body)
		return nil
	})
}

func draftedSection(field, body string) (int, error) {
	sections := new(Ticket).Drafted()
	at := slices.IndexFunc(sections, func(d Drafted) bool { return d.Field == field })
	switch {
	case at < 0:
		return at, fmt.Errorf("%q is not a section a ticket is drafted with", field)
	case strings.TrimSpace(body) == "":
		return at, fmt.Errorf("say what %s holds", sections[at].Heading)
	}
	return at, nil
}

func (m Managed) heldForPerson(s subject, ref, unread string) error {
	subjectName := cmp.Or(s.ticket.ID, strconv.Quote(s.ticket.Title))
	if m.actorKind(s.actor) != rules.ActorPerson {
		return WaitingError{Ticket: subjectName, Rule: ref, Detail: "Jev could not answer, so the person decides: " + unread}
	}
	return m.Store.Record(s.key, Event{Ticket: s.ticket.ID, Kind: RuleFired, Actor: s.actor, To: s.to, Text: hookText(ref, string(rules.ThenNudge), s.event, subjectName+" went ahead unjudged: "+unread)})
}

type ruling struct {
	answers   map[string]core.Answer
	threshold float64
	unread    string
}

func (m Managed) judged(ref, decides string, state any, options []core.Option) ruling {
	name, _, _ := strings.Cut(ref, "@")
	body, err := json.Marshal(state)
	row := ledger.Row{Point: name, Questions: name, StateHash: ledger.HashOf(body), StateBuilder: name + "_state@1", State: body,
		Verdict: ledger.VerdictAsk, Reason: &ledger.Reason{Question: decides, Mode: ledger.ModeEnforced}}
	var ruled ruling
	var set question.Set
	var asked []core.Question
	var decision core.Decision
	if err == nil {
		set, asked, ruled.threshold, err = m.library(ref, decides, options)
	}
	if err == nil {
		row.Version, row.Reason.Threshold, row.Reason.Comparison = set.QuestionsVersion, ruled.threshold, decides+"_at"
		var client *core.Client
		if client, err = classifiers.Open(m.Store.Root, core.ReadKeyring(), 1); err == nil {
			decision, err = client.Ask(m.waits(), core.Request{State: json.RawMessage(body), Questions: asked})
		}
	}
	if err != nil {
		cause := err.Error()
		row.Reason.ModeReason, ruled.unread = &cause, cause
		if core.IsUnbound(err) {
			ruled.unread = "no classifier is bound, so Jev did not answer: tofu models bind classifier <source/model>"
		}
	} else {
		decision.Record(&row, asked, set.QuestionsVersion)
		ruled.answers, row.Reason.Value, row.Verdict = decision.Answers, decision.Answers[decides].Noul, ledger.VerdictAllow
		if row.Reason.Value >= ruled.threshold {
			row.Verdict = ledger.VerdictDeny
		}
	}
	places, err := project.ResolvePlaces(m.Store.Root)
	if err == nil {
		_, err = ledger.NewWriter(places.Log).Append(row)
	}
	if err != nil {
		ruled.unread = "the decision did not reach the ledger: " + err.Error()
	}
	return ruled
}

func (m Managed) library(ref, decides string, options []core.Option) (question.Set, []core.Question, float64, error) {
	layers, err := question.Layers(questions.Files(), m.Store.Root)
	if err != nil {
		return question.Set{}, nil, 0, err
	}
	set, fields, err := question.Resolve(ref, layers)
	if err != nil {
		return set, nil, 0, err
	}
	at := slices.IndexFunc(fields, func(f question.Field) bool { return f.Path == "thresholds."+decides+"_at" })
	var threshold float64
	if at >= 0 {
		threshold, err = strconv.ParseFloat(fields[at].Value, 64)
	}
	if at < 0 || err != nil || threshold <= 0 || threshold > 1 {
		return set, nil, 0, fmt.Errorf("%s declares no thresholds.%s_at above 0 and at or below 1", ref, decides)
	}
	asked := make([]core.Question, len(set.Questions))
	for i, q := range set.Questions {
		asked[i] = q.ToJev()
		if asked[i].Kind == core.QuestionChoice {
			asked[i].Options = append(asked[i].Options, options...)
		}
	}
	return set, asked, threshold, nil
}

func (s Store) logLine(actor, text string) string {
	return "- " + time.Now().Format(time.DateOnly) + " " + s.authorOf(actor).ID + ": " + text
}
