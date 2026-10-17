package boardy

import (
	"cmp"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"tofu/internal/rule"
)

const (
	HookChanged EventKind = "hook"
	HookFailed  EventKind = "hook-failed"
)

type HookScope string

const (
	ScopeBoard   HookScope = "board"
	ScopeProject HookScope = "project"
	ScopeUser    HookScope = "user"
)

func hookScopes() []HookScope { return []HookScope{ScopeBoard, ScopeProject, ScopeUser} }

func (HookScope) Enum() []string { return enum(hookScopes()...) }

type HookRow struct {
	ID        string         `json:"id"`
	Scope     HookScope      `json:"scope"`
	File      string         `json:"file"`
	When      rule.When      `json:"when"`
	On        rule.HookEvent `json:"on"`
	Trigger   string         `json:"trigger"`
	Do        rule.Do        `json:"do"`
	Arg       string         `json:"arg,omitempty"`
	Then      rule.Then      `json:"then,omitempty"`
	Text      string         `json:"text,omitempty"`
	LastFired time.Time      `json:"last_fired,omitzero"`
	hook      rule.Hook
}

func hookRow(h rule.Hook, scope HookScope) HookRow {
	words := []string{string(h.When), string(h.On)}
	switch {
	case h.Every > 0:
		words = append(words, "every "+shortDuration(h.Every))
	case h.Daily != "":
		words = append(words, "daily "+h.Daily)
	}
	c := h.If
	var said []string
	add := func(key string, values ...string) {
		if joined := strings.Join(values, ","); joined != "" {
			said = append(said, key+" "+joined)
		}
	}
	actors := make([]string, len(c.Actor))
	for i, actor := range c.Actor {
		actors[i] = string(actor)
	}
	add("from", c.From...)
	add("to", c.To...)
	add("type", c.Type...)
	add("priority", c.Priority...)
	add("labels", c.Labels...)
	add("actor", actors...)
	add("epic", c.Epic)
	add("sprint", c.Sprint)
	add("owns", c.Owns)
	if c.InStatusFor > 0 {
		add("in_status_for", shortDuration(c.InStatusFor))
	}
	if said != nil {
		words = append(words, "if "+strings.Join(said, ", "))
	}
	return HookRow{ID: h.ID, Scope: scope, File: h.File, When: h.When, On: h.On, Trigger: strings.Join(words, " "), Do: h.Action.Do, Arg: h.Action.Arg, Then: h.Then, Text: h.Text, hook: h}
}

func shortDuration(d time.Duration) string {
	text := d.String()
	if strings.HasSuffix(text, "m0s") {
		text = strings.TrimSuffix(text, "0s")
	}
	if strings.HasSuffix(text, "h0m") {
		text = strings.TrimSuffix(text, "0m")
	}
	return text
}

type RulesRequest struct {
	Board string `json:"board,omitempty"`
}

type RulesAnswer struct {
	Board   string    `json:"board"`
	Hooks   []HookRow `json:"hooks"`
	Invalid []string  `json:"invalid"`
	Words   Words     `json:"words"`
}

func (a API) Rules(request RulesRequest) (RulesAnswer, error) {
	key, err := a.boardKey(request.Board)
	if err != nil {
		return RulesAnswer{}, err
	}
	store := a.Board.Store
	events, eventsErr := store.Events(key, 0)
	ran, tickedErr := ticked(filepath.Join(store.BoardDir(key), tickedFileName))
	fired := map[string]time.Time{}
	for _, event := range events {
		if event.Kind == RuleFired || event.Kind == Waiting {
			id, _, _ := strings.Cut(event.Text, " (")
			fired[id] = event.At
		}
	}
	answer := RulesAnswer{Board: key, Hooks: []HookRow{}, Invalid: []string{}, Words: a.Words}
	for _, scope := range hookScopes() {
		hooks, err := store.scopeHooks(key, scope)
		if err != nil {
			answer.Invalid = append(answer.Invalid, strings.Split(err.Error(), "\n")...)
		}
		for _, h := range hooks {
			row := hookRow(h, scope)
			row.LastFired = fired[h.ID]
			if ran[h.File].After(row.LastFired) {
				row.LastFired = ran[h.File].UTC().Truncate(time.Second)
			}
			answer.Hooks = append(answer.Hooks, row)
		}
	}
	return answer, errors.Join(eventsErr, tickedErr)
}

type RuleRequest struct {
	Board string    `json:"board,omitempty"`
	Scope HookScope `json:"scope,omitempty"`
	ID    string    `json:"id"`
}

type RuleAnswer struct {
	Board  string  `json:"board"`
	Hook   HookRow `json:"hook"`
	Source string  `json:"source"`
	Words  Words   `json:"words"`
}

func (a API) RuleGet(request RuleRequest) (RuleAnswer, error) {
	listed, err := a.Rules(RulesRequest{Board: request.Board})
	if err != nil {
		return RuleAnswer{}, err
	}
	for _, row := range listed.Hooks {
		if row.ID == request.ID && (request.Scope == "" || request.Scope == row.Scope) {
			source, err := os.ReadFile(row.File)
			return RuleAnswer{Board: listed.Board, Hook: row, Source: string(source), Words: a.Words}, err
		}
	}
	return RuleAnswer{}, fmt.Errorf("%s has no hook %q in scope %s", listed.Board, request.ID, cmp.Or(string(request.Scope), "board, project or user"))
}

type RulePutRequest struct {
	Board  string    `json:"board,omitempty"`
	Scope  HookScope `json:"scope,omitempty"`
	Source string    `json:"source"`
}

