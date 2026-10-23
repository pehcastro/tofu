package host

import (
	"bufio"
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math/rand/v2"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"tofu/internal/konst"
	"tofu/internal/llm/quota"
	"tofu/internal/safe"
	"tofu/internal/session"
	"tofu/internal/shell"
	"tofu/internal/snapshot"
	"tofu/internal/status"
	"tofu/internal/sys"
	"tofu/internal/turn"
	"tofu/internal/turn/tools"
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
	Accounts func() []AccountNow
	Sessions func(open string) (SessionList, error)
	Branch   func(SessionBranchParams) (SessionBranchResult, error)
	Access   func(SessionAccessParams) (SessionAccess, error)
	Wires    func() []string
	Sources  map[string]string
	Ledger   func(LedgerParams) (LedgerReport, error)
	Run      func(ctx context.Context, command string) (output string, stopped bool)
	Compact  func(*Host) (Compaction, error)
	Stale    func() bool
	Setup    func() []Requirement
	SaveKey  func(provider, key string) (note string, err error)
	Logout   func(LogoutParams) (note string, err error)
	Spawn    func() (*Host, func())
	Release  func()
}

const noShellRegistry = "this tofu has no shell registry"

var errCommandRunning = errors.New("a shell.run command is still running: wait for it, or turn.stop stops it")

type server struct {
	ServeConfig
	box      *outbox
	mu       sync.Mutex
	opening  sync.Mutex
	items    items
	client   string
	ready    bool
	answers  bool
	takeover bool
	shells   map[string]*watchedShell
	command  context.CancelFunc
	status   statusFeed
	probed   time.Time
	retryAt  time.Time
	requota  chan struct{}
	accounts map[string]AccountCondition
	seen     settingsSeen
	lanes    atomic.Pointer[[]*lane]
}

func Serve(cfg ServeConfig) error {
	s := &server{ServeConfig: cfg, box: newOutbox(), shells: map[string]*watchedShell{}, items: newItems(cfg.Host.ID()),
		status: statusFeed{board: status.Board{Now: time.Now}, acked: map[string]bool{}}, requota: make(chan struct{}, 1), accounts: map[string]AccountCondition{}}
	s.seen.files = readSettingsFiles(cfg.Dir)
	first := s.first()
	first.stop, first.pumped = make(chan struct{}), make(chan struct{})
	s.lanes.Store(&[]*lane{first})
	written, read := make(chan error, 1), make(chan error, 1)
	safe.Fatal(s.report, "serve outbox", func(died error) { written <- died }, func() { written <- s.box.drain(cfg.Out) })
	quit := make(chan struct{})
	var workers sync.WaitGroup
	s.pumpOn(first)
	watch := func(unit string, watcher func(quit <-chan struct{})) {
		workers.Go(func() { safe.Loop(s.report, unit, quit, func() { watcher(quit) }) })
	}
	watch("serve shells", s.watchShells)
	watch("serve quota", s.pollQuota)
	watch("serve boardy", s.watchBoardy)
	watch("serve boardy hooks", s.tickBoardy)
	watch("serve settings", s.watchSettings)
	safe.Fatal(s.report, "serve read", func(died error) { read <- died }, func() { read <- s.read() })
	var err error
	select {
	case err = <-read:
	case err = <-written:
		written <- nil
	}
	s.stopCommand()
	lanes := s.tracked()
	ended := StopAsked{By: s.asker("its connection"), Why: "the connection ended, so tofu serve exits", Everything: true}
	if err != nil {
		ended.Why += ": " + err.Error()
	}
	s.stopTurns(ended, slices.DeleteFunc(slices.Clone(lanes), func(l *lane) bool {
		_, joined := l.host.joined()
		return joined
	})...)
	for _, l := range lanes {
		l.halt()
	}
	close(quit)
	workers.Wait()
	for _, l := range lanes {
		if l.release != nil {
			l.release()
		}
	}
	s.box.close()
	return errors.Join(err, <-written)
}

func (s *server) read() error {
	lines := bufio.NewScanner(s.In)
	lines.Buffer(nil, konst.ServeLineBytes)
	for lines.Scan() {
		if line := bytes.TrimSpace(lines.Bytes()); len(line) > 0 {
			_ = safe.Call(s.report, "serve receive", func() error {
				s.receive(line)
				return nil
			})
		}
	}
	s.opening.Lock()
	defer s.opening.Unlock()
	return lines.Err()
}

