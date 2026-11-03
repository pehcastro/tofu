package boardy

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"tofu/internal/konst"
	"tofu/internal/librarian/rules"
)

const (
	bypassFileName = "bypasses.json"
	bypassLockName = "bypasses"
)

const (
	thenBypassed       rule.Then = "bypassed"
	thenBypassReview   rule.Then = "bypass-review"
	thenBypassAsked    rule.Then = "bypass-asked"
	thenBypassAccepted rule.Then = "bypass-accepted"
	thenBypassAnswered rule.Then = "bypass-answered"
)

type BypassState string

const (
	BypassOpen    BypassState = "open"
	BypassReview  BypassState = "review"
	BypassYes     BypassState = "yes"
	BypassNo      BypassState = "no"
	BypassExpired BypassState = "expired"
)

type BypassRequest struct {
	ID         string         `json:"id"`
	Ticket     string         `json:"ticket"`
	Hook       string         `json:"hook"`
	HookSum    string         `json:"hook_sum"`
	Event      rule.HookEvent `json:"event"`
	From       Status         `json:"from"`
	To         Status         `json:"to"`
	Why        string         `json:"why"`
	By         string         `json:"by"`
	ByKind     rule.HookActor `json:"by_kind"`
	Accept     rule.Accept    `json:"accept"`
	At         time.Time      `json:"at"`
	State      BypassState    `json:"state"`
	AnsweredBy string         `json:"answered_by,omitempty"`
	AnsweredAt time.Time      `json:"answered_at,omitzero"`
	Answer     string         `json:"answer,omitempty"`
	Applied    bool           `json:"applied,omitempty"`
}

type FlaggedHook struct {
	Hook  string `json:"hook"`
	Times int    `json:"times"`
}

type bypassBook struct {
	Last     int             `json:"last"`
	Requests []BypassRequest `json:"requests"`
}

type bypassRefused struct{ why string }

func (e bypassRefused) Error() string { return "bypass refused: " + e.why }

func SubAgentActor(actor string) bool { return strings.Contains(actor, subAgentOfSession) }

func (m Managed) bypassable(s subject) bool {
	return m.Bypass.Hook != "" && s.depth == 0 && (s.event == rule.HookTicketMove || s.event == rule.HookTicketClose)
}

func (m Managed) vetBypass(hooks []rule.Hook, s subject) error {
	if !m.bypassable(s) {
		return nil
	}
	held, _ := m.holding(hooks, rule.WhenBefore, s)
	at := slices.IndexFunc(held, func(h rule.Hook) bool { return h.ID == m.Bypass.Hook && h.Action.Do != doBroken })
	var why string
	switch {
	case at < 0:
		why = fmt.Sprintf("no hook %s runs before this %s of %s", m.Bypass.Hook, s.event, s.ticket.ID)
	case !held[at].Action.Do.Check() || held[at].Then != rule.ThenRefuse && held[at].Then != rule.ThenAsk:
		why = fmt.Sprintf("hook %s does not refuse or ask, so there is nothing to bypass", m.Bypass.Hook)
	case held[at].Bypass == nil || !slices.Contains(held[at].Bypass.Who, m.actorKind(s.actor)):
		why = "hook " + m.Bypass.Hook + " is " + m.bypassHint(held[at], s)
	case strings.TrimSpace(m.Bypass.Why) == "":
		why = "a bypass says why: --why <reason>"
	case utf8.RuneCountInString(m.Bypass.Why) > konst.BoardBypassWhyRunesMost:
		why = fmt.Sprintf("--why runs to %d characters, over the %d a reason holds", utf8.RuneCountInString(m.Bypass.Why), konst.BoardBypassWhyRunesMost)
	default:
		return nil
	}
	return bypassRefused{why}
}

