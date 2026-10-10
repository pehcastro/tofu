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

	"tofu/internal/konst"
	"tofu/internal/llm/quota"
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

var errCommandRunning = errors.New("a shell.run command is still running: wait for it, or turn.stop stops it")

type server struct {
	ServeConfig
	box      *outbox
	mu       sync.Mutex
	items    items
	client   string
	ready    bool
	pending  map[string]ApprovalRequest
	asked    map[string]QuestionRequest
	answers  bool
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
	s := &server{ServeConfig: cfg, box: newOutbox(), pending: map[string]ApprovalRequest{}, asked: map[string]QuestionRequest{}, shells: map[string]*watchedShell{}, items: newItems(cfg.Host.ID()),
		status: statusFeed{board: status.Board{Now: time.Now}, acked: map[string]bool{}}, requota: make(chan struct{}, 1), accounts: map[string]AccountCondition{}}
	s.seen.files = readSettingsFiles(cfg.Dir)
	first := s.first()
	first.stop, first.pumped = make(chan struct{}), make(chan struct{})
	s.lanes.Store(&[]*lane{first})
	written := make(chan error, 1)
	go func() { written <- s.box.drain(cfg.Out) }()
	quit := make(chan struct{})
	var workers sync.WaitGroup
	s.pumpOn(first)
	workers.Go(func() { s.watchShells(quit) })
	workers.Go(func() { s.pollQuota(quit) })
	workers.Go(func() { s.watchBoardy(quit) })
	workers.Go(func() { s.watchSettings(quit) })
	err := s.read()
	s.stopCommand()
	lanes := s.tracked()
	s.stopTurns(lanes...)
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
		return handle(raw, func(p LedgerParams) (any, error) {
			if s.Ledger == nil {
				return nil, &Refusal{Code: CodeRefused, Message: "this tofu reads no ledger"}
			}
			report, err := s.Ledger(p)
			if err != nil {
				return nil, err
			}
			return s.withSubjects(report)
		})
	case "undo":
		return handle(raw, s.undo)
	case "shell.read":
		return handle(raw, s.shellRead)
	case "shell.kill":
		return handle(raw, s.shellKill)
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
	s.mu.Unlock()
	return InitializeResult{Protocol: Protocol, Tofu: konst.Version, Project: s.Dir, Capabilities: capabilities()}, nil
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
	named.items.session = compacted.Into
	s.box.push(sessionUpdated(named.items.identity("", compacted.Into), labelled))
	s.mu.Unlock()
	go s.listed(named)
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
	go s.listed(s.focus())
	return Ack{OK: true}, nil
}

func busy(err error) error {
	var held session.BusyError
	if errors.As(err, &held) {
		return &Refusal{Code: CodeSessionBusy, Message: "session.busy", Data: held}
	}
	return err
}

