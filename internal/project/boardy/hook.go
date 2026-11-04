package boardy

import (
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"time"

	"tofu/internal/konst"
	"tofu/internal/librarian/rules"
	"tofu/internal/sys"
	"tofu/library"
)

const (
	hookActorPrefix   = "hook:"
	userBoardyDirName = "boardy"
	tickedFileName    = "hooks-ticked.json"
	presetFileName    = ".preset"
	builtinPreset     = "builtin"
	presetsDir        = "boardy/hooks"
	libraryRoot       = "library"
	removedSuffix     = ".removed"
)

type subject struct {
	event    rules.HookEvent
	key      string
	ticket   Ticket
	path     string
	from, to Status
	actor    string
	depth    int
	entered  time.Time
	leave    []int
	owns     []string
	fields   []string
	replaces string
	reason   string
	entry    EventKind
	waived   string
	dry      bool
}

func (m Managed) subjectOf(event rules.HookEvent, key string, ticket Ticket, to Status, actor string, depth int) subject {
	path, _ := m.Store.TicketPath(ticket.ID)
	return subject{event: event, key: key, ticket: ticket, path: path, from: ticket.Status, to: to, actor: actor, depth: depth, owns: ticket.Owns}
}

func (s subject) expand(text string) string {
	return strings.NewReplacer("{ticket}", s.ticket.ID, "{title}", s.ticket.Title).Replace(text)
}

const doBroken rules.Do = "broken"

func (s Store) hooks(key string) ([]rules.Hook, error) {
	library, broken, err := s.libraryHooks(key)
	var hooks []rules.Hook
	won := func(h rules.Hook) bool {
		return slices.ContainsFunc(hooks, func(first rules.Hook) bool { return first.ID == h.ID })
	}
	for _, scope := range hookScopes() {
		scoped, scopeBroken, scopeErr := s.scopeHooks(key, scope)
		err = errors.Join(err, scopeErr)
		for _, h := range scoped {
			if !won(h) {
				hooks = append(hooks, h)
			}
		}
		broken = append(broken, scopeBroken...)
	}
	return slices.Concat(slices.DeleteFunc(library, won), hooks, broken), err
}

func (s Store) preset(key string) (string, error) {
	raw, err := os.ReadFile(filepath.Join(s.BoardDir(key), presetFileName))
	if errors.Is(err, fs.ErrNotExist) {
		return builtinPreset, nil
	}
	return strings.TrimSpace(string(raw)), err
}

func (s Store) libraryHooks(key string) ([]rules.Hook, []rules.Hook, error) {
	preset, err := s.preset(key)
	if err != nil || preset == "" {
		return nil, nil, err
	}
	dir := path.Join(presetsDir, preset)
	entries, err := fs.ReadDir(library.Files(), dir)
	if err != nil {
		return nil, nil, fmt.Errorf("%s names the preset %q, and the library holds no %s: %w", filepath.Join(s.BoardDir(key), presetFileName), preset, dir, err)
	}
	var hooks, broken []rules.Hook
	for _, entry := range entries {
		data, err := fs.ReadFile(library.Files(), path.Join(dir, entry.Name()))
		file := path.Join(libraryRoot, dir, entry.Name())
		h, parseErr := rules.ParseHook(data, file)
		if err = errors.Join(err, parseErr); err == nil {
			err = validHook(h)
		}
		if err != nil {
			broken = append(broken, brokenHooks(file, declaredEvents(data), err)...)
			continue
		}
		if removed, _ := sys.Exists(filepath.Join(s.BoardDir(key), RulesDirName, h.ID+removedSuffix)); !removed {
			hooks = append(hooks, h)
		}
	}
	return hooks, broken, nil
}

func brokenHooks(file string, on []rules.HookEvent, err error) []rules.Hook {
	hooks := make([]rules.Hook, len(on))
	for i, event := range on {
		hooks[i] = rules.Hook{ID: strings.TrimSuffix(filepath.Base(file), filepath.Ext(file)), File: file, Text: err.Error(), When: rules.WhenBefore, On: event, Action: rules.Action{Do: doBroken}}
	}
	return hooks
}