func (m Managed) bypassHint(h rule.Hook, s subject) string {
	b := h.Bypass
	switch {
	case b == nil:
		return "not bypassable"
	case s.event != rule.HookTicketMove && s.event != rule.HookTicketClose:
		return "a bypass is asked on a move, and this is " + string(s.event)
	case !slices.Contains(b.Who, m.actorKind(s.actor)):
		return fmt.Sprintf("bypassable by %s; you act as %s, so it is not yours to bypass", strings.Join(actorNames(b.Who), ", "), m.actorKind(s.actor))
	}
	return fmt.Sprintf("bypass: tofu boardy move %s %s --bypass %s --why <reason>", s.ticket.ID, s.to, h.ID)
}

func (m Managed) bypass(h rule.Hook, s subject) ([]Fired, error) {
	b, why := h.Bypass, m.Bypass.Why
	accept, routed := m.acceptor(b.Accept, s)
	switch {
	case b.Accept == rule.AcceptNone:
		return []Fired{firing(h, s, rule.ModeShadow, false, thenBypassed, "accept none, so it passes: "+why)}, nil
	case b.Wait == rule.WaitAfter:
		return []Fired{firing(h, s, rule.ModeShadow, false, thenBypassReview, fmt.Sprintf("it passes, and the %s reviews it after%s: %s", accept, routed, why))}, nil
	}
	book, _ := m.Store.bypassBook(s.key)
	at := slices.IndexFunc(book.Requests, func(r BypassRequest) bool { return r.holds(h, s) })
	if at < 0 {
		detail := fmt.Sprintf("the bypass waits for the %s to accept it%s: %s", accept, routed, why)
		return []Fired{firing(h, s, rule.ModeShadow, false, thenBypassAsked, detail)}, WaitingError{Ticket: s.ticket.ID, Rule: h.ID, Detail: detail, For: string(accept)}
	}
	req := book.Requests[at]
	if req.State == BypassYes {
		return []Fired{firing(h, s, rule.ModeShadow, false, thenBypassAccepted, fmt.Sprintf("%s accepted by %s: %s", req.ID, req.AnsweredBy, req.Why))}, nil
	}
	detail := fmt.Sprintf("%s was asked at %s and still waits for the %s: tofu boardy bypass answer %s yes|no", req.ID, req.At.Local().Format(time.DateTime), req.Accept, req.ID)
	return nil, WaitingError{Ticket: s.ticket.ID, Rule: h.ID, Detail: detail, For: string(req.Accept)}
}

func (r BypassRequest) Line() string {
	head := fmt.Sprintf("%s %s %s to %s past %s, asked by %s at %s: %s", r.ID, r.Ticket, r.From, r.To, r.Hook, r.By, r.At.Local().Format(time.DateTime), r.Why)
	answer := fmt.Sprintf("tofu boardy bypass answer %s yes|no", r.ID)
	switch r.State {
	case BypassOpen:
		return fmt.Sprintf("%s (waits for the %s: %s)", head, r.Accept, answer)
	case BypassReview:
		return fmt.Sprintf("%s (it moved, and the %s reviews it: %s)", head, r.Accept, answer)
	case BypassExpired:
		return head + " (expired with no answer)"
	}
	return fmt.Sprintf("%s (answered %s by %s)", head, r.State, r.AnsweredBy)
}

func (r BypassRequest) holds(h rule.Hook, s subject) bool {
	same := r.Ticket == s.ticket.ID && r.Hook == h.ID && r.From == s.from && r.To == s.to
	return same && (r.State == BypassOpen || r.State == BypassYes && !r.Applied && r.HookSum == hookSum(h.File))
}

func (m Managed) acceptor(accept rule.Accept, s subject) (rule.Accept, string) {
	board, _ := m.Store.Board(s.key)
	switch {
	case accept == rule.AcceptJev:
		return rule.AcceptPerson, ", since no classifier answers a bypass yet"
	case accept == rule.AcceptLead && m.actorKind(s.actor) == rule.ActorLead, accept == rule.AcceptManager && m.actorKind(s.actor) != rule.ActorPerson && m.role(board, s.actor) == RoleManager:
		return rule.AcceptPerson, fmt.Sprintf(", since the %s asked it and it goes up", accept)
	}
	return accept, ""
}

