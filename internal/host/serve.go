package host

import (
	"bufio"
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"tofu/internal/konst"
	"tofu/internal/session"
	"tofu/internal/shell"
	"tofu/internal/sys"
	"tofu/internal/turn"
)

type ServeConfig struct {
	Host     *Host
	Dir      string
	In       io.Reader
	Out      io.Writer
	Shells   *shell.Registry
	Carry    func(handle string) (Carry, error)
	Verb     func(args []string) (VerbResult, error)
	Quota    func() []QuotaWindow
	Sessions func(open string) (SessionList, error)
	Wires    func() []string
	Sources  map[string]string
	Ledger   func(LedgerParams) (LedgerReport, error)
	Run      func(ctx context.Context, command string) (output string, stopped bool)
	Compact  func() (Compaction, error)
	Stale    func() bool
	Setup    func() []Requirement
	SaveKey  func(provider, key string) (note string, err error)
	Logout   func(LogoutParams) (note string, err error)
}

var errCommandRunning = errors.New("a shell.run command is still running: wait for it, or turn.stop stops it")

type server struct {
	ServeConfig
	box     *outbox
	mu      sync.Mutex
	items   items
	client  string
	ready   bool
	pending map[string]ApprovalRequest
	shells  map[string]*watchedShell
	command context.CancelFunc
	usage   usageHeld
}

func Serve(cfg ServeConfig) error {
	s := &server{ServeConfig: cfg, box: newOutbox(), pending: map[string]ApprovalRequest{}, shells: map[string]*watchedShell{}, items: newItems(cfg.Host.ID())}
	written := make(chan error, 1)
	go func() { written <- s.box.drain(cfg.Out) }()
	quit := make(chan struct{})
	var workers sync.WaitGroup
	workers.Go(func() { s.pump(quit) })
	workers.Go(func() { s.watchShells(quit) })
	workers.Go(func() { s.pollQuota(quit) })
	err := s.read()
	s.windDown()
	close(quit)
	workers.Wait()
	s.box.close()
	return errors.Join(err, <-written)
}

func (s *server) read() error {
	lines := bufio.NewScanner(s.In)
	lines.Buffer(nil, konst.ServeLineBytes)
	for lines.Scan() {
		if line := bytes.TrimSpace(lines.Bytes()); len(line) > 0 {
			s.receive(line)
		}
	}
	return lines.Err()
}

func (s *server) receive(line []byte) {
	var in struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
		Params json.RawMessage `json:"params"`
		Result json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(line, &in); err != nil {
		s.respond(json.RawMessage("null"), nil, &Refusal{Code: CodeParse, Message: err.Error()})
		return
	}
	switch {
	case in.Method == "" && in.ID != nil:
		s.answered(in.ID, in.Result)
	case in.ID == nil:
	case strings.HasPrefix(in.Method, queryPrefix) || slices.Contains([]string{"login.start", "login.key", "login.logout", "session.info", "session.find", "session.trace", "shell.run", "session.list", "session.history", "reload", "models.reload", "hooks.trust", "learn.scan", "setup.check"}, in.Method):
		go func() {
			result, err := s.call(in.Method, in.Params)
			s.respond(in.ID, result, err)
		}()
	default:
		result, err := s.call(in.Method, in.Params)
		s.respond(in.ID, result, err)
	}
}

func (s *server) respond(id json.RawMessage, result any, err error) {
	reply := message{JSONRPC: rpcVersion, ID: id}
	var refused *Refusal
	switch {
	case errors.As(err, &refused):
		reply.Error = refused
	case err != nil:
		reply.Error = &Refusal{Code: CodeRefused, Message: sys.LoadKeyRedactor().Redact(err.Error())}
	default:
		reply.Result = result
	}
	s.box.push(outgoing{msg: reply, kept: true})
}

func handle[P any](raw json.RawMessage, run func(P) (any, error)) (any, error) {
	var params P
	if len(raw) > 0 {
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&params); err != nil {
			return nil, &Refusal{Code: CodeBadParams, Message: err.Error()}
		}
	}
	return run(params)
}