func declaredEvents(data []byte) []rules.HookEvent {
	for line := range strings.Lines(string(data)) {
		value, found := strings.CutPrefix(line, "on:")
		if !found {
			continue
		}
		var events []rules.HookEvent
		for one := range strings.SplitSeq(strings.Trim(strings.TrimSpace(value), `"`), ",") {
			one = strings.TrimSpace(one)
			for _, event := range []rules.HookEvent{rules.HookEvent(one), rules.Event(one).Hook()} {
				if slices.Contains(rules.HookEvents(), event) {
					events = append(events, event)
					break
				}
			}
		}
		if events != nil {
			return events
		}
	}
	return rules.HookEvents()
}

func leafErrors(err error) []error {
	if err == nil {
		return nil
	}
	joined, many := err.(interface{ Unwrap() []error })
	if !many {
		return []error{err}
	}
	var leaves []error
	for _, one := range joined.Unwrap() {
		leaves = append(leaves, leafErrors(one)...)
	}
	return leaves
}

func userHooksDir() (string, error) {
	home, err := sys.HomeConfigDir()
	return filepath.Join(home, userBoardyDirName, RulesDirName), err
}

func (s Store) hookDir(key string, scope HookScope) (string, error) {
	switch scope {
	case ScopeBoard:
		return filepath.Join(s.BoardDir(key), RulesDirName), nil
	case ScopeProject:
		return filepath.Join(s.Dir, RulesDirName), nil
	case ScopeUser:
		return userHooksDir()
	}
	return "", fmt.Errorf("scope %q is one of %s", scope, strings.Join(scope.Enum(), ", "))
}

func (s Store) hookSource(row HookRow) ([]byte, error) {
	if row.Scope == ScopeLibrary {
		return fs.ReadFile(library.Files(), strings.TrimPrefix(row.File, libraryRoot+"/"))
	}
	return os.ReadFile(row.File)
}