func (m Managed) settleBypass(hooks []rule.Hook, s subject, fired []Fired, err error) error {
	at := slices.IndexFunc(fired, func(f Fired) bool {
		return f.RuleID == m.Bypass.Hook && slices.Contains([]rule.Then{thenBypassed, thenBypassReview, thenBypassAsked, thenBypassAccepted}, f.Then)
	})
	var waiting WaitingError
	held := errors.As(err, &waiting) && waiting.Rule == m.Bypass.Hook
	switch {
	case at < 0 && (held || errors.As(err, new(RefusedError))):
		return err
	case at < 0:
		return bypassRefused{fmt.Sprintf("hook %s passes this %s, so there is nothing to bypass", m.Bypass.Hook, s.event)}
	}
	h := hooks[slices.IndexFunc(hooks, func(h rule.Hook) bool { return h.ID == m.Bypass.Hook && h.Action.Do != doBroken })]
	switch fired[at].Then {
	case thenBypassAccepted:
		return errors.Join(err, m.applyBypass(s, h))
	case thenBypassAsked, thenBypassReview:
		state := map[rule.Then]BypassState{thenBypassAsked: BypassOpen, thenBypassReview: BypassReview}[fired[at].Then]
		id, fileErr := m.fileBypass(s, h, state)
		if fileErr != nil {
			return errors.Join(err, fileErr)
		}
		if held {
			waiting.Detail += fmt.Sprintf(" (%s: tofu boardy bypass answer %s yes|no)", id, id)
			return waiting
		}
	}
	return err
}

func (m Managed) fileBypass(s subject, h rule.Hook, state BypassState) (string, error) {
	accept, _ := m.acceptor(h.Bypass.Accept, s)
	var id string
	err := m.editBypasses(s.key, func(book *bypassBook) error {
		if at := slices.IndexFunc(book.Requests, func(r BypassRequest) bool { return r.State == BypassOpen && r.holds(h, s) }); at >= 0 && state == BypassOpen {
			id = book.Requests[at].ID
			return nil
		}
		book.Last++
		id = "B" + strconv.Itoa(book.Last)
		book.Requests = append(book.Requests, BypassRequest{ID: id, Ticket: s.ticket.ID, Hook: h.ID, HookSum: hookSum(h.File), Event: s.event, From: s.from, To: s.to,
			Why: m.Bypass.Why, By: s.actor, ByKind: m.actorKind(s.actor), Accept: accept, At: time.Now().UTC().Truncate(time.Second), State: state})
		return nil
	})
	return id, err
}

func (m Managed) applyBypass(s subject, h rule.Hook) error {
	return m.editBypasses(s.key, func(book *bypassBook) error {
		at := slices.IndexFunc(book.Requests, func(r BypassRequest) bool { return r.State == BypassYes && r.holds(h, s) })
		if at < 0 {
			return bypassRefused{fmt.Sprintf("the accepted bypass of %s for %s was already applied", h.ID, s.ticket.ID)}
		}
		book.Requests[at].Applied = true
		return nil
	})
}

func (m Managed) editBypasses(key string, edit func(*bypassBook) error) error {
	var expired []BypassRequest
	err := m.Store.locked(key, bypassLockName, func() error {
		book, err := m.Store.bypassBook(key)
		if err != nil {
			return err
		}
		read, _ := json.Marshal(book)
		for i, req := range book.Requests {
			if req.State == BypassOpen && time.Since(req.At) > konst.BoardBypassWaitHours*time.Hour {
				book.Requests[i].State = BypassExpired
				expired = append(expired, book.Requests[i])
			}
		}
		editErr := edit(&book)
		if editErr != nil && len(expired) == 0 {
			return editErr
		}
		if changed, _ := json.Marshal(book); string(changed) == string(read) {
			return editErr
		}
		data, err := json.MarshalIndent(book, "", "  ")
		if err != nil {
			return err
		}
		return errors.Join(editErr, writeAtomic(filepath.Join(m.Store.BoardDir(key), bypassFileName), append(data, '\n')))
	})
	for _, req := range expired {
		text := fmt.Sprintf("%s expired after %dh with no answer from the %s; ask again: tofu boardy move %s %s --bypass %s --why <reason>", req.ID, konst.BoardBypassWaitHours, req.Accept, req.Ticket, req.To, req.Hook)
		err = errors.Join(err, m.Store.Record(key, Event{Ticket: req.Ticket, Kind: RuleFired, Actor: req.By, To: req.To, Text: hookText(req.Hook, string(rule.ThenNudge), req.Event, text)}))
	}
	return err
}