func (s *server) receive(line []byte) {
	var in struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
		Params json.RawMessage `json:"params"`
		Result json.RawMessage `json:"result"`
		Error  json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(line, &in); err != nil {
		s.respond(json.RawMessage("null"), nil, &Refusal{Code: CodeParse, Message: err.Error()})
		return
	}
	switch {
	case in.Method == "" && in.Error != nil:
	case in.Method == "" && in.ID != nil:
		if err := s.answered(in.ID, in.Result); err != nil {
			s.respond(in.ID, nil, err)
		}
	case in.ID == nil:
	case strings.HasPrefix(in.Method, queryPrefix) || slices.Contains([]string{"login.start", "login.key", "login.logout", "session.info", "session.find", "session.trace", "session.turns", "mention.resolve", "shell.run", "session.list", "session.history", "memory.view", "memory.zoom", "memory.recall", "reload", "models.reload", "hooks.trust", "learn.scan", "setup.check"}, in.Method):
		safe.Go(s.report, in.Method, func() { s.answer(in.ID, in.Method, in.Params) })
	case in.Method == "session.open":
		s.opening.Lock()
		safe.Go(s.report, in.Method, func() {
			defer s.opening.Unlock()
			s.answer(in.ID, in.Method, in.Params)
		})
	default:
		s.opening.Lock()
		defer s.opening.Unlock()
		s.answer(in.ID, in.Method, in.Params)
	}
}

func (s *server) answer(id json.RawMessage, method string, params json.RawMessage) {
	var result any
	err := safe.Call(s.report, method, func() (err error) {
		result, err = s.call(method, params)
		return err
	})
	s.respond(id, result, err)
}

func (s *server) report(p safe.Panic) {
	s.focus().host.report(p)
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
	case "approval.revoke":
		return handle(raw, s.revoke)
	case "session.open":
		result, err := handle(raw, s.open)
		if err == nil {
			safe.Go(s.report, "serve quota", s.quota)
		}
		if opened, ok := result.(SessionOpenResult); ok {
			opened.Members = s.focus().host.seen().Members
			result = opened
		}
		return result, err
	case "session.close":
		return handle(raw, s.closeSession)
	case "session.rename":
		return handle(raw, s.rename)
	case "session.branch":
		return handle(raw, s.branch)
	case "session.access":
		return handle(raw, s.access)
	case "cron.command":
		return handle(raw, func(p CronCommandParams) (any, error) {
			reply, err := s.focus().host.CronCommand(p.Line)
			return CronCommandResult{Note: reply.Note}, err
		})
	case queryPrefix + "cron":
		return handle(raw, func(NoParams) (any, error) { return s.focus().host.cronState(), nil })
	case "turn.send":
		return handle(raw, s.send)
	case "turn.steer":
		return handle(raw, s.steer)
	case "turn.sendNow":
		return handle(raw, s.sendNow)
	case "turn.stop":
		return handle(raw, s.stop)
	case "turn.unsteer":
		return handle(raw, func(p UnsteerParams) (any, error) { return UnsteerResult{Removed: s.focus().host.Unsteer(p.Text)}, nil })
	case "shell.run":
		return handle(raw, s.shellRun)
	case "session.compact":
		return handle(raw, s.compact)
	case "session.history":
		return handle(raw, s.history)
	case "session.turns":
		return handle(raw, s.turns)
	case queryPrefix + "ledger":
		return handle(raw, s.ledgerPage)
	case "undo":
		return handle(raw, s.undo)
	case "shell.read":
		return handle(raw, s.shellRead)
	case "shell.kill":
		return handle(raw, s.shellKill)
	case "shell.takeover":
		return handle(raw, s.shellTakeOver)
	case "shell.input":
		return handle(raw, s.shellInput)
	case "shell.resize":
		return handle(raw, s.shellResize)
	case "shell.release":
		return handle(raw, s.shellRelease)
	case "label":
		return handle(raw, func(p LabelParams) (any, error) {
			return verbAs[LabelResult](s, "label", cmp.Or(p.Row, "--last"), p.Outcome)
		})
	case "settings.set":
		return handle(raw, s.setSetting)
	case "login.start":
		return handle(raw, func(p LoginParams) (any, error) {
			if !slices.Contains(AccountRole("").enum(), p.Role) {
				return nil, &Refusal{Code: CodeBadParams, Message: "role " + strconv.Quote(p.Role) + " is none of " + strings.Join(AccountRole("").enum(), ", ")}
			}
			return verbAs[LoginStarted](s, "login", p.Role, p.Provider)
		})
	case statusMethod + ".list":
		return handle(raw, s.statusList)
	case statusMethod + ".ack":
		return handle(raw, s.statusAck)
	}
	switch {
	case strings.HasPrefix(method, boardyPrefix):
		return s.boardy(method, raw)
	case strings.HasPrefix(method, scratchPrefix):
		return s.scratch(method, raw)
	}
	return s.data(method, raw)
}

func (s *server) initialize(p InitializeParams) (any, error) {
	if len(p.Versions) > 0 && !slices.Contains(p.Versions, Protocol) {
		return nil, &Refusal{Code: CodeInvalid, Message: "this tofu speaks " + Protocol + " and the client offered " + strings.Join(p.Versions, ", ")}
	}
	s.mu.Lock()
	s.client, s.ready, s.answers = p.Client, true, slices.Contains(p.Capabilities, "questions")
	s.takeover = slices.Contains(p.Capabilities, "takeover") && s.Shells != nil
	if s.Shells != nil {
		s.Shells.Terminals.Store(s.takeover)
	}
	s.mu.Unlock()
	s.Host.joinAs(memberOf(p.Client))
	return InitializeResult{Protocol: Protocol, Tofu: konst.Version, Project: s.Dir, Capabilities: capabilities()}, nil
}

