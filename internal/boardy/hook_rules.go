package boardy

import (
	"cmp"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
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
	ID          string         `json:"id"`
	Scope       HookScope      `json:"scope"`
	File        string         `json:"file"`
	When        rule.When      `json:"when"`
	On          rule.HookEvent `json:"on"`
	Trigger     string         `json:"trigger"`
	Do          rule.Do        `json:"do"`
	Arg         string         `json:"arg,omitempty"`
	Then        rule.Then      `json:"then,omitempty"`
	Text        string         `json:"text,omitempty"`
	Name        string         `json:"name,omitempty"`
	Description string         `json:"description,omitempty"`
	Bypass      string         `json:"bypass,omitempty"`
	ShadowedBy  HookScope      `json:"shadowed_by,omitempty"`
	Bypassed    int            `json:"bypassed,omitempty"`
	LastFired   time.Time      `json:"last_fired,omitzero"`
	hook        rule.Hook
}

func actorNames(actors []rule.HookActor) []string {
	names := make([]string, len(actors))
	for i, actor := range actors {
		names[i] = string(actor)
	}
	return names
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
	add("from", c.From...)
	add("to", c.To...)
	add("type", c.Type...)
	add("priority", c.Priority...)
	add("labels", c.Labels...)
	add("not_labels", c.NotLabels...)
	add("actor", actorNames(c.Actor)...)
	add("epic", c.Epic)
	add("sprint", c.Sprint)
	add("owns", c.Owns)
	if c.InStatusFor > 0 {
		add("in_status_for", shortDuration(c.InStatusFor))
	}
	add("created_before", shortMoment(c.CreatedBefore))
	add("created_after", shortMoment(c.CreatedAfter))
	if said != nil {
		words = append(words, "if "+strings.Join(said, ", "))
	}
	row := HookRow{ID: h.ID, Scope: scope, File: h.File, When: h.When, On: h.On, Trigger: strings.Join(words, " "), Do: h.Action.Do, Arg: h.Action.Arg, Then: h.Then, Text: h.Text,
		Name: h.Name, Description: h.Description, hook: h}
	if b := h.Bypass; b != nil {
		row.Bypass = fmt.Sprintf("who %s, accept %s, wait %s", strings.Join(actorNames(b.Who), ","), b.Accept, b.Wait)
	}
	return row
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

func shortMoment(at time.Time) string {
	switch {
	case at.IsZero():
		return ""
	case at.Equal(at.Truncate(24 * time.Hour)):
		return at.Format(time.DateOnly)
	}
	return at.Format(time.RFC3339)
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
	switch _, err := store.Board(key); {
	case errors.Is(err, fs.ErrNotExist):
		return RulesAnswer{}, fmt.Errorf("no board %s in this project: tofu boardy boards lists them", key)
	case err != nil:
		return RulesAnswer{}, err
	}
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
	won := map[string]HookScope{}
	flagged, err := a.Board.flaggedHooks(key, time.Now())
	if err != nil {
		return answer, err
	}
	for _, scope := range hookScopes() {
		hooks, broken, err := store.scopeHooks(key, scope)
		if err != nil {
			answer.Invalid = append(answer.Invalid, strings.Split(err.Error(), "\n")...)
		}
		var on []string
		for i, h := range broken {
			on = append(on, string(h.On))
			if i+1 < len(broken) && broken[i+1].File == h.File {
				continue
			}
			if len(on) == len(rule.HookEvents()) {
				on = []string{"every event"}
			}
			answer.Invalid = append(answer.Invalid, strings.ReplaceAll(h.Text, "\n", "; ")+" (refuses "+strings.Join(on, ", ")+" until it is fixed)")
			on = nil
		}
		for _, h := range hooks {
			row := hookRow(h, scope)
			if first, shadowed := won[h.ID]; shadowed {
				row.ShadowedBy = first
			} else {
				won[h.ID] = scope
				if at := slices.IndexFunc(flagged, func(f FlaggedHook) bool { return f.Hook == h.ID }); at >= 0 {
					row.Bypassed = flagged[at].Times
				}
			}
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
	Board     string   `json:"board"`
	Hook      HookRow  `json:"hook"`
	Source    string   `json:"source"`
	TakesOver *HookRow `json:"takes_over,omitempty"`
	Words     Words    `json:"words"`
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
	switch {
	case err == nil && file == dir:
		err = fmt.Errorf("%s: hook id %q names its file, so it is one plain name", dir, h.ID)
	case err == nil && h.Action.Do == rule.DoCheck:
		if _, _, unknown := a.Board.namedCheck(h); unknown != nil {
			err = fmt.Errorf("%s: %w", file, unknown)
		}
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
	if err == nil {
		err = a.weakens(key, scope, h)
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

func (a API) weakens(key string, scope HookScope, h rule.Hook) error {
	kind := a.Board.actorKind(a.Actor)
	if kind == rule.ActorPerson {
		return nil
	}
	listed, err := a.Rules(RulesRequest{Board: key})
	if err != nil {
		return err
	}
	strictness := func(then rule.Then) int { return slices.Index([]rule.Then{rule.ThenAsk, rule.ThenRefuse}, then) }
	for _, row := range listed.Hooks {
		was := row.hook
		if row.ID != h.ID || strictness(was.Then) < 0 {
			continue
		}
		how := "shadows"
		if row.Scope == scope {
			how = "weakens"
		}
		shadows := slices.Index(hookScopes(), scope) < slices.Index(hookScopes(), row.Scope)
		weaker := row.Scope == scope && (strictness(h.Then) < strictness(was.Then) || was.Bypass == nil && h.Bypass != nil)
		if shadows || weaker {
			return WaitingError{Ticket: "hook " + h.ID, Rule: h.ID, Detail: fmt.Sprintf("%s acts as %s, and this %s hook %s the %s hook that would %s, so the person writes it", a.Actor, kind, scope, how, row.Scope, was.Then)}
		}
	}
	return nil
}

func (a API) RuleRemove(request RuleRequest) (RuleAnswer, error) {
	answer, err := a.RuleGet(request)
	if err == nil {
		err = a.Board.Manages(answer.Board, a.Actor, "remove a board hook")
	}
	if err == nil {
		err = errors.Join(os.Remove(answer.Hook.File), a.hookChanged(answer.Board, "removed", answer.Hook))
	}
	if next, missing := a.RuleGet(RuleRequest{Board: answer.Board, ID: request.ID}); err == nil && missing == nil {
		answer.TakesOver = &next.Hook
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