func (s *server) call(method string, raw json.RawMessage) (any, error) {
	if method != "initialize" && !s.ready {
		return nil, &Refusal{Code: CodeInvalid, Message: "initialize first"}
	}
	switch method {
	case "initialize":
		return handle(raw, s.initialize)
	case "session.list":
		return handle(raw, s.sessions)
	case "session.state":
		return handle(raw, func(NoParams) (any, error) { return s.state(), nil })
	case "session.set":
		return handle(raw, s.set)
	case "session.open":
		result, err := handle(raw, s.open)
		if err == nil {
			go s.quota()
		}
		if err == nil && s.Verb != nil {
			go s.refreshUsage()
		}
		return result, err
	case "session.rename":
		return handle(raw, s.rename)
	case "cron.command":
		return handle(raw, func(p CronCommandParams) (any, error) {
			reply, err := s.Host.CronCommand(p.Line)
			return CronCommandResult{Note: reply.Note}, err
		})
	case queryPrefix + "cron":
		return handle(raw, func(NoParams) (any, error) { return s.cronState(), nil })
	case "turn.send":
		return handle(raw, s.send)
	case "turn.steer":
		return handle(raw, s.steer)
	case "turn.sendNow":
		return handle(raw, s.sendNow)
	case "turn.stop":
		return handle(raw, s.stop)
	case "turn.unsteer":
		return handle(raw, func(p UnsteerParams) (any, error) { return UnsteerResult{Removed: s.Host.Unsteer(p.Text)}, nil })
	case "shell.run":
		return handle(raw, s.shellRun)
	case "session.compact":
		return handle(raw, s.compact)
	case "session.history":
		return handle(raw, s.history)
	case queryPrefix + "ledger":
		return handle(raw, func(p LedgerParams) (any, error) {
			if s.Ledger == nil {
				return nil, &Refusal{Code: CodeRefused, Message: "this tofu reads no ledger"}
			}
			return s.Ledger(p)
		})
	case "undo":
		return handle(raw, s.undo)
	case "shell.read":
		return handle(raw, s.shellRead)
	case "shell.kill":
		return handle(raw, s.shellKill)
	case "label":
		return handle(raw, func(p LabelParams) (any, error) { return s.Verb([]string{"label", cmp.Or(p.Row, "--last"), p.Outcome}) })
	case "settings.set":
		return handle(raw, func(p SettingsSetParams) (any, error) {
			return s.Verb([]string{"settings", "set", "--scope", cmp.Or(p.Scope, "global"), p.Key, p.Value})
		})
	case "login.start":
		return handle(raw, func(p LoginParams) (any, error) { return s.Verb([]string{"login", p.Role, p.Provider}) })
	}
	return s.data(method, raw)
}

func (s *server) initialize(p InitializeParams) (any, error) {
	if len(p.Versions) > 0 && !slices.Contains(p.Versions, Protocol) {
		return nil, &Refusal{Code: CodeInvalid, Message: "this tofu speaks " + Protocol + " and the client offered " + strings.Join(p.Versions, ", ")}
	}
	s.mu.Lock()
	s.client, s.ready = p.Client, true
	s.mu.Unlock()
	return InitializeResult{Protocol: Protocol, Tofu: konst.Version, Project: s.Dir, Capabilities: capabilities()}, nil
}