func (s *server) history(p SessionHistoryParams) (any, error) {
	if p.Limit < 1 {
		return nil, &Refusal{Code: CodeBadParams, Message: "limit is the number of lines a page holds, at least 1"}
	}
	s.mu.Lock()
	lanes := s.tracked()
	var lines []HistoryLine
	if at := slices.IndexFunc(lanes, func(l *lane) bool { return l.items.session == p.Session }); at >= 0 {
		lines = lanes[at].replay
	}
	s.mu.Unlock()
	session := p.Session
	if lines == nil {
		carry, err := s.Carry(p.Session)
		if err != nil {
			return nil, err
		}
		read := newItems(carry.Session)
		lines, session = freeze(read.all(resumedChat(carry, s.Host.dir))), carry.Session
	}
	end := len(lines)
	if p.Before != nil {
		end = min(max(*p.Before, 0), end)
	}
	first := max(end-p.Limit, 0)
	return SessionHistory{Session: session, First: first, Total: len(lines), Lines: lines[first:end]}, nil
}

func freeze(lines []outgoing) []HistoryLine {
	frozen := []HistoryLine{}
	for _, line := range lines {
		var written bytes.Buffer
		encoder := json.NewEncoder(&written)
		encoder.SetEscapeHTML(false)
		if encoder.Encode(line.msg.Params) == nil {
			frozen = append(frozen, HistoryLine{Method: line.msg.Method, Params: json.RawMessage(bytes.TrimSpace(written.Bytes()))})
		}
	}
	return frozen
}

func (s *server) compact(p SessionParams) (any, error) {
	if s.Compact == nil {
		return nil, &Refusal{Code: CodeRefused, Message: "this tofu compacts no sessions"}
	}
	named, err := s.lane(p.Session)
	switch {
	case err != nil:
		return nil, err
	case named.running():
		return nil, errTurnRunning
	}
	compacted, err := s.Compact(named.host)
	if err != nil || compacted.Into == "" {
		return compacted, err
	}
	labelled := Event{Kind: EventSession, ID: compacted.Into, Root: compacted.Into}
	if store, err := session.OpenIn(s.Host.dir); err == nil {
		labelled = labelledAs(store, labelled)
	}
	s.mu.Lock()
	named.items.session, named.replay = compacted.Into, nil
	s.box.push(sessionUpdated(named.items.identity("", compacted.Into), labelled))
	s.mu.Unlock()
	safe.Go(s.report, "serve list", func() { s.listed(named) })
	s.rebuild(named)
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
	focused := s.focus()
	safe.Go(s.report, "serve list", func() { s.listed(focused) })
	return Ack{OK: true}, nil
}

func (s *server) send(p TurnSendParams) (any, error) {
	l, err := s.lane(p.Session)
	if err != nil {
		return nil, err
	}
	_, picked := l.host.Settings()
	pick, err := s.chosen(picked, p.ModelPick)
	if err != nil {
		return nil, err
	}
	task := p.Text
	for _, mention := range p.Mentions {
		s.mu.Lock()
		item, asked := l.items.loggedAs(mention)
		s.mu.Unlock()
		if item != "" {
			quoted := tools.QuoteRef(strings.TrimPrefix(asked, "#"))
			task, mention = strings.ReplaceAll(task, mention, quoted), quoted
		}
		if !strings.HasPrefix(mention, "[") {
			mention = "@" + mention
		}
		if !strings.Contains(task, mention) {
			task += " " + mention
		}
	}
	tokens, err := attach(l.host, p.Images)
	if err != nil {
		return nil, err
	}
	task += tokens
	if !l.running() {
		s.mu.Lock()
		commanding := s.command != nil
		s.mu.Unlock()
		if commanding {
			return nil, errCommandRunning
		}
		if l.host.Send(pick, task) {
			turn, _ := l.host.Turn()
			return TurnResult{Turn: turn}, nil
		}
	}
	turn, _ := l.host.Turn()
	if p.ExpectedTurnID != "" && p.ExpectedTurnID != turn {
		return nil, &Refusal{Code: CodeRefused, Message: "turn " + strconv.Quote(turn) + " is running, not " + strconv.Quote(p.ExpectedTurnID) + ", so nothing was sent"}
	}
	return TurnResult{Turn: turn, Steered: l.host.queueTyped(task)}, nil
}

func (s *server) steer(p TurnSteerParams) (any, error) {
	l, err := s.lane(p.Session)
	if err != nil {
		return nil, err
	}
	turn, running := l.host.Turn()
	if !running || turn != p.ExpectedTurnID {
		return nil, &Refusal{Code: CodeRefused, Message: "turn " + strconv.Quote(p.ExpectedTurnID) + " is not running, so nothing was steered"}
	}
	tokens, err := attach(l.host, p.Images)
	if err != nil {
		return nil, err
	}
	return SteerResult{Turn: turn, ID: l.host.Steer(p.Text + tokens)}, nil
}