func (s Store) bypassBook(key string) (bypassBook, error) {
	var book bypassBook
	raw, err := os.ReadFile(filepath.Join(s.BoardDir(key), bypassFileName))
	if errors.Is(err, fs.ErrNotExist) {
		return book, nil
	}
	if err == nil {
		err = json.Unmarshal(raw, &book)
	}
	return book, err
}

func hookSum(file string) string {
	raw, _ := os.ReadFile(file)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func (m Managed) Bypasses(key string) ([]BypassRequest, error) {
	var listed []BypassRequest
	err := m.editBypasses(key, func(book *bypassBook) error {
		listed = slices.Clone(book.Requests)
		return nil
	})
	return listed, err
}

func (m Managed) AnswerBypass(key, id string, yes bool, why, actor string) (BypassRequest, Ticket, error) {
	hooks, err := m.Store.hooks(key)
	if err != nil {
		return BypassRequest{}, Ticket{}, err
	}
	var req BypassRequest
	err = m.editBypasses(key, func(book *bypassBook) error {
		at := slices.IndexFunc(book.Requests, func(r BypassRequest) bool { return r.ID == id })
		if at < 0 {
			return RefusedError{Why: fmt.Sprintf("no bypass %s on %s: tofu boardy bypass list shows them", id, key)}
		}
		req = book.Requests[at]
		if refusal := m.unanswerable(req, hooks, key, actor); refusal != "" {
			return RefusedError{Ticket: req.Ticket, Why: req.ID + ": " + refusal}
		}
		req.State, req.AnsweredBy, req.AnsweredAt, req.Answer = BypassNo, actor, time.Now().UTC().Truncate(time.Second), strings.TrimSpace(why)
		if yes {
			req.State = BypassYes
		}
		req.Applied = req.Applied || book.Requests[at].State == BypassReview
		book.Requests[at] = req
		return nil
	})
	if err != nil {
		return req, Ticket{}, err
	}
	said, then := fmt.Sprintf("%s answered %s by %s", req.ID, req.State, actor), thenBypassAnswered
	if req.Answer != "" {
		said += ": " + req.Answer
	}
	if req.State == BypassNo {
		then = rule.ThenNudge
	}
	err = m.Store.Record(key, Event{Ticket: req.Ticket, Kind: RuleFired, Actor: actor, To: req.To, Text: hookText(req.Hook, string(then), req.Event, said)})
	if err != nil || req.State != BypassYes || req.Applied {
		return req, Ticket{}, err
	}
	asker := m
	asker.Bypass = Bypassing{Hook: req.Hook, Why: req.Why}
	if req.ByKind == rule.ActorLead {
		asker.Lead = req.By
	}
	moved, err := asker.Move(req.Ticket, req.To, fmt.Sprintf("bypass %s of %s accepted by %s", req.ID, req.Hook, actor), req.By)
	req.Applied = err == nil
	return req, moved, err
}

func (m Managed) unanswerable(req BypassRequest, hooks []rule.Hook, key, actor string) string {
	board, _ := m.Store.Board(key)
	kind := m.actorKind(actor)
	ticket, err := m.Store.Get(req.Ticket)
	at := slices.IndexFunc(hooks, func(h rule.Hook) bool { return h.ID == req.Hook && h.Action.Do != doBroken })
	switch {
	case req.State == BypassExpired:
		return fmt.Sprintf("it expired after %dh with no answer; ask again: tofu boardy move %s %s --bypass %s --why <reason>", konst.BoardBypassWaitHours, req.Ticket, req.To, req.Hook)
	case req.State != BypassOpen && req.State != BypassReview:
		return fmt.Sprintf("it was answered %s by %s at %s", req.State, req.AnsweredBy, req.AnsweredAt.Local().Format(time.DateTime))
	case kind != rule.ActorPerson && (actor == req.By || req.Accept == rule.AcceptPerson || req.Accept == rule.AcceptLead && kind != rule.ActorLead || req.Accept == rule.AcceptManager && m.role(board, actor) != RoleManager):
		return fmt.Sprintf("it waits for the %s, asked by %s; you act as %s, so it is not yours to answer", req.Accept, req.By, kind)
	case req.State == BypassReview:
		return ""
	case err != nil:
		return err.Error()
	case ticket.Status != req.From:
		return fmt.Sprintf("%s left %s and is %s now, so the held move to %s no longer applies", req.Ticket, req.From, ticket.Status, req.To)
	case at < 0:
		return fmt.Sprintf("hook %s no longer runs on %s, so there is nothing left to bypass", req.Hook, key)
	case hookSum(hooks[at].File) != req.HookSum:
		return fmt.Sprintf("hook %s changed since the bypass was asked, so the answer would bypass a hook nobody asked about; ask again", req.Hook)
	}
	return ""
}

func (m Managed) flaggedHooks(key string, now time.Time) ([]FlaggedHook, error) {
	events, err := m.Store.Events(key, 0)
	counts := map[string]int{}
	since := now.Add(-konst.BoardBypassFlagDays * 24 * time.Hour)
	for _, event := range events {
		for _, then := range []rule.Then{thenBypassed, thenBypassReview, thenBypassAccepted} {
			if hook, _, found := strings.Cut(event.Text, " ("+string(then)+", "); found && event.Kind == RuleFired && event.At.After(since) {
				counts[hook]++
			}
		}
	}
	var flagged []FlaggedHook
	for hook, times := range counts {
		if times >= konst.BoardBypassFlagCount {
			flagged = append(flagged, FlaggedHook{Hook: hook, Times: times})
		}
	}
	slices.SortFunc(flagged, func(a, b FlaggedHook) int { return strings.Compare(a.Hook, b.Hook) })
	return flagged, err
}

type BypassListRequest struct {
	Board string `json:"board,omitempty"`
}

type BypassListAnswer struct {
	Board    string          `json:"board"`
	Bypasses []BypassRequest `json:"bypasses"`
	Flagged  []FlaggedHook   `json:"flagged"`
}

func (a API) BypassList(request BypassListRequest) (BypassListAnswer, error) {
	key, err := a.boardKey(request.Board)
	if err != nil {
		return BypassListAnswer{}, err
	}
	answer := BypassListAnswer{Board: key}
	answer.Bypasses, err = a.Board.Bypasses(key)
	if err != nil {
		return answer, err
	}
	answer.Flagged, err = a.Board.flaggedHooks(key, time.Now())
	return answer, err
}

type BypassAnswerRequest struct {
	Board string `json:"board,omitempty"`
	ID    string `json:"id"`
	Yes   bool   `json:"yes"`
	Why   string `json:"why,omitempty"`
}

type BypassAnswerAnswer struct {
	Bypass BypassRequest `json:"bypass"`
	Moved  *Ticket       `json:"moved,omitempty"`
}

func (a API) BypassAnswer(request BypassAnswerRequest) (BypassAnswerAnswer, error) {
	key, err := a.boardKey(request.Board)
	if err != nil {
		return BypassAnswerAnswer{}, err
	}
	req, moved, err := a.Board.AnswerBypass(key, request.ID, request.Yes, request.Why, a.Actor)
	answer := BypassAnswerAnswer{Bypass: req}
	if moved.ID != "" {
		answer.Moved = &moved
	}
	return answer, err
}