func (s *server) open(p SessionOpenParams) (any, error) {
	if p.Project != "" && filepath.Clean(p.Project) != filepath.Clean(s.Dir) {
		return nil, &Refusal{Code: CodeRefused, Message: "this tofu serves " + s.Dir + ", not " + p.Project}
	}
	if err := checkAsking(p.Asking); err != nil {
		return nil, err
	}
	if p.Asking != "" {
		s.Host.SetAsking(p.Asking)
	}
	if p.Session == "" {
		id, err := s.Host.OpenFresh()
		if err != nil {
			return nil, busy(err)
		}
		s.mu.Lock()
		s.items.session = id
		s.mu.Unlock()
		return SessionOpenResult{Session: id, Fresh: true}, nil
	}
	carry, err := s.Carry(p.Session)
	if err != nil {
		return nil, err
	}
	chat, err := s.Host.Resume(carry)
	if err != nil {
		return nil, busy(err)
	}
	read := newItems(carry.Session)
	lines := read.all(chat)
	if p.Replay != nil {
		lines = lines[len(lines)-min(max(*p.Replay, 0), len(lines)):]
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items = read
	for _, line := range lines {
		s.box.push(line)
	}
	return SessionOpenResult{Session: carry.Session}, nil
}

func (s *server) history(p SessionHistoryParams) (any, error) {
	if p.Limit < 1 {
		return nil, &Refusal{Code: CodeBadParams, Message: "limit is the number of lines a page holds, at least 1"}
	}
	carry, err := s.Carry(p.Session)
	if err != nil {
		return nil, err
	}
	read := newItems(carry.Session)
	lines := read.all(resumedChat(carry, s.Host.dir))
	end := len(lines)
	if p.Before != nil {
		end = min(max(*p.Before, 0), end)
	}
	page := SessionHistory{Session: carry.Session, First: max(end-p.Limit, 0), Total: len(lines), Lines: []HistoryLine{}}
	for _, line := range lines[page.First:end] {
		page.Lines = append(page.Lines, HistoryLine{Method: line.msg.Method, Params: line.msg.Params})
	}
	return page, nil
}

func (s *server) compact(NoParams) (any, error) {
	if s.Compact == nil {
		return nil, &Refusal{Code: CodeRefused, Message: "this tofu compacts no sessions"}
	}
	if _, running := s.Host.Turn(); running {
		return nil, errTurnRunning
	}
	compacted, err := s.Compact()
	if err != nil || compacted.Into == "" {
		return compacted, err
	}
	labelled := Event{Kind: EventSession, ID: compacted.Into, Root: compacted.Into}
	if store, err := session.OpenIn(s.Host.dir); err == nil {
		labelled = labelledAs(store, labelled)
	}
	s.mu.Lock()
	s.items.session = compacted.Into
	s.box.push(sessionUpdated(s.items.identity("", compacted.Into), labelled))
	s.mu.Unlock()
	go s.listed()
	return compacted, nil
}

func (s *server) rename(p SessionRenameParams) (any, error) {
	store, err := session.OpenIn(s.Host.dir)
	if err != nil {
		return nil, err
	}
	header, err := store.SetName(p.Session, p.Name)
	if err != nil {
		return nil, err
	}
	s.box.push(sessionUpdated(Identity{Session: header.ID, Item: header.ID}, labelledAs(store, Event{Kind: EventSession, ID: header.ID, Root: header.ID})))
	go s.listed()
	return Ack{OK: true}, nil
}

func busy(err error) error {
	var held session.BusyError
	if errors.As(err, &held) {
		return &Refusal{Code: CodeSessionBusy, Message: "session.busy", Data: held}
	}
	return err
}

func (s *server) sameSession(id string) error {
	open := s.Host.ID()
	if id == open {
		return nil
	}
	if store, err := session.OpenIn(s.Host.dir); err == nil {
		if family, err := store.Identity(open); err == nil && family.Family == id {
			return nil
		}
	}
	return &Refusal{Code: CodeRefused, Message: "session " + strconv.Quote(id) + " is not the one open here, " + strconv.Quote(open) + ": open it first"}
}

func (s *server) send(p TurnSendParams) (any, error) {
	if err := s.sameSession(p.Session); err != nil {
		return nil, err
	}
	if _, running := s.Host.Turn(); running {
		return nil, errTurnRunning
	}
	s.mu.Lock()
	commanding := s.command != nil
	s.mu.Unlock()
	if commanding {
		return nil, errCommandRunning
	}
	_, picked := s.Host.Settings()
	pick, err := s.chosen(picked, p.ModelPick)
	if err != nil {
		return nil, err
	}
	task := p.Text
	for _, mention := range p.Mentions {
		task += " @" + mention
	}
	tokens, err := s.attach(p.Images)
	if err != nil {
		return nil, err
	}
	if !s.Host.Send(pick, task+tokens) {
		return nil, errTurnRunning
	}
	turn, _ := s.Host.Turn()
	return TurnResult{Turn: turn}, nil
}

func (s *server) steer(p TurnSteerParams) (any, error) {
	if err := s.sameSession(p.Session); err != nil {
		return nil, err
	}
	turn, running := s.Host.Turn()
	if !running || turn != p.ExpectedTurnID {
		return nil, &Refusal{Code: CodeRefused, Message: "turn " + strconv.Quote(p.ExpectedTurnID) + " is not running, so nothing was steered"}
	}
	return SteerResult{Turn: turn, ID: s.Host.Steer(p.Text)}, nil
}

func (s *server) sendNow(p SendNowParams) (any, error) {
	if _, running := s.Host.Turn(); !running || !s.Host.SendNow(p.ID) {
		return nil, &Refusal{Code: CodeRefused, Message: "no turn is running with that message queued, so nothing was sent"}
	}
	return Ack{OK: true}, nil
}

func (s *server) stop(p TurnParams) (any, error) {
	turn, running := s.Host.Turn()
	stopping := running && (p.Turn == "" || p.Turn == turn)
	switch {
	case stopping && p.Lead:
		s.Host.StopLead()
	case stopping:
		s.Host.Stop()
	}
	return Ack{OK: stopping || !running && s.stopCommand()}, nil
}

func (s *server) stopCommand() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.command != nil {
		s.command()
	}
	return s.command != nil
}