func (s *server) send(p TurnSendParams) (any, error) {
	l, err := s.lane(p.Session)
	if err != nil {
		return nil, err
	}
	if l.running() {
		return nil, errTurnRunning
	}
	s.mu.Lock()
	commanding := s.command != nil
	s.mu.Unlock()
	if commanding {
		return nil, errCommandRunning
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
	if !l.host.Send(pick, task+tokens) {
		return nil, errTurnRunning
	}
	turn, _ := l.host.Turn()
	return TurnResult{Turn: turn}, nil
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
	return SteerResult{Turn: turn, ID: l.host.Steer(p.Text)}, nil
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
		l.host.StopLead()
	case stopping:
		l.host.Stop()
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

func (s *server) answered(id, result json.RawMessage) error {
	var approval string
	_ = json.Unmarshal(id, &approval)
	s.mu.Lock()
	defer s.mu.Unlock()
	lanes := s.tracked()
	at := slices.IndexFunc(lanes, func(l *lane) bool {
		_, asked := l.asked[approval]
		_, pending := l.pending[approval]
		return asked || pending
	})
	if at < 0 {
		return &Refusal{Code: CodeInvalid, Message: "nothing waits on the answer " + string(id) + ": it was answered, withdrawn, or never asked"}
	}
	l := lanes[at]
	if asked, open := l.asked[approval]; open {
		return s.questionAnswered(l, asked, result)
	}
	asked := l.pending[approval]
	var answer ApprovalAnswer
	if json.Unmarshal(result, &answer) != nil || !slices.Contains(asked.Decisions, answer.Decision) {
		accepted := make([]string, len(asked.Decisions))
		for index, decision := range asked.Decisions {
			accepted[index] = string(decision)
		}
		return &Refusal{Code: CodeBadParams, Message: "approval " + approval + " takes a decision of " + strings.Join(accepted, ", ")}
	}
	answered, stands := true, false
	switch {
	case answer.Decision == Cancelled:
		l.host.Stop()
	case asked.Tool == turn.RememberToolName:
		answered = l.host.AnswerMemory(approval, keptAs(answer.Decision))
	default:
		answered, stands = l.host.AnswerAsk(approval, personAnswerOf(answer.Decision))
	}
	if !answered {
		return &Refusal{Code: CodeInvalid, Message: "approval " + approval + " was withdrawn before this answer arrived"}
	}
	delete(l.pending, approval)
	s.box.push(kept("approval.resolved", &ApprovalResolved{Identity: asked.Identity, Approval: approval, Decision: answer.Decision, By: s.client, Standing: stands}))
	return nil
}

func (s *server) questionAnswered(l *lane, asked QuestionRequest, result json.RawMessage) error {
	var answer QuestionAnswer
	if json.Unmarshal(result, &answer) != nil || answer.Outcome != QuestionSubmitted && answer.Outcome != QuestionCancelled || !offered(asked.Questions, answer.Answers) {
		return &Refusal{Code: CodeBadParams, Message: "question " + asked.Question + " takes submitted with answers among its options, or cancelled"}
	}
	if !l.host.AnswerQuestion(asked.Question, answer, s.client) {
		return &Refusal{Code: CodeInvalid, Message: "question " + asked.Question + " was withdrawn before this answer arrived"}
	}
	delete(l.asked, asked.Question)
	s.box.push(kept("question.resolved", &QuestionResolved{Identity: asked.Identity, Question: asked.Question, Outcome: answer.Outcome, By: s.client}))
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
			go l.host.AnswerQuestion(event.ID, QuestionAnswer{Outcome: QuestionUndelivered}, cmp.Or(s.client, "this client"))
			return
		}
		if event.Questions != nil {
			l.asked[event.ID] = questionRequest(l.items.identity(event.Agent, event.ID), event)
			break
		}
		l.pending[event.ID] = approvalRequest(l.items.identity(event.Agent, event.ID), event)
	case EventResumed:
		if asked, open := l.asked[event.ID]; open {
			delete(l.asked, event.ID)
			s.box.push(kept("question.resolved", &QuestionResolved{Identity: asked.Identity, Question: event.ID, Outcome: QuestionOutcome(event.Text), By: event.Detail}))
		}
		if asked, pending := l.pending[event.ID]; pending {
			delete(l.pending, event.ID)
			s.box.push(kept("approval.resolved", &ApprovalResolved{Identity: asked.Identity, Approval: event.ID, Decision: Cancelled, By: "tofu"}))
		}
	case EventTurnEnded:
		go s.quota()
		go s.listed(l)
		go s.turnEnded(l, l.items.turn)
	case EventTurnStarted, EventForkEnd:
		go s.listed(l)
	case EventAccount:
		if event.Account.Reason == AccountReason(turn.AccountMoved) {
			go s.quota()
		}
	}
	for _, out := range l.items.translate(event, time.Now()) {
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
	command   string
	exitCode  *int
	status    shellStatus
	portFrom  int
}

func (w *watchedShell) portPrinted() int {
	port := shell.PortInOutput(w.stream[w.portFrom:])
	w.portFrom = max(w.portFrom, strings.LastIndexByte(w.stream, '\n')+1)
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
	if err != nil {
		return
	}
	mask, listening := sys.LoadKeyRedactor().Redact, s.listenersOf(found)
	s.mu.Lock()
	defer s.mu.Unlock()
	focus := s.focus()
	listed := map[string]bool{}
	for _, one := range slices.DeleteFunc(found, shell.Shell.OneShot) {
		listed[one.Name] = true
		watched := s.watched(one.Name)
		watched.owner, watched.command, watched.exitCode = one.Owner, mask(one.Command), one.ExitCode
		watched.status.ranHere = watched.status.ranHere || one.State == shell.Running
		if watched.announced && watched.state != shell.Running || !watched.announced && first && one.State != shell.Running {
			watched.announced, watched.state = true, one.State
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
				Kept: ShellKept(one.Kept), Dir: one.Dir, Port: one.Port, Ready: ShellReady(one.Ready), LeftOver: one.LeftOver(), call: one.Call}))
			if first {
				watched.sent = len(watched.stream)
			}
		}
		if learned > 0 {
			s.box.push(kept("shell.ready", &ShellListening{Identity: id, Shell: one.Name, Port: learned}))
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
	for name, watched := range s.shells {
		if listed[name] || !watched.announced || watched.state != shell.Running {
			continue
		}
		ended := time.Now()
		s.box.push(kept("shell.exited", &ShellExited{Identity: focus.items.identity(watched.owner, name), Shell: name, EndedAt: &ended}))
		watched.state = shell.Exited
	}
	s.reportStatus(focus)
}