func (s *server) sendNow(p SendNowParams) (any, error) {
	if focus := s.focus(); !focus.running() || !focus.host.SendNow(p.ID) {
		return nil, &Refusal{Code: CodeRefused, Message: "no turn is running with that message queued, so nothing was sent"}
	}
	return Ack{OK: true}, nil
}

func (s *server) stop(p TurnParams) (any, error) {
	l, err := s.lane(p.Session)
	if err != nil {
		l = s.focus()
	}
	turn, running := l.host.Turn()
	stopping := running && (p.Turn == "" || p.Turn == turn)
	switch {
	case stopping && p.Lead:
		l.host.StopLead(StopAsked{By: s.asker("turn.stop with lead")})
	case stopping && p.Everything:
		l.host.Stop(StopAsked{By: s.asker("turn.stop with everything"), Why: "the client asked to stop every running sub-agent", Everything: true})
	case stopping:
		l.host.Stop(StopAsked{By: s.asker("turn.stop")})
	}
	return Ack{OK: stopping || !running && s.stopCommand()}, nil
}

func (s *server) asker(method string) string {
	return "serve client " + cmp.Or(s.client, "unnamed") + ", " + method
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
	focus := s.focus()
	if focus.running() {
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
	return ShellRunResult{Output: focus.host.Ran(p.Command, output, stopped), Stopped: stopped}, nil
}

func (s *server) undo(p UndoParams) (any, error) {
	l, err := s.lane(p.Session)
	if err != nil {
		return nil, err
	}
	if l.running() {
		return nil, errTurnRunning
	}
	return verbAs[snapshot.Report](s, "undo", strconv.Itoa(max(p.Turns, 1)), "--session", p.Session)
}

func (s *server) shellRead(p ShellParams) (any, error) {
	if s.Shells == nil {
		return nil, &Refusal{Code: CodeRefused, Message: noShellRegistry}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	read := s.shells[p.Shell]
	if read == nil || read.state != shell.Running {
		read = &watchedShell{}
	}
	if err := s.follow(p.Shell, read, sys.LoadKeyRedactor().Redact); err != nil {
		return nil, err
	}
	if read != s.shells[p.Shell] {
		read.keepTail()
	}
	offset := min(max(p.Offset, read.base), read.next())
	return ShellReadResult{Shell: p.Shell, Offset: offset, Text: read.since(offset), Next: read.next()}, nil
}

func (s *server) shellKill(p ShellParams) (any, error) {
	if s.Shells == nil {
		return nil, &Refusal{Code: CodeRefused, Message: noShellRegistry}
	}
	err := s.Shells.Kill(p.Shell)
	if err == nil {
		s.focus().host.traceStop(session.Stop{Kind: session.ShellKill, By: s.asker("shell.kill"), Took: []string{p.Shell}})
	}
	return Ack{OK: true}, err
}

func endedWhileHeld(one shell.Shell) string {
	switch {
	case one.State == shell.Killed:
		return one.Name + " was killed while the person held it."
	case one.ExitCode != nil:
		return one.Name + " exited with code " + strconv.Itoa(*one.ExitCode) + " while the person held it."
	}
	return one.Name + " ended while the person held it."
}

type RefusalReason struct {
	Reason string `json:"reason"`
}

func shellRefusal(name string, err error) error {
	var refused shell.Refused
	if !errors.As(err, &refused) {
		return err
	}
	return &Refusal{Code: CodeRefused, Message: name + ": " + refused.Error(), Data: RefusalReason{Reason: string(refused)}}
}

func (s *server) shellSize(p ShellSizeParams) (shell.Size, error) {
	if s.Shells == nil {
		return shell.Size{}, &Refusal{Code: CodeRefused, Message: noShellRegistry}
	}
	if p.Shell == "" {
		return shell.Size{}, &Refusal{Code: CodeBadParams, Message: "shell is the name of a kept shell, such as bash-3"}
	}
	size, err := shell.NewSize(p.Cols, p.Rows)
	if err != nil {
		return size, &Refusal{Code: CodeBadParams, Message: err.Error()}
	}
	return size, nil
}

func (s *server) changeHolder(owner string, held ShellHeld, note, notice string) {
	s.watched(held.Shell).holder = shell.Holder(held.Holder)
	focus := s.focus()
	s.box.push(kept("shell.holder", &ShellHolderChanged{Identity: focus.items.identity(owner, held.Shell), ShellHeld: held, Note: note}))
	focus.host.Steer(notice)
	s.reportStatus(focus)
}

func (s *server) shellTakeOver(p ShellSizeParams) (any, error) {
	size, err := s.shellSize(p)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.takeover {
		return nil, &Refusal{Code: CodeRefused, Message: "this client did not declare takeover in initialize", Data: RefusalReason{Reason: "not_declared"}}
	}
	hold, err := s.Shells.TakeOver(p.Shell, size)
	if err != nil {
		hold, err = s.elsewhere(p.Shell, err, followAsk{Op: followTakeOver, Cols: p.Cols, Rows: p.Rows})
	}
	if err != nil {
		return nil, shellRefusal(p.Shell, err)
	}
	watched := s.caughtUp(p.Shell)
	held := ShellHeld{Shell: p.Shell, Holder: ShellHolder(shell.HolderPerson), Since: *hold.Shell.HeldSince, Offset: watched.next()}
	if watched.holder != shell.HolderPerson {
		s.changeHolder(hold.Shell.Owner, held, "", "The person took over "+p.Shell+" ("+sys.LoadKeyRedactor().Redact(hold.Shell.Command)+"). Do not read, wait on, stop or restart it until it is handed back.")
	}
	watched.raw = hold.Next
	return ShellTakenOver{ShellHeld: held, Cols: p.Cols, Rows: p.Rows, Base: hold.Base, Next: hold.Next, Replay: hold.Replay}, nil
}

func (s *server) shellInput(p ShellInputParams) (any, error) {
	switch {
	case s.Shells == nil:
		return nil, &Refusal{Code: CodeRefused, Message: noShellRegistry}
	case len(p.Data) == 0 || len(p.Data) > konst.ShellInputMaxBytes:
		return nil, &Refusal{Code: CodeBadParams, Message: "data is the base64 of 1 to " + strconv.Itoa(konst.ShellInputMaxBytes) + " bytes, and this decoded to " + strconv.Itoa(len(p.Data))}
	}
	err := s.Shells.Input(p.Shell, p.Data)
	if err != nil {
		_, err = s.elsewhere(p.Shell, err, followAsk{Op: followInput, Data: p.Data})
	}
	return Ack{OK: true}, shellRefusal(p.Shell, err)
}

func (s *server) shellResize(p ShellSizeParams) (any, error) {
	size, err := s.shellSize(p)
	if err != nil {
		return nil, err
	}
	if err = s.Shells.Resize(p.Shell, size); err != nil {
		_, err = s.elsewhere(p.Shell, err, followAsk{Op: followResize, Cols: p.Cols, Rows: p.Rows})
	}
	return Ack{OK: true}, shellRefusal(p.Shell, err)
}

func (s *server) elsewhere(name string, refused error, ask followAsk) (shell.Hold, error) {
	entry, err := s.Shells.Read(name)
	if err != nil {
		return shell.Hold{}, refused
	}
	for _, l := range s.tracked() {
		if hub, joined := l.host.joined(); joined && hub.PID == entry.TofuPID && hub.Session == entry.Session {
			ask.Shell = name
			return l.host.heldShell(ask)
		}
	}
	return shell.Hold{}, refused
}

func (s *server) rawCame(l *lane, raw ShellRaw) {
	s.mu.Lock()
	defer s.mu.Unlock()
	watched := s.watched(raw.Shell)
	raw.Identity = l.items.identity(watched.owner, raw.Shell)
	s.box.push(merged("shell.raw", raw.Shell, &raw))
	watched.raw = raw.Offset + int64(len(raw.Data))
}

func (s *server) shellRelease(p ShellReleaseParams) (any, error) {
	switch {
	case s.Shells == nil:
		return nil, &Refusal{Code: CodeRefused, Message: noShellRegistry}
	case utf8.RuneCountInString(p.Note) > konst.ShellHandBackNoteChars:
		return nil, &Refusal{Code: CodeBadParams, Message: "note is at most " + strconv.Itoa(konst.ShellHandBackNoteChars) + " characters"}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, err := s.Shells.HandBack(p.Shell)
	if err != nil {
		var hold shell.Hold
		hold, err = s.elsewhere(p.Shell, err, followAsk{Op: followRelease})
		entry = hold.Shell
	}
	if err != nil {
		return nil, shellRefusal(p.Shell, err)
	}
	notice := "The person handed " + p.Shell + " back."
	if p.Note != "" {
		notice += " Note from them: " + strconv.Quote(p.Note) + "."
	}
	held := ShellHeld{Shell: p.Shell, Holder: ShellHolder(shell.HolderAgent), Since: time.Now(), Offset: s.caughtUp(p.Shell).next()}
	s.changeHolder(entry.Owner, held, p.Note, notice+" It is running. Its output while they held it is in shell logs.")
	return held, nil
}

func (s *server) answered(id, result json.RawMessage) error {
	var asked string
	_ = json.Unmarshal(id, &asked)
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, l := range s.tracked() {
		open, waiting := l.host.asked(asked)
		switch {
		case waiting && open.Questions != nil:
			return s.questionAnswered(l, open, result)
		case waiting:
			return s.approvalAnswered(l, open, result)
		}
	}
	return &Refusal{Code: CodeInvalid, Message: "nothing waits on the answer " + string(id) + ": it was answered, withdrawn, or never asked"}
}

func (s *server) approvalAnswered(l *lane, asked Event, result json.RawMessage) error {
	approval := asked.ID
	var answer ApprovalAnswer
	if json.Unmarshal(result, &answer) != nil || !slices.Contains(asked.Accepts, answer.Decision) {
		accepted := make([]string, len(asked.Accepts))
		for index, decision := range asked.Accepts {
			accepted[index] = string(decision)
		}
		return &Refusal{Code: CodeBadParams, Message: "approval " + approval + " takes a decision of " + strings.Join(accepted, ", ")}
	}
	if answer.ForSeconds < 0 || answer.ForSeconds > konst.ApprovalAllowForMostSeconds || answer.ForSeconds > 0 && answer.Decision != AllowFor {
		return &Refusal{Code: CodeBadParams, Message: "approval " + approval + " takes forSeconds only with allow_for, from 1 to " + strconv.Itoa(konst.ApprovalAllowForMostSeconds)}
	}
	if !l.host.takeAsk(approval) {
		return withdrawn("approval " + approval)
	}
	answered, stands, until := true, false, time.Time{}
	switch {
	case answer.Decision == AllowFor:
		until = time.Now().Add(time.Duration(cmp.Or(answer.ForSeconds, konst.ApprovalAllowForSeconds)) * time.Second)
		answered, stands = l.host.AllowAskFor(approval, until)
	case answer.Decision == Cancelled:
		l.host.Stop(StopAsked{By: s.asker("approval " + approval + " answered cancelled"), Why: "the person cancelled the ask about " + asked.Tool})
	case asked.Tool == turn.RememberToolName:
		answered = l.host.AnswerMemory(approval, keptAs(answer.Decision))
	default:
		answered, stands = l.host.AnswerAsk(approval, personAnswerOf(answer.Decision))
	}
	if !answered {
		return s.unanswered(l, asked, "approval "+approval)
	}
	s.box.push(kept("approval.resolved", &ApprovalResolved{Identity: l.items.identity(asked.Agent, approval), Approval: approval, Decision: answer.Decision, By: s.client, Standing: stands, Until: until}))
	return nil
}

func (s *server) unanswered(l *lane, asked Event, named string) error {
	if resumed, settled := l.host.putBack(asked); settled {
		s.box.push(s.resolution(l, asked, resumed))
	}
	return withdrawn(named)
}

func withdrawn(named string) error {
	return &Refusal{Code: CodeInvalid, Message: named + " was withdrawn before this answer arrived"}
}

func (s *server) resolution(l *lane, asked, resumed Event) outgoing {
	id := l.items.identity(asked.Agent, asked.ID)
	if asked.Questions != nil {
		return kept("question.resolved", &QuestionResolved{Identity: id, Question: asked.ID, Outcome: QuestionOutcome(resumed.Text), By: resumed.Detail})
	}
	return kept("approval.resolved", &ApprovalResolved{Identity: id, Approval: asked.ID, Decision: Cancelled, By: "tofu"})
}

func (s *server) revoke(p ApprovalRevokeParams) (any, error) {
	focus := s.focus()
	gone, revoked := focus.host.Revoke(p.Target)
	if !revoked {
		return nil, &Refusal{Code: CodeInvalid, Message: "no grant stands on " + p.Target + ": it was revoked, it ran out, or it was never given"}
	}
	s.box.push(kept("approval.revoked", &ApprovalRevoked{Identity: Identity{Session: focus.host.ID(), Item: session.NewEventID()}, StandingAnswer: gone, By: s.client}))
	return gone, nil
}

func (s *server) questionAnswered(l *lane, asked Event, result json.RawMessage) error {
	named := "question " + asked.ID
	var answer QuestionAnswer
	if json.Unmarshal(result, &answer) != nil || answer.Outcome != QuestionSubmitted && answer.Outcome != QuestionCancelled || !offered(asked.Questions, answer.Answers) {
		return &Refusal{Code: CodeBadParams, Message: named + " takes submitted with answers among its options, or cancelled"}
	}
	if !l.host.takeAsk(asked.ID) {
		return withdrawn(named)
	}
	if !l.host.AnswerQuestion(asked.ID, answer, s.client) {
		return s.unanswered(l, asked, named)
	}
	s.box.push(s.resolution(l, asked, Event{Text: string(answer.Outcome), Detail: s.client}))
	return nil
}

func offered(questions []turn.PersonQuestion, answers []turn.PersonReply) bool {
	for _, reply := range answers {
		at := slices.IndexFunc(questions, func(q turn.PersonQuestion) bool { return q.ID == reply.ID })
		if at < 0 {
			return false
		}
		for _, chosen := range reply.Chosen {
			if !slices.ContainsFunc(questions[at].Options, func(o turn.PersonOption) bool { return o.Label == chosen }) {
				return false
			}
		}
	}
	return true
}

func (s *server) publish(event Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	l := s.laneOf(event.from)
	switch event.Kind {
	case EventAwaitPerson:
		if event.Questions != nil && !s.answers {
			by := cmp.Or(s.client, "this client")
			safe.Go(s.report, "serve undelivered question", func() {
				l.host.questions.answer(event.ID, answeredQuestion{QuestionAnswer{Outcome: QuestionUndelivered}, by})
			})
			return
		}
	case EventResumed:
		if event.Tool != "" {
			s.box.push(s.resolution(l, event, event))
		}
	case EventTurnEnded:
		ended := l.items.turn
		safe.Go(s.report, "serve quota", s.quota)
		safe.Go(s.report, "serve list", func() { s.listed(l) })
		safe.Go(s.report, "serve turn ended", func() { s.turnEnded(l, ended) })
		safe.Go(s.report, "serve replay", func() { s.rebuild(l) })
	case EventTurnStarted, EventForkEnd:
		safe.Go(s.report, "serve list", func() { s.listed(l) })
	case EventAccount:
		if event.Account.Reason == AccountReason(turn.AccountMoved) {
			safe.Go(s.report, "serve quota", s.quota)
		}
	}
	was := l.items.session
	lines := l.items.translate(event, time.Now())
	if l.items.session != was {
		l.replay = nil
		safe.Go(s.report, "serve replay", func() { s.rebuild(l) })
	}
	for _, out := range lines {
		if l.replay != nil && out.merge == "" && out.msg.Method != "message.delta" {
			l.replay = append(l.replay, freeze([]outgoing{out})...)
		}
		s.box.push(out)
	}
	l.status.follow(event)
	s.reportStatus(l)
}

func (s *server) quota() {
	if s.Quota == nil {
		return
	}
	windows := append([]QuotaWindow{}, s.Quota()...)
	var held []AccountNow
	if s.Accounts != nil {
		held = s.Accounts()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	focus := s.focus().items
	s.box.push(merged("quota.updated", "", &QuotaUpdated{Identity: focus.identity("", focus.turn), Windows: windows}))
	s.retryAt = time.Time{}
	for _, account := range held {
		if !account.RetryAt.IsZero() && (s.retryAt.IsZero() || account.RetryAt.Before(s.retryAt)) {
			s.retryAt = account.RetryAt
		}
		key := account.Source + " " + strconv.FormatInt(account.AccountID, 10)
		was, known := s.accounts[key]
		s.accounts[key] = account.State
		if was != account.State && (known || account.State != ConditionServing) {
			s.box.push(merged("account.state", key, &AccountStateChanged{Identity: focus.identity("", ""), AccountNow: account}))
		}
	}
	select {
	case s.requota <- struct{}{}:
	default:
	}
}

func (s *server) pollQuota(quit <-chan struct{}) {
	for {
		wait := quota.Jittered(konst.ServeQuotaPollMinutes*time.Minute, rand.Float64())
		s.mu.Lock()
		if !s.retryAt.IsZero() {
			wait = min(wait, time.Until(s.retryAt))
		}
		s.mu.Unlock()
		select {
		case <-quit:
			return
		case <-s.requota:
		case <-time.After(wait):
			s.quota()
		}
	}
}

type watchedShell struct {
	state     shell.State
	owner     string
	announced bool
	sent      int
	cursor    shell.Cursor
	stream    string
	base      int
	command   string
	exitCode  *int
	status    shellStatus
	portFrom  int
	holder    shell.Holder
	raw       int64
}

func (w *watchedShell) next() int { return w.base + len(w.stream) }

func (w *watchedShell) since(at int) string { return w.stream[min(max(at-w.base, 0), len(w.stream)):] }

func (w *watchedShell) keepTail() {
	cut := len(w.stream) - konst.ShellTailBytes
	if cut <= 0 {
		return
	}
	for cut < len(w.stream) && !utf8.RuneStart(w.stream[cut]) {
		cut++
	}
	w.stream, w.base = strings.Clone(w.stream[cut:]), w.base+cut
}

func (w *watchedShell) portPrinted() int {
	port := shell.PortInOutput(w.since(w.portFrom))
	w.portFrom = max(w.portFrom, w.base+strings.LastIndexByte(w.stream, '\n')+1)
	return port
}

func (s *server) listenersOf(found []shell.Shell) map[string]int {
	window := konst.BackgroundYieldMillis * time.Millisecond
	waiting := slices.DeleteFunc(slices.Clone(found), func(one shell.Shell) bool {
		return one.State != shell.Running || one.Kept == "" || one.Port != 0 || time.Since(one.Started) < window
	})
	if len(waiting) == 0 || time.Since(s.probed) < window {
		return nil
	}
	s.probed = time.Now()
	lookup, cancel := context.WithTimeout(context.Background(), konst.PortHolderTimeoutMillis*time.Millisecond)
	defer cancel()
	return s.Shells.Listening(lookup, waiting)
}

func (s *server) watched(name string) *watchedShell {
	if s.shells[name] == nil {
		s.shells[name] = &watchedShell{}
	}
	return s.shells[name]
}

func (s *server) caughtUp(name string) *watchedShell {
	watched := s.watched(name)
	_ = s.follow(name, watched, sys.LoadKeyRedactor().Redact)
	return watched
}

func (s *server) follow(name string, watched *watchedShell, mask func(string) string) error {
	text, err := s.Shells.Follow(name, &watched.cursor)
	text, reports := watched.status.stream.Take(text)
	for _, report := range reports {
		watched.status.program.Apply(report)
	}
	watched.stream += mask(text)
	return err
}

func (s *server) watchShells(quit <-chan struct{}) {
	if s.Shells == nil {
		return
	}
	every := time.NewTicker(konst.ServeShellPollMillis * time.Millisecond)
	defer every.Stop()
	s.scanShells(true)
	for {
		select {
		case <-quit:
			s.scanShells(false)
			return
		case <-every.C:
			s.scanShells(false)
		case <-s.Shells.Wrote():
			s.sendHeldRaw()
		}
	}
}

func (s *server) sendHeldRaw() {
	s.mu.Lock()
	defer s.mu.Unlock()
	focus := s.focus()
	for name, watched := range s.shells {
		if watched.holder == shell.HolderPerson {
			s.sendRaw(focus.items.identity(watched.owner, name), name, watched)
		}
	}
}

func (s *server) sendRaw(id Identity, name string, watched *watchedShell) {
	if data, from := s.Shells.Raw(name, watched.raw); len(data) > 0 {
		s.box.push(merged("shell.raw", name, &ShellRaw{Identity: id, Shell: name, Offset: from, Data: data}))
		watched.raw = from + int64(len(data))
	}
}

func (s *server) scanShells(first bool) {
	found, err := s.Shells.List()
	if err != nil {
		return
	}
	var ended []string
	s.mu.Lock()
	for name, watched := range s.shells {
		if watched.state == shell.Running && !slices.ContainsFunc(found, func(one shell.Shell) bool { return one.Name == name }) {
			ended = append(ended, name)
		}
	}
	s.mu.Unlock()
	for _, name := range ended {
		if one, err := s.Shells.Read(name); err == nil {
			found = append(found, one)
		}
	}
	listening := s.listenersOf(found)
	background := slices.DeleteFunc(found, shell.Shell.OneShot)
	mask := func(text string) string { return text }
	if len(background) > 0 {
		mask = sys.LoadKeyRedactor().Redact
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	focus := s.focus()
	listed := map[string]bool{}
	for _, one := range background {
		listed[one.Name] = true
		watched := s.watched(one.Name)
		watched.owner, watched.command, watched.exitCode = one.Owner, mask(one.Command), one.ExitCode
		watched.status.ranHere = watched.status.ranHere || one.State == shell.Running
		if watched.announced && watched.state != shell.Running || !watched.announced && first && one.State != shell.Running {
			watched.announced, watched.state = true, one.State
			continue
		}
		if !watched.announced && one.State == shell.Running && one.Ready == "" && time.Since(one.Started) < (konst.BackgroundReadyMillis+konst.ServeShellPollMillis)*time.Millisecond {
			continue
		}
		id := focus.items.identity(one.Owner, one.Name)
		_ = s.follow(one.Name, watched, mask)
		learned := 0
		if port := cmp.Or(listening[one.Name], watched.portPrinted()); port > 0 && one.Port == 0 && one.Kept != "" && s.Shells.SetPort(one.Name, port) == nil {
			one.Port, learned = port, port
		}
		if !watched.announced {
			s.box.push(kept("shell.started", &ShellStarted{Identity: id, Shell: one.Name, Command: mask(one.Command), PID: one.PID, StartedAt: one.Started,
				Kept: ShellKept(one.Kept), Dir: one.Dir, Port: one.Port, Ready: ShellReady(one.Ready), LeftOver: one.LeftOver(), ShellHolding: holdingOf(one), call: one.Call}))
			if first {
				watched.sent = watched.next()
			}
		}
		if learned > 0 {
			s.box.push(kept("shell.ready", &ShellListening{Identity: id, Shell: one.Name, Port: learned}))
		}
		if watched.next() > watched.sent {
			s.box.push(merged("shell.output", one.Name, &ShellOutput{Identity: id, Shell: one.Name, Offset: watched.sent, Text: watched.since(watched.sent)}))
			watched.sent = watched.next()
		}
		watched.keepTail()
		if watched.holder == shell.HolderPerson {
			s.sendRaw(id, one.Name, watched)
			if one.State != shell.Running {
				s.changeHolder(one.Owner, ShellHeld{Shell: one.Name, Holder: ShellHolder(shell.HolderAgent), Since: time.Now(), Offset: watched.next()}, "", endedWhileHeld(one))
			}
		}
		if one.State != shell.Running {
			s.box.push(kept("shell.exited", &ShellExited{Identity: id, Shell: one.Name, ExitCode: one.ExitCode, Killed: one.State == shell.Killed, EndedAt: one.Ended}))
		}
		watched.announced, watched.state = true, one.State
	}
	for name, watched := range s.shells {
		if !watched.announced {
			continue
		}
		if !listed[name] && watched.state == shell.Running {
			ended := time.Now()
			s.box.push(kept("shell.exited", &ShellExited{Identity: focus.items.identity(watched.owner, name), Shell: name, EndedAt: &ended}))
			watched.state = shell.Exited
		}
		if watched.state == shell.Running {
			continue
		}
		watched.stream, watched.base, watched.cursor = "", watched.next(), shell.Cursor{}
		if !slices.ContainsFunc(watched.records(name), func(record status.Record) bool { return !focus.status.acked[record.ID] }) {
			delete(s.shells, name)
		}
	}
	s.reportStatus(focus)
}