func (a API) parseHook(board string, scope HookScope, source string) (string, rule.Hook, error) {
	key, err := a.boardKey(board)
	if err != nil {
		return "", rule.Hook{}, err
	}
	dir, err := a.Board.Store.hookDir(key, scope)
	if err != nil {
		return "", rule.Hook{}, err
	}
	file := dir
	for line := range strings.Lines(source) {
		name, value, _ := strings.Cut(strings.TrimSpace(line), ":")
		if id := strings.Trim(strings.TrimSpace(value), `"`); strings.TrimSpace(name) == "id" && id != "" && !strings.ContainsAny(id, `/\`) && filepath.IsLocal(id) {
			file = filepath.Join(dir, id+".yaml")
		}
	}
	h, err := rule.ParseHook([]byte(source), file)
	if err == nil && file == dir {
		err = fmt.Errorf("%s: hook id %q names its file, so it is one plain name", dir, h.ID)
	}
	if err != nil {
		return "", rule.Hook{}, err
	}
	return key, h, validHook(h)
}

func (a API) RulePut(request RulePutRequest) (RuleAnswer, error) {
	scope := cmp.Or(request.Scope, ScopeBoard)
	key, h, err := a.parseHook(request.Board, scope, request.Source)
	if err == nil {
		err = a.Board.Manages(key, a.Actor, "change a board hook")
	}
	if err != nil {
		return RuleAnswer{}, err
	}
	verb := "added"
	if existing, err := a.RuleGet(RuleRequest{Board: key, Scope: scope, ID: h.ID}); err == nil {
		h.File, verb = existing.Hook.File, "changed"
	}
	if err := os.MkdirAll(filepath.Dir(h.File), 0o755); err != nil {
		return RuleAnswer{}, err
	}
	if err := writeAtomic(h.File, []byte(strings.TrimSpace(request.Source)+"\n")); err != nil {
		return RuleAnswer{}, err
	}
	answer, err := a.RuleGet(RuleRequest{Board: key, Scope: scope, ID: h.ID})
	return answer, errors.Join(err, a.hookChanged(key, verb, answer.Hook))
}

func (a API) RuleRemove(request RuleRequest) (RuleAnswer, error) {
	answer, err := a.RuleGet(request)
	if err == nil {
		err = a.Board.Manages(answer.Board, a.Actor, "remove a board hook")
	}
	if err == nil {
		err = errors.Join(os.Remove(answer.Hook.File), a.hookChanged(answer.Board, "removed", answer.Hook))
	}
	return answer, err
}

func (a API) hookChanged(key, verb string, row HookRow) error {
	return a.Board.Store.Record(key, Event{Kind: HookChanged, Actor: a.Actor, Text: fmt.Sprintf("%s hook %s (%s scope): %s", verb, row.ID, row.Scope, row.File)})
}

type RuleTestRequest struct {
	Board  string    `json:"board,omitempty"`
	Scope  HookScope `json:"scope,omitempty"`
	ID     string    `json:"id,omitempty"`
	Source string    `json:"source,omitempty"`
	Ticket string    `json:"ticket,omitempty"`
	To     Status    `json:"to,omitempty"`
}

type RuleTestAnswer struct {
	Hook    HookRow   `json:"hook"`
	Ticket  string    `json:"ticket,omitempty"`
	To      Status    `json:"to,omitempty"`
	Holds   bool      `json:"holds"`
	Fails   bool      `json:"fails"`
	Blocks  bool      `json:"blocks"`
	Then    rule.Then `json:"then,omitempty"`
	Verdict string    `json:"verdict"`
	Detail  string    `json:"detail,omitempty"`
	Words   Words     `json:"words"`
}

func (a API) RuleTest(request RuleTestRequest) (RuleTestAnswer, error) {
	var key string
	var row HookRow
	if request.Source != "" {
		scope := cmp.Or(request.Scope, ScopeBoard)
		parsedKey, h, err := a.parseHook(request.Board, scope, request.Source)
		if err != nil {
			return RuleTestAnswer{}, err
		}
		key, row = parsedKey, hookRow(h, scope)
	} else {
		got, err := a.RuleGet(RuleRequest{Board: request.Board, Scope: request.Scope, ID: request.ID})
		if err != nil {
			return RuleTestAnswer{}, err
		}
		key, row = got.Board, got.Hook
	}
	h, m := row.hook, a.Board
	s := subject{event: h.On, key: key, actor: a.Actor, entered: time.Now()}
	if request.Ticket != "" {
		ticket, board, err := m.ticketOnBoard(request.Ticket)
		if err != nil {
			return RuleTestAnswer{}, err
		}
		to := ticket.Status
		if (h.On == rule.HookTicketMove || h.On == rule.HookTicketClose) && len(h.If.To) > 0 {
			to = Status(h.If.To[0])
		}
		if request.To != "" {
			if to, err = ParseStatus(string(request.To)); err != nil {
				return RuleTestAnswer{}, err
			}
		}
		s = m.subjectOf(h.On, board.Key, ticket, to, a.Actor, 0)
		s.entered = m.entered(board.Key, ticket)
		if h.On == rule.HookSchedule {
			s.from = ""
		}
	}
	answer := RuleTestAnswer{Hook: row, Ticket: s.ticket.ID, To: s.to, Holds: m.holds(h, s), Words: a.Words}
	switch {
	case !answer.Holds:
		answer.Verdict = "its conditions do not hold, so it does not run"
	case h.Action.Do.Check():
		t, err := m.try(h, s)
		if err != nil {
			return answer, err
		}
		answer.Fails, answer.Blocks, answer.Verdict = t.failed, t.blocked, "the check passes"
		if t.failed {
			answer.Then, answer.Detail, answer.Verdict = t.then, t.detail, "the check fails, so it would "+string(t.then)
		}
	default:
		answer.Verdict = "it would " + string(h.Action.Do) + " " + s.expand(h.Action.Arg)
	}
	return answer, nil
}