func (s *server) shellRun(p ShellRunParams) (any, error) {
	if s.Run == nil {
		return nil, &Refusal{Code: CodeRefused, Message: "this tofu runs no commands"}
	}
	if _, running := s.Host.Turn(); running {
		return nil, errTurnRunning
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.mu.Lock()
	if s.command != nil {
		s.mu.Unlock()
		return nil, errCommandRunning
	}
	s.command = cancel
	s.mu.Unlock()
	output, stopped := s.Run(ctx, p.Command)
	s.mu.Lock()
	s.command = nil
	s.mu.Unlock()
	return ShellRunResult{Output: s.Host.Ran(p.Command, output, stopped), Stopped: stopped}, nil
}

func (s *server) undo(p UndoParams) (any, error) {
	if err := s.sameSession(p.Session); err != nil {
		return nil, err
	}
	if _, running := s.Host.Turn(); running {
		return nil, errTurnRunning
	}
	return s.Verb([]string{"undo", strconv.Itoa(max(p.Turns, 1)), "--session", p.Session})
}

func (s *server) shellRead(p ShellParams) (any, error) {
	if s.Shells == nil {
		return nil, &Refusal{Code: CodeRefused, Message: "this tofu has no shell registry"}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	watched := s.watched(p.Shell)
	if err := s.follow(p.Shell, watched, sys.LoadKeyRedactor().Redact); err != nil {
		return nil, err
	}
	offset := min(max(p.Offset, 0), len(watched.stream))
	return ShellReadResult{Shell: p.Shell, Offset: offset, Text: watched.stream[offset:], Next: len(watched.stream)}, nil
}

func (s *server) shellKill(p ShellParams) (any, error) {
	if s.Shells == nil {
		return nil, &Refusal{Code: CodeRefused, Message: "this tofu has no shell registry"}
	}
	return Ack{OK: true}, s.Shells.Kill(p.Shell)
}

func (s *server) answered(id, result json.RawMessage) {
	var approval string
	var answer ApprovalAnswer
	if json.Unmarshal(id, &approval) != nil || json.Unmarshal(result, &answer) != nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	asked, pending := s.pending[approval]
	if !pending || (answer.Decision == RememberProject || answer.Decision == RememberGlobal) && asked.Tool != turn.RememberToolName {
		return
	}
	switch answer.Decision {
	case AllowOnce, RememberProject:
		s.Host.AnswerAsk(approval, AllowedOnce)
	case AllowAlways, RememberGlobal:
		s.Host.AnswerAsk(approval, AlwaysHere)
	case RejectOnce:
		s.Host.AnswerAsk(approval, Denied)
	case RejectAlways:
		s.Host.AnswerAsk(approval, NeverHere)
	case Cancelled:
		s.Host.Stop()
	default:
		return
	}
	delete(s.pending, approval)
	s.box.push(kept("approval.resolved", &ApprovalResolved{Identity: asked.Identity, Approval: approval, Decision: answer.Decision, By: s.client}))
}

func (s *server) pump(quit <-chan struct{}) {
	for {
		select {
		case event := <-s.Host.Events():
			s.publish(event)
		case <-s.Host.cronMove:
			s.mu.Lock()
			id := s.items.identity("", "cron")
			s.mu.Unlock()
			s.box.push(merged("cron.updated", "", &CronUpdated{Identity: id, CronState: s.cronState()}))
		case <-quit:
			for events := s.Host.Events(); len(events) > 0; {
				s.publish(<-events)
			}
			return
		}
	}
}

func (s *server) publish(event Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch event.Kind {
	case EventAwaitPerson:
		s.pending[event.ID] = approvalRequest(s.items.identity(event.Agent, event.ID), event)
	case EventResumed:
		if asked, pending := s.pending[event.ID]; pending {
			delete(s.pending, event.ID)
			s.box.push(kept("approval.resolved", &ApprovalResolved{Identity: asked.Identity, Approval: event.ID, Decision: Cancelled, By: "tofu"}))
		}
	case EventTurnEnded:
		go s.quota()
		go s.listed()
	case EventTurnStarted, EventForkEnd:
		go s.listed()
	}
	s.translated(event)
}

func (s *server) cronState() CronState {
	state := CronState{Jobs: []CronJob{}}
	for _, job := range s.Host.Cron().Jobs() {
		spec := job.Spec()
		one := CronJob{ID: job.ID, Schedule: spec.Schedule, Prompt: spec.Prompt, Paused: spec.Paused, Ended: job.Ended}
		if !job.Next.IsZero() {
			one.Next = &job.Next
		}
		if job.Live() {
			state.Live++
		}
		if job.Live() && job.Noun() == "goal" {
			state.Goals++
		}
		state.Jobs = append(state.Jobs, one)
	}
	return state
}

func (s *server) translated(event Event) {
	for _, out := range s.items.translate(event, time.Now()) {
		s.box.push(out)
	}
}

func (s *server) quota() {
	if s.Quota == nil {
		return
	}
	windows := append([]QuotaWindow{}, s.Quota()...)
	s.mu.Lock()
	id := s.items.identity("", s.items.turn)
	s.mu.Unlock()
	s.box.push(merged("quota.updated", "", &QuotaUpdated{Identity: id, Windows: windows}))
}

func (s *server) pollQuota(quit <-chan struct{}) {
	every := time.NewTicker(konst.ServeQuotaPollMinutes * time.Minute)
	defer every.Stop()
	for {
		select {
		case <-quit:
			return
		case <-every.C:
			s.quota()
		}
	}
}

type watchedShell struct {
	state     shell.State
	announced bool
	sent      int
	cursor    shell.Cursor
	stream    string
}

func (s *server) watched(name string) *watchedShell {
	if s.shells[name] == nil {
		s.shells[name] = &watchedShell{}
	}
	return s.shells[name]
}

func (s *server) follow(name string, watched *watchedShell, mask func(string) string) error {
	text, err := s.Shells.Follow(name, &watched.cursor)
	watched.stream += mask(text)
	return err
}

func (s *server) watchShells(quit <-chan struct{}) {
	if s.Shells == nil {
		return
	}
	every := time.NewTicker(konst.ServeShellPollMillis * time.Millisecond)
	defer every.Stop()
	for first := true; ; first = false {
		s.scanShells(first)
		select {
		case <-quit:
			s.scanShells(false)
			return
		case <-every.C:
		}
	}
}

func (s *server) scanShells(first bool) {
	found, err := s.Shells.List()
	if err != nil || len(found) == 0 {
		return
	}
	mask := sys.LoadKeyRedactor().Redact
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, one := range found {
		watched := s.watched(one.Name)
		if watched.announced && watched.state != shell.Running || !watched.announced && first && one.State != shell.Running {
			watched.announced, watched.state = true, one.State
			continue
		}
		id := s.items.identity(one.Owner, one.Name)
		_ = s.follow(one.Name, watched, mask)
		if !watched.announced {
			s.box.push(kept("shell.started", &ShellStarted{Identity: id, Shell: one.Name, Command: mask(one.Command), PID: one.PID, StartedAt: one.Started}))
			if first {
				watched.sent = len(watched.stream)
			}
		}
		if len(watched.stream) > watched.sent {
			s.box.push(merged("shell.output", one.Name, &ShellOutput{Identity: id, Shell: one.Name, Offset: watched.sent, Text: watched.stream[watched.sent:]}))
			watched.sent = len(watched.stream)
		}
		if one.State != shell.Running {
			s.box.push(kept("shell.exited", &ShellExited{Identity: id, Shell: one.Name, ExitCode: one.ExitCode, Killed: one.State == shell.Killed, EndedAt: one.Ended}))
		}
		watched.announced, watched.state = true, one.State
	}
}

func (s *server) windDown() {
	s.stopCommand()
	if _, running := s.Host.Turn(); !running {
		return
	}
	s.Host.Stop()
	for gaveUp := time.Now().Add(konst.ServeStopMillis * time.Millisecond); time.Now().Before(gaveUp); {
		if _, running := s.Host.Turn(); !running {
			return
		}
		time.Sleep(konst.ServeShellPollMillis * time.Millisecond)
	}
}