func (s Store) removeHook(key string, row HookRow) error {
	if row.Scope != ScopeLibrary {
		return os.Remove(row.File)
	}
	dir := filepath.Join(s.BoardDir(key), RulesDirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return writeAtomic(filepath.Join(dir, row.ID+removedSuffix), []byte(row.File+"\n"))
}

func (s Store) scopeHooks(key string, scope HookScope) ([]rules.Hook, []rules.Hook, error) {
	if scope == ScopeLibrary {
		return s.libraryHooks(key)
	}
	dir, err := s.hookDir(key, scope)
	if err != nil {
		return nil, nil, err
	}
	hooks, loadErr := rules.LoadHooks(dir)
	var broken []rules.Hook
	hooks = slices.DeleteFunc(hooks, func(h rules.Hook) bool {
		invalid := validHook(h)
		if invalid != nil {
			broken = append(broken, brokenHooks(h.File, h.Events(), invalid)...)
		}
		return invalid != nil
	})
	files, _ := filepath.Glob(filepath.Join(dir, "*.yaml"))
	for _, one := range leafErrors(loadErr) {
		at := slices.IndexFunc(files, func(file string) bool { return strings.Contains(one.Error(), file) })
		if at < 0 {
			err = errors.Join(err, one)
			continue
		}
		data, _ := os.ReadFile(files[at])
		broken = append(broken, brokenHooks(files[at], declaredEvents(data), one)...)
	}
	return hooks, broken, err
}

func validHook(h rules.Hook) error {
	var errs []error
	for _, status := range slices.Concat(h.If.From, h.If.To) {
		_, err := ParseStatus(status)
		errs = append(errs, err)
	}
	for _, kind := range h.If.Type {
		_, err := ParseKind(kind)
		errs = append(errs, err)
	}
	for _, priority := range h.If.Priority {
		_, err := ParsePriority(priority)
		errs = append(errs, err)
	}
	for _, field := range h.If.Field {
		if !slices.Contains(ticketFields(), field) {
			errs = append(errs, fmt.Errorf("field %q is not a ticket field: %s", field, strings.Join(ticketFields(), ", ")))
		}
	}
	switch h.Action.Do {
	case rules.DoCheck:
		_, _, err := resolveCheck(h)
		errs = append(errs, err)
	case rules.DoMove:
		_, err := ParseStatus(h.Action.Arg)
		errs = append(errs, err)
	case rules.DoField:
		if !slices.Contains(ticketFields(), h.Action.Arg) {
			errs = append(errs, fmt.Errorf("%q is not a ticket field: %s", h.Action.Arg, strings.Join(ticketFields(), ", ")))
		}
	case rules.DoSet:
		field, value, asks := h.Action.Setting()
		switch settable := []string{"epic", "sprint", "assignee", "priority", "component", "spec", "size"}; {
		case !slices.Contains(settable, field):
			errs = append(errs, fmt.Errorf("set names %q, and a hook sets one of %s", field, strings.Join(settable, ", ")))
		case asks == "":
			errs = append(errs, new(Ticket).set(field, value))
		}
	}
	if err := errors.Join(errs...); err != nil {
		return fmt.Errorf("%s: hook %q: %w", h.File, h.ID, err)
	}
	return nil
}

func ticketFields() []string {
	var names []string
	for _, shape := range []reflect.Type{reflect.TypeFor[Front](), reflect.TypeFor[Ticket]()} {
		for i := range shape.NumField() {
			if name, _, _ := strings.Cut(shape.Field(i).Tag.Get("json"), ","); name != "" {
				names = append(names, name)
			}
		}
	}
	return names
}

func filled(ticket Ticket) []string {
	raw, _ := json.Marshal(ticket)
	var all map[string]any
	_ = json.Unmarshal(raw, &all)
	var names []string
	for _, name := range ticketFields() {
		switch value := all[name].(type) {
		case string:
			if strings.TrimSpace(value) != "" {
				names = append(names, name)
			}
		case []any:
			if slices.ContainsFunc(value, func(one any) bool { return one != "" }) {
				names = append(names, name)
			}
		case float64:
			if value != 0 {
				names = append(names, name)
			}
		}
	}
	return names
}

func (m Managed) actorKind(actor string) rules.HookActor {
	switch {
	case strings.HasPrefix(actor, hookActorPrefix):
		return rules.ActorHook
	case strings.Contains(actor, subAgentOfSession):
		return rules.ActorSubAgent
	case m.actsForPerson(actor):
		return rules.ActorPerson
	}
	return rules.ActorLead
}

func (m Managed) holds(h rules.Hook, s subject) bool {
	c, t := h.If, s.ticket
	among := func(want []string, have ...string) bool {
		return want == nil || slices.ContainsFunc(have, func(one string) bool { return slices.Contains(want, one) })
	}
	named := func(want, have string) bool {
		switch want {
		case "":
			return true
		case "set":
			return have != ""
		case "empty":
			return have == ""
		}
		return want == have
	}
	created := cmp.Or(t.Created, time.Now())
	return among(c.From, string(s.from)) && among(c.To, string(s.to)) && among(c.Type, string(t.Type)) && among(c.Priority, string(t.Priority)) && among(c.Labels, t.Labels...) &&
		(c.NotLabels == nil || !among(c.NotLabels, t.Labels...)) && among(c.Field, s.fields...) &&
		(c.CreatedBefore.IsZero() || created.Before(c.CreatedBefore)) && (c.CreatedAfter.IsZero() || !created.Before(c.CreatedAfter)) &&
		named(c.Epic, t.Epic) && named(c.Sprint, t.Sprint) &&
		(c.Owns == "" || slices.ContainsFunc(t.Owns, func(glob string) bool { return rules.ScopeReaches(c.Owns, glob) })) &&
		(c.Actor == nil || slices.Contains(c.Actor, m.actorKind(s.actor))) &&
		time.Since(s.entered) >= c.InStatusFor
}

func enteredStatus(events []Event) map[string]time.Time {
	at := map[string]time.Time{}
	for _, event := range events {
		if event.Ticket != "" && event.To != "" && (event.Kind == Created || event.Kind == Moved || event.Kind == Accepted) {
			at[event.Ticket] = event.At
		}
	}
	return at
}

func (m Managed) check(h rules.Hook, s subject) (checked, error) {
	text := s.expand(h.Text)
	switch h.Action.Do {
	case rules.DoField:
		return checked{detail: cmp.Or(text, "it has no "+h.Action.Arg), failed: !slices.Contains(filled(s.ticket), h.Action.Arg)}, nil
	case rules.DoPerson:
		return checked{detail: cmp.Or(text, "this waits for the person to approve it"), failed: m.actorKind(s.actor) != rules.ActorPerson}, nil
	case rules.DoJev:
		answer, said := m.askJev(h.Action.Arg, s)
		return checked{detail: strings.TrimSpace(text + "\n" + said), failed: answer == ""}, nil
	case rules.DoContract, rules.DoCheck:
		run, arg, err := resolveCheck(h)
		if err != nil {
			return checked{}, err
		}
		return run(m, h, s, arg)
	case rules.DoCommand:
		output, failed, err := rules.RunCommand(h.Action.Arg, s.path, []string{"TOFU_BOARD_EVENT=" + string(s.event), "TOFU_TICKET=" + s.path,
			"TOFU_TICKET_ID=" + s.ticket.ID, "TOFU_FROM=" + string(s.from), "TOFU_TO=" + string(s.to), "TOFU_ACTOR=" + s.actor})
		if err != nil {
			return checked{}, fmt.Errorf("hook %q %w", h.ID, err)
		}
		return checked{detail: strings.TrimSpace(text + "\n" + output), failed: failed}, nil
	}
	panic("boardy: unknown check " + string(h.Action.Do))
}

type trial struct {
	checked
	blocked bool
	then    rules.Then
}

func (m Managed) try(h rules.Hook, s subject) (trial, error) {
	result, err := m.check(h, s)
	blocked := result.failed && (h.Then == rules.ThenRefuse || h.Then == rules.ThenAsk && m.actorKind(s.actor) != rules.ActorPerson)
	return trial{checked: result, blocked: blocked, then: h.Then}, err
}

func (m Managed) entered(key string, ticket Ticket) time.Time {
	events, err := m.Store.Events(key, 0)
	if err != nil {
		return time.Now()
	}
	return cmp.Or(enteredStatus(events)[ticket.ID], ticket.Created)
}

func (m Managed) holding(hooks []rules.Hook, when rules.When, s subject) ([]rules.Hook, subject) {
	hooks = slices.DeleteFunc(slices.Clone(hooks), func(h rules.Hook) bool {
		return h.When != when || !h.Fires(s.event) || s.actor == hookActorPrefix+h.ID
	})
	if s.entered.IsZero() {
		s.entered = time.Now()
		if s.ticket.ID != "" && slices.ContainsFunc(hooks, func(h rules.Hook) bool { return h.If.InStatusFor > 0 }) {
			s.entered = m.entered(s.key, s.ticket)
		}
	}
	return slices.DeleteFunc(hooks, func(h rules.Hook) bool { return !m.holds(h, s) }), s
}

func (m Managed) excused(hooks []rules.Hook, s subject) string {
	var said []string
	for _, h := range hooks {
		lifted := h
		lifted.If.NotLabels, lifted.If.CreatedBefore, lifted.If.CreatedAfter = nil, time.Time{}, time.Time{}
		if h.When != rules.WhenBefore || !h.Fires(s.event) || !h.Action.Do.Check() || m.holds(h, s) || !m.holds(lifted, s) {
			continue
		}
		why := "made after " + utcStamp(h.If.CreatedBefore)
		if at := slices.IndexFunc(s.ticket.Labels, func(label string) bool { return slices.Contains(h.If.NotLabels, label) }); at >= 0 {
			why = "labelled " + s.ticket.Labels[at]
		} else if s.ticket.Created.Before(h.If.CreatedAfter) {
			why = "made before " + utcStamp(h.If.CreatedAfter)
		}
		said = append(said, why+", so "+h.ID+" was not asked")
	}
	return strings.Join(said, "; ")
}

func (m Managed) weigh(hooks []rules.Hook, when rules.When, s subject) ([]Fired, []rules.Hook, error) {
	hooks, s = m.holding(hooks, when, s)
	var fired, bypassed []Fired
	var effects []rules.Hook
	var waits error
	for _, h := range hooks {
		if h.Action.Do == doBroken {
			return fired, effects, RefusedError{Ticket: s.ticket.ID, Why: h.ID + ": " + h.Text + " (not bypassable: fix or remove " + h.File + ")"}
		}
		if !h.Action.Do.Check() {
			effects = append(effects, h)
			continue
		}
		if h.ID == s.waived {
			bypassed = append(bypassed, firing(h, s, rules.ModeShadow, false, thenBypassed, "read as a bypass from the "+notOwnedOnPurpose+" reason: "+s.reason))
			continue
		}
		t, err := m.try(h, s)
		if err != nil {
			return nil, nil, err
		}
		if !t.failed {
			continue
		}
		if t.blocked && when == rules.WhenBefore && m.bypassable(s) && h.ID == m.Bypass.Hook {
			f, waiting := m.bypass(h, s)
			bypassed = append(bypassed, f...)
			if waiting != nil {
				waits = waiting
			}
			continue
		}
		mode := rules.ModeShadow
		if t.blocked {
			mode = rules.ModeEnforced
		}
		fired = append(fired, firing(h, s, mode, t.blocked, t.then, t.detail))
		switch {
		case t.blocked && t.then == rules.ThenRefuse:
			return fired, effects, RefusedError{Ticket: s.ticket.ID, Why: h.ID + ": " + t.detail + " (" + m.bypassHint(h, s) + ")", Done: t.done}
		case t.blocked:
			waits = WaitingError{Ticket: s.ticket.ID, Rule: h.ID, Detail: t.detail + " (" + m.bypassHint(h, s) + ")"}
		}
	}
	return append(fired, bypassed...), effects, waits
}

func firing(h rules.Hook, s subject, mode rules.Mode, blocked bool, then rules.Then, detail string) Fired {
	finding := rules.Finding{RuleID: h.ID, Target: s.ticket.ID, Detail: detail}
	return Fired{Fire: rules.Fire{RuleID: h.ID, Mode: mode, Target: s.ticket.ID, Findings: []rules.Finding{finding}, Blocked: blocked, At: time.Now()}, Checker: string(h.Action.Do), Then: then}
}

func hookText(id, what string, event rules.HookEvent, detail string) string {
	return fmt.Sprintf("%s (%s, %s): %s", id, what, event, detail)
}

func (e Event) hookSaid(whats ...string) (string, bool) {
	for _, what := range whats {
		if id, rest, found := strings.Cut(e.Text, " ("+what+", "); found && e.Kind == RuleFired {
			_, detail, _ := strings.Cut(rest, "): ")
			return id + ": " + detail, true
		}
	}
	return "", false
}

func (s Store) EventSizes() map[string]int64 {
	boards, _ := s.Boards()
	sizes := map[string]int64{}
	for _, board := range boards {
		if info, err := os.Stat(filepath.Join(board.Dir, EventsFileName)); err == nil {
			sizes[board.Key] = info.Size()
		}
	}
	return sizes
}

func (s Store) NudgesSince(sizes map[string]int64) []string {
	nudges, _ := s.saidSince(sizes, string(rules.ThenNudge), string(rules.DoSet))
	return nudges
}

func (s Store) NotifiesSince(sizes map[string]int64) ([]string, map[string]int64) {
	return s.saidSince(sizes, string(rules.DoNotify))
}

func (s Store) saidSince(sizes map[string]int64, whats ...string) ([]string, map[string]int64) {
	boards, _ := s.Boards()
	var said []string
	read := map[string]int64{}
	maps.Copy(read, sizes)
	for _, board := range boards {
		raw, err := os.ReadFile(filepath.Join(board.Dir, EventsFileName))
		start, end := sizes[board.Key], int64(bytes.LastIndexByte(raw, '\n')+1)
		if err != nil || end <= start {
			continue
		}
		read[board.Key] = end
		for line := range strings.Lines(string(raw[start:end])) {
			var event Event
			if json.Unmarshal([]byte(line), &event) != nil {
				continue
			}
			if text, found := event.hookSaid(whats...); found {
				said = append(said, text)
			}
		}
	}
	return said, read
}

func (m Managed) record(s subject, fired []Fired) error {
	var err error
	for _, f := range fired {
		err = errors.Join(err, m.Store.Record(s.key, s.firedEvent(f)))
	}
	return err
}

func (s subject) firedEvent(f Fired) Event {
	kind := RuleFired
	if f.waits() {
		kind = Waiting
	}
	return Event{Ticket: s.ticket.ID, Kind: kind, Actor: s.actor, To: s.to, Text: hookText(f.RuleID, string(f.Then), s.event, f.Text())}
}

func (m Managed) note(h rules.Hook, s subject, text string) error {
	return m.Store.Record(s.key, Event{Ticket: s.ticket.ID, Kind: RuleFired, Actor: hookActorPrefix + h.ID, To: s.to, Text: hookText(h.ID, string(h.Action.Do), s.event, text)})
}

func (m Managed) gate(s *subject) error {
	hooks, err := m.Store.hooks(s.key)
	if err != nil {
		return err
	}
	if err := m.vetBypass(hooks, *s); err != nil {
		return err
	}
	_, sets, _ := m.weigh(slices.DeleteFunc(slices.Clone(hooks), func(h rules.Hook) bool { return h.Action.Do != rules.DoSet }), rules.WhenBefore, *s)
	var heldWrites []func() error
	for _, h := range sets {
		said, write, err := m.set(h, s)
		if err != nil {
			return errors.Join(err, m.note(h, *s, said))
		}
		persist := func() error { return errors.Join(write(), m.note(h, *s, said)) }
		if m.bypassable(*s) {
			heldWrites = append(heldWrites, persist)
		} else if err := persist(); err != nil {
			return err
		}
	}
	fired, _, err := m.weigh(hooks, rules.WhenBefore, *s)
	if m.bypassable(*s) {
		if err = m.settleBypass(hooks, *s, fired, err); errors.As(err, new(bypassRefused)) {
			return err
		}
	}
	for _, persist := range heldWrites {
		if err := persist(); err != nil {
			return err
		}
	}
	return errors.Join(m.record(*s, fired), err)
}

func (m Managed) after(s subject) error {
	hooks, err := m.Store.hooks(s.key)
	if err != nil {
		return err
	}
	return m.run(hooks, s)
}

func (m Managed) run(hooks []rules.Hook, s subject) error {
	fired, effects, err := m.weigh(hooks, rules.WhenAfter, s)
	err = errors.Join(err, m.record(s, fired))
	for _, h := range effects {
		err = errors.Join(err, m.effect(h, s))
	}
	return err
}

func (m Managed) effect(h rules.Hook, s subject) error {
	arg, depth, actor := s.expand(h.Action.Arg), s.depth+1, hookActorPrefix+h.ID
	if depth > konst.BoardHookDepthMost {
		return m.note(h, s, fmt.Sprintf("stopped at depth %d, the cap: %s %s did not run", konst.BoardHookDepthMost, h.Action.Do, arg))
	}
	var err error
	switch h.Action.Do {
	case rules.DoCreate:
		var made Ticket
		follow := Ticket{Front: Front{Title: arg, Epic: s.ticket.Epic, Sprint: s.ticket.Sprint}, Log: m.Store.logLine(actor, fmt.Sprintf("made by hook %s after %s of %s", h.ID, s.event, cmp.Or(s.ticket.ID, s.ticket.Title)))}
		if s.ticket.ID != "" {
			follow.Relates = []string{s.ticket.ID}
		}
		if made, err = m.created(s.key, follow, actor, "", depth); err == nil {
			arg += " is " + made.ID
		}
	case rules.DoMove, rules.DoAssign, rules.DoLog:
		var ticket Ticket
		var board Board
		if ticket, board, err = m.ticketOnBoard(s.ticket.ID); err != nil {
			break
		}
		switch h.Action.Do {
		case rules.DoMove:
			_, err = m.move(board, ticket, Status(arg), "hook "+h.ID, actor, depth, nil)
		case rules.DoAssign:
			_, err = m.assign(board, ticket, arg, actor, depth)
		case rules.DoLog:
			_, err = m.comment(board, ticket, arg, actor, depth)
		}
	case rules.DoSet:
		var write func() error
		arg, write, err = m.set(h, &s)
		err = errors.Join(err, write())
	case rules.DoNotify:
	}
	if err != nil {
		return m.Store.Record(s.key, Event{Ticket: s.ticket.ID, Kind: RuleFired, Actor: actor, To: s.to, Text: hookText(h.ID, string(rules.ThenNudge), s.event, string(h.Action.Do)+" "+arg+" did not run: "+err.Error())})
	}
	return m.note(h, s, arg)
}

func (m Managed) set(h rules.Hook, s *subject) (string, func() error, error) {
	unwritten := func() error { return nil }
	field, value, asks := h.Action.Setting()
	value = s.expand(value)
	said := field + " " + value
	if asks != "" {
		var why string
		if value, why = m.askJev(asks, *s); value == "" {
			return field + " left as it was: " + why, unwritten, nil
		}
		said = field + " " + value + ": " + why
	}
	if err := s.ticket.set(field, value); err != nil || s.ticket.ID == "" {
		return said, unwritten, err
	}
	id := s.ticket.ID
	return said, func() error {
		_, err := m.Store.Edit(id, func(t *Ticket) error { return t.set(field, value) })
		return err
	}, nil
}

func (m Managed) Fire(when rules.When, event rules.HookEvent, key, id, actor string) error {
	s, err := m.firedOn(event, key, id, actor)
	if err != nil {
		return err
	}
	if when == rules.WhenBefore {
		return m.gate(&s)
	}
	return m.after(s)
}

func (m Managed) firedOn(event rules.HookEvent, key, id, actor string) (subject, error) {
	if id == "" {
		return subject{event: event, key: key, actor: actor}, nil
	}
	ticket, board, err := m.ticketOnBoard(id)
	if err != nil {
		return subject{}, err
	}
	return m.subjectOf(event, board.Key, ticket, ticket.Status, actor, 0), nil
}

type Foreseen struct {
	Nudges, Notifies, Effects []string
}

func (m Managed) Foresee(event rules.HookEvent, key, id, actor string) (Foreseen, error) {
	s, err := m.firedOn(event, key, id, actor)
	if err != nil {
		return Foreseen{}, err
	}
	hooks, err := m.Store.hooks(s.key)
	if err != nil {
		return Foreseen{}, err
	}
	var foreseen Foreseen
	held, s := m.holding(hooks, rules.WhenAfter, s)
	var decidable []rules.Hook
	for _, h := range held {
		if h.Action.Do == rules.DoCommand || h.Action.Do == rules.DoJev {
			foreseen.Effects = append(foreseen.Effects, h.ID+" would run "+string(h.Action.Do)+" "+h.Action.Arg+" to decide")
			continue
		}
		decidable = append(decidable, h)
	}
	fired, effects, err := m.weigh(decidable, rules.WhenAfter, s)
	for _, f := range fired {
		if said, found := s.firedEvent(f).hookSaid(string(rules.ThenNudge)); found {
			foreseen.Nudges = append(foreseen.Nudges, said)
		}
	}
	for _, h := range effects {
		arg := s.expand(h.Action.Arg)
		if h.Action.Do == rules.DoNotify {
			foreseen.Notifies = append(foreseen.Notifies, h.ID+": "+arg)
			continue
		}
		foreseen.Effects = append(foreseen.Effects, h.ID+" would "+string(h.Action.Do)+" "+arg)
	}
	return foreseen, err
}

func (m Managed) Tick(key string) ([]string, error) {
	hooks, loadErr := m.Store.hooks(key)
	if _, err := m.Bypasses(key); err != nil {
		loadErr = errors.Join(loadErr, err)
	}
	state := filepath.Join(m.Store.BoardDir(key), tickedFileName)
	var ran []string
	err := m.Store.locked(key, "hooks-ticked", func() error {
		last, err := ticked(state)
		if err != nil {
			return err
		}
		now := time.Now()
		due := slices.DeleteFunc(hooks, func(h rules.Hook) bool {
			return h.On != rules.HookSchedule || h.Action.Do == doBroken || !h.Due(last[h.File], now)
		})
		if len(due) == 0 {
			return nil
		}
		tickets, err := m.Store.Tickets(key)
		events, eventsErr := m.Store.Events(key, 0)
		entered := enteredStatus(events)
		for _, h := range due {
			last[h.File], ran = now, append(ran, h.ID)
			for _, ticket := range tickets {
				s := m.subjectOf(rules.HookSchedule, key, ticket, ticket.Status, "", 0)
				s.from, s.entered = "", cmp.Or(entered[ticket.ID], ticket.Created)
				err = errors.Join(err, m.run([]rules.Hook{h}, s))
			}
		}
		data, marshalErr := json.Marshal(last)
		return errors.Join(err, eventsErr, marshalErr, writeAtomic(state, data))
	})
	return ran, errors.Join(loadErr, err)
}

func ticked(state string) (map[string]time.Time, error) {
	last := map[string]time.Time{}
	raw, err := os.ReadFile(state)
	if err == nil {
		err = json.Unmarshal(raw, &last)
	}
	if errors.Is(err, fs.ErrNotExist) {
		return last, nil
	}
	return last, err
}
