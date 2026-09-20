package main

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/x/term"

	"boji/interface/tui"
	"boji/interface/tui/crew"
	"boji/interface/tui/frame"
	"boji/interface/tui/session"
	"boji/interface/tui/settings"
	"boji/internal/judge/jev"
	"boji/internal/judge/ledger"
	"boji/internal/konst"
	"boji/internal/llm"
	"boji/internal/llm/cred"
	"boji/internal/llm/quota"
	sessionstore "boji/internal/session"
	"boji/internal/turn"
	"boji/internal/turn/tools"
	"boji/internal/widget"
)

const (
	noTerminal       = "boji: the app needs a terminal. with input redirected, use boji run --dir <dir> <task>"
	gateOffNote      = "gate off: no tool call is judged until boji login openrouter stores the key"
	noCredential     = "no subscription is signed in, so no model can answer"
	loginFix         = "boji login anthropic, which opens the browser; boji login codex signs in the other subscription"
	noGateKey        = "there is no openrouter key, so jev judges no tool call"
	gateKeyFix       = "boji login openrouter, which asks for the key and checks it reaches jev"
	unreadableSource = "unreadable: "
	jevName          = "jev"
	freshSessionNote = "the next task starts a new session and carries nothing from the last one"
)

const (
	kilobyte            = 1024
	quotedColumns       = 32
	workingDirectory    = "the working directory"
	changeDirectory     = "cd "
	moreOfAStoredResult = "more of a stored result"
	storedNote          = ", first and last part kept"
	noOutput            = "no output"
	artifactPrefix      = "artifact "
	artifactHolds       = " holds this result whole: "
	artifactUnit        = " bytes,"
	unifiedDiffHeader   = "--- "
	createdFilePrefix   = "created "
	placeWords          = 2
	queuedMessages      = 64
)

func appVerb(in io.Reader, out, errOut io.Writer, resumed sessionResume) int {
	file, isFile := in.(*os.File)
	if !isFile || !term.IsTerminal(file.Fd()) {
		_, _ = fmt.Fprintln(errOut, noTerminal)
		return exitUsage
	}
	dir, err := os.Getwd()
	if err != nil {
		_, _ = fmt.Fprintf(errOut, "boji: the working directory is unreadable: %v\n", err)
		return exitVerdict
	}
	note := ""
	if _, err := gateKey(); err != nil {
		note = gateOffNote
	}
	answers := make(chan tui.Answer, 1)
	steering := make(chan string, queuedMessages)
	live := newAppSession(dir, openAppWire, answers, time.Now, resumed)
	live.steer = steering
	if err := tui.Run(tui.Options{
		Repo:         filepath.Base(dir),
		Branch:       branchOf(dir),
		Note:         note,
		Requirements: appRequirements(),
		Recheck:      appRequirements,
		Login:        loginCommand(wireSubscription),
		Wires:        appWires,
		Providers:    appProviders(),
		Quota:        appQuota,
		Turn:         live.run,
		Answers:      answers,
		Steering:     steering,
		Paths:        appPaths(dir),
		ResumeHead:   live.resumeHead,
		NewSession:   live.startFresh,
	}); err != nil {
		_, _ = fmt.Fprintf(errOut, "boji: %v\n", err)
		return exitVerdict
	}
	_, _ = fmt.Fprintln(out, "boji: session ended")
	return exitOK
}

func appPaths(dir string) func() []string {
	return func() []string {
		listed, err := tools.ListPaths(dir)
		if err != nil {
			return nil
		}
		return listed
	}
}

func loginCommand(provider string) func() *exec.Cmd {
	return func() *exec.Cmd { return exec.Command(os.Args[0], "login", provider) }
}

func appWires() []tui.Wire {
	path, err := cred.Path()
	if err != nil {
		return nil
	}
	store, err := cred.Open(path)
	if err != nil {
		return nil
	}
	defer func() { _ = store.Close() }()
	var wires []tui.Wire
	for _, provider := range []cred.Provider{cred.Anthropic, cred.Codex} {
		row, present, err := store.Row(provider)
		if err != nil || !present || row.DisabledCause != "" {
			continue
		}
		selected, err := selectModel(string(provider), "")
		if err != nil {
			continue
		}
		wires = append(wires, tui.Wire{Name: string(provider), Model: selected.ID})
	}
	return wires
}

type startBlocker struct {
	label   string
	what    string
	fix     string
	command string
	run     func() *exec.Cmd
}

func startBlockers() []startBlocker {
	var blocking []startBlocker
	if len(appWires()) == 0 {
		blocking = append(blocking, startBlocker{
			label:   wireSubscription,
			what:    noCredential,
			fix:     loginFix,
			command: "boji login " + wireSubscription,
			run:     loginCommand(wireSubscription),
		})
	}
	if _, err := gateKey(); err != nil {
		blocking = append(blocking, startBlocker{
			label:   jevName,
			what:    noGateKey,
			fix:     gateKeyFix,
			command: "boji login " + openRouterName,
			run:     loginCommand(openRouterName),
		})
	}
	return blocking
}

func appRequirements() []tui.Requirement {
	blocking := startBlockers()
	needed := make([]tui.Requirement, 0, len(blocking))
	for _, blocker := range blocking {
		needed = append(needed, tui.Requirement{What: blocker.what, Fix: blocker.fix, Run: blocker.run})
	}
	return needed
}

func appProviders() []settings.Provider {
	path, err := cred.Path()
	var store *cred.Store
	if err == nil {
		store, err = cred.Open(path)
	}
	if store != nil {
		defer func() { _ = store.Close() }()
	}
	return []settings.Provider{
		subscriptionProvider(wireSubscription, cred.Anthropic, store, path, err),
		openRouterProvider(),
		jevProvider(),
		subscriptionProvider(wireCodex, cred.Codex, store, path, err),
	}
}

func subscriptionProvider(name string, provider cred.Provider, store *cred.Store, path string, openErr error) settings.Provider {
	row := settings.Provider{Name: name, Source: path}
	if openErr != nil {
		row.Fix = unreadableSource + openErr.Error()
		return row
	}
	stored, present, err := store.Row(provider)
	switch {
	case err != nil:
		row.Fix = unreadableSource + err.Error()
	case !present:
		row.Fix = "run boji login " + name
	case stored.DisabledCause != "":
		row.State = "disabled, " + stored.DisabledCause
	default:
		row.State = strings.TrimSpace("signed in " + cmp.Or(stored.Credential.Identity.Email, stored.Credential.Identity.AccountID))
	}
	return row
}

func openRouterProvider() settings.Provider {
	located, err := locateGateKey()
	row := settings.Provider{Name: openRouterName, Source: located.Path}
	if located.Source == jev.SourceEnvironment {
		row.Source = located.Name + " in the environment"
	}
	key, keyErr := gateKey()
	if err != nil || keyErr != nil {
		row.Fix = openRouterFix
		return row
	}
	row.Key = widget.Mask(key)
	return row
}

func jevProvider() settings.Provider {
	dir, err := ledger.Dir()
	if err != nil {
		return settings.Provider{Name: jevName, Fix: unreadableSource + err.Error()}
	}
	row := settings.Provider{Name: jevName, Source: dir}
	current, err := currentBuild(dir, "")
	switch {
	case err != nil:
		row.Fix = unreadableSource + err.Error()
	case current.Known:
		row.State = current.Build
	default:
		row.Fix = "no decision recorded yet"
	}
	return row
}

func branchOf(dir string) string {
	head, err := os.ReadFile(filepath.Join(dir, ".git", "HEAD"))
	if err != nil {
		return ""
	}
	return strings.TrimPrefix(strings.TrimSpace(string(head)), "ref: refs/heads/")
}

func appQuota(wire string) frame.Quota {
	results, err := pollCredentials(context.Background(), time.Now)
	if err != nil {
		return frame.Quota{}
	}
	var fullest frame.Quota
	for _, result := range results {
		if result.err != nil || result.report.Provider != quota.Provider(cmp.Or(wire, wireSubscription)) {
			continue
		}
		fullest.Label = string(result.report.Provider)
		for _, window := range result.report.Windows {
			if !window.Used.Reported || strings.Contains(window.ID, ":") || window.Used.Fraction < fullest.Fraction {
				continue
			}
			fullest = frame.Quota{
				Label:    string(result.report.Provider) + " " + window.ID,
				Fraction: window.Used.Fraction,
				Reported: true,
				ResetsAt: window.ResetsAt,
			}
		}
	}
	return fullest
}

type appWire struct {
	model   turn.Model
	spend   turn.Spend
	store   *cred.Store
	windows string
}

func openAppWire(opts runOpts) (appWire, error) {
	selected, err := chooseModel(opts)
	if err != nil {
		return appWire{}, err
	}
	model, spend, store, err := runModel(opts, selected.ID)
	return appWire{model: model, spend: spend, store: store, windows: selected.WindowText()}, err
}

func awaitPerson(emit func(tui.Event), answers <-chan tui.Answer, granted map[string]bool) turn.Person {
	return func(ctx context.Context, request turn.GateRequest, _ turn.GateDecision) (turn.PersonAnswer, error) {
		place := askedPlace(request)
		if granted[place] {
			return turn.PersonAlwaysHere, nil
		}
		emit(tui.Event{Kind: tui.EventAwaitPerson})
		defer emit(tui.Event{Kind: tui.EventResumed})
		select {
		case answered, open := <-answers:
			if !open {
				return turn.PersonDenied, errors.New("the app stopped taking answers")
			}
			switch answered {
			case tui.AlwaysHere:
				granted[place] = true
				return turn.PersonAlwaysHere, nil
			case tui.AllowedOnce:
				return turn.PersonAllowedOnce, nil
			case tui.Denied:
				return turn.PersonDenied, nil
			}
			panic("boji: unknown answer from the app")
		case <-ctx.Done():
			return turn.PersonDenied, ctx.Err()
		}
	}
}

func steered(queue <-chan string, emit func(tui.Event)) []string {
	var taken []string
	for {
		select {
		case task := <-queue:
			emit(tui.Event{Kind: tui.EventSteered, Text: task})
			taken = append(taken, task)
		default:
			return taken
		}
	}
}

func askedPlace(request turn.GateRequest) string {
	var fields struct {
		Path    string `json:"path"`
		Command string `json:"command"`
	}
	if err := json.Unmarshal(request.Args, &fields); err != nil {
		return request.Tool + " " + string(request.Args)
	}
	switch {
	case fields.Path != "":
		return request.Tool + " " + fields.Path
	case fields.Command != "":
		return request.Tool + " " + commandPlace(fields.Command)
	}
	return request.Tool + " " + string(request.Args)
}

func commandPlace(command string) string {
	words, named := make([]string, 0, placeWords), 0
	for _, word := range strings.Fields(command) {
		if strings.HasPrefix(word, "-") {
			words = append(words, word)
			continue
		}
		if named++; named <= placeWords {
			words = append(words, word)
		}
	}
	return strings.Join(words, " ")
}

type appSession struct {
	dir     string
	open    func(runOpts) (appWire, error)
	answers <-chan tui.Answer
	steer   <-chan string
	now     func() time.Time
	id      string
	carried []llm.Message
	shown   map[string]bool
	granted map[string]bool
}

func newAppSession(dir string, open func(runOpts) (appWire, error), answers <-chan tui.Answer, now func() time.Time, resumed sessionResume) *appSession {
	return &appSession{
		dir:     dir,
		open:    open,
		answers: answers,
		now:     now,
		id:      resumed.Session,
		carried: resumed.messages,
		shown:   map[string]bool{},
		granted: map[string]bool{},
	}
}

func appTurnOn(dir string, open func(runOpts) (appWire, error), answers <-chan tui.Answer, now func() time.Time, resumed sessionResume) tui.Turn {
	return newAppSession(dir, open, answers, now, resumed).run
}

func (s *appSession) startFresh() string {
	s.id, s.carried = "", nil
	return freshSessionNote
}

func (s *appSession) resumeHead() string {
	store, err := sessionstore.Open()
	if err != nil {
		return "the session store does not open, so nothing is carried: " + err.Error()
	}
	carry := continueCarry(store)
	if carry.Fresh != "" {
		return carry.Fresh
	}
	s.id, s.carried = carry.Session, carry.messages
	return "continuing " + carry.Session + ", " + strconv.Itoa(carry.Carried) + " messages from " + sessionSteps(carry.Steps)
}

func (s *appSession) run(ctx context.Context, wire, task string, emit func(tui.Event)) {
	fail := func(err error) { emit(tui.Event{Kind: tui.EventFailure, Text: err.Error()}) }
	opts := runOpts{
		dir:          s.dir,
		task:         task,
		turnID:       s.id,
		wire:         cmp.Or(wire, wireSubscription),
		toolSet:      toolSetFull,
		maxDecisions: konst.TurnMaxDecisions,
	}
	opened, err := s.open(opts)
	if opened.store != nil {
		defer func() { _ = opened.store.Close() }()
	}
	if err != nil {
		fail(err)
		return
	}
	built, builtErr := buildRunTools(s.dir, opts.toolSet)
	sessions, sessionsErr := sessionstore.Open()
	if err := cmp.Or(builtErr, sessionsErr); err != nil {
		fail(err)
		return
	}
	gate, gateErr := newToolGate(s.dir)
	if gateErr != nil {
		emit(tui.Event{Kind: tui.EventGateOff, Text: gateErr.Error()})
	}
	if gate != nil {
		gate.watch = func(tool string, gated turn.GateDecision, err error) {
			switch {
			case err != nil:
				failed := session.Decision{Tool: tool, Verdict: session.Ask, Failure: err.Error()}
				emit(tui.Event{Kind: tui.EventDecision, Decision: &failed})
			case gated.Verdict != ledger.VerdictUnset:
				decided := gateDecision(tool, gated)
				emit(tui.Event{Kind: tui.EventDecision, Decision: &decided})
			}
		}
	}

	watch := &appWatcher{inner: opened.model, gate: gate, emit: emit, now: s.now, seen: s.shown}
	config, spawner := runConfig(opts, built, watch, opened.spend, gate)
	config.History = s.carried
	if s.answers != nil {
		config.Person = awaitPerson(emit, s.answers, s.granted)
	}
	if s.steer != nil {
		config.Steering = func() []string { return steered(s.steer, emit) }
	}
	watch.spawner = spawner
	config.Step = func(step turn.StepRow) {
		if step.Occupancy == nil {
			return
		}
		emit(tui.Event{Kind: tui.EventContext, Context: frame.Context{
			Used:   step.Occupancy.Total(),
			Budget: konst.ContextCeilingTokens,
		}})
	}
	config.EndedSession = func(turn.Row) error {
		emit(tui.Event{Kind: tui.EventForkStart})
		emit(tui.Event{Kind: tui.EventForkEnd})
		return nil
	}
	config.Sessions = sessions
	row, runErr := turn.Run(ctx, config)
	stopped := errors.Is(runErr, context.Canceled)
	if runErr != nil && !stopped {
		fail(runErr)
	}
	for _, child := range childRows(spawner) {
		if writeErr := turn.WriteSession(sessions, child); writeErr != nil {
			fail(writeErr)
		}
	}
	if row.Conversation != nil {
		s.carried = turn.Sendable(row.Conversation)
	}
	if row.ID != "" {
		s.id = row.ID
		if headErr := sessions.SetHead(row.ID); headErr != nil {
			fail(headErr)
		}
	}
	outcome := row.Outcome
	if stopped {
		outcome = turn.OutcomeStopped
	}
	emit(tui.Event{
		Kind: tui.EventDone,
		Text: fmt.Sprintf("turn %s, %d steps, %d ms, quota windows %s",
			outcome, len(row.Steps), row.WallClockMS, opened.windows),
	})
}

func gateDecision(tool string, gated turn.GateDecision) session.Decision {
	decision := session.Decision{Tool: tool, Verdict: sessionVerdict(gated.Verdict)}
	for _, answer := range gated.Answers {
		decision.Answers = append(decision.Answers, sessionAnswer(answer))
	}
	if gated.Reason == nil {
		return decision
	}
	decision.Reason = session.Reason{
		Question:  gated.Reason.Question,
		Limit:     gated.Reason.Comparison,
		Threshold: gated.Reason.Threshold,
		Value:     gated.Reason.Value,
		DeadBand:  gated.Reason.DeadBand,
		RelaxedBy: gated.Reason.RelaxedBy,
		Blocked:   gated.Reason.Blocked,
	}
	if len(gated.Answers) == 0 && gated.Reason.ModeReason != nil {
		decision.Failure = *gated.Reason.ModeReason
	}
	return decision
}

func sessionVerdict(verdict ledger.Verdict) session.Verdict {
	switch verdict {
	case ledger.VerdictAllow:
		return session.Allow
	case ledger.VerdictDeny:
		return session.Deny
	case ledger.VerdictAsk, ledger.VerdictUnset:
		return session.Ask
	}
	panic("boji: unknown verdict " + string(verdict))
}

func sessionAnswer(answer ledger.Answer) session.Answer {
	out := session.Answer{Question: answer.Question, Max: 1}
	switch answer.Kind {
	case ledger.AnswerNoul:
		out.Value = answer.Noul
	case ledger.AnswerScore:
		out.Value, out.Max = answer.Score, float64(max(len(answer.Dist)-1, 1))
	case ledger.AnswerChoice:
		out.Choice = answer.Choice
		for _, slice := range answer.Dist {
			if slice.Option == answer.Choice {
				out.Value = slice.P
			}
		}
	default:
		panic("boji: unknown answer kind " + string(answer.Kind))
	}
	return out
}

type watchedChild struct {
	child   crew.Child
	call    string
	started time.Time
	rows    int
}

type appWatcher struct {
	inner    turn.Model
	gate     *toolGate
	spawner  *turn.SpawnTool
	emit     func(tui.Event)
	now      func() time.Time
	seen     map[string]bool
	wrote    map[string]string
	in       int
	out      int
	children []watchedChild
}

func (a *appWatcher) Ask(ctx context.Context, request llm.Request) (llm.Decision, error) {
	for _, message := range request.Messages {
		if message.Role != llm.RoleTool || a.seen[message.ToolCallID] {
			continue
		}
		a.seen[message.ToolCallID] = true
		result := tui.Event{
			Kind:   tui.EventToolResult,
			ID:     message.ToolCallID,
			Text:   resultSummary(message.Content),
			Bytes:  message.ToolResultBytes,
			Failed: message.ToolOutcome.Failed(),
		}
		if !result.Failed {
			if strings.HasPrefix(message.Content, unifiedDiffHeader) {
				result.Diff = message.Content
			}
			if strings.HasPrefix(message.Content, createdFilePrefix) {
				result.Created = a.wrote[message.ToolCallID]
			}
		}
		delete(a.wrote, message.ToolCallID)
		a.emit(result)
		a.childReturned(message)
	}

	decision, err := a.inner.Ask(ctx, request)
	if err != nil {
		return decision, err
	}
	a.in += decision.Usage.InputTokens
	a.out += decision.Usage.OutputTokens
	for index := len(a.children) - 1; index >= 0; index-- {
		if a.children[index].child.State == crew.Running {
			a.children[index].child.Tokens += decision.Usage.InputTokens + decision.Usage.OutputTokens
			a.sendCrew()
			break
		}
	}
	stats := tui.Event{Kind: tui.EventStats, Model: decision.Build, TokensIn: a.in, TokensOut: a.out}
	if a.gate != nil {
		stats.Decisions = a.gate.decisions
	}
	a.emit(stats)

	if text := strings.TrimSpace(decision.Content); text != "" {
		a.emit(tui.Event{Kind: tui.EventText, Text: text})
	}
	for _, call := range decision.ToolCalls {
		intent, detail := callIntent(call)
		a.emit(tui.Event{Kind: tui.EventToolCall, ID: call.ID, Tool: call.Name, Text: intent, Detail: detail})
		a.noteWholeFile(call)
		a.childStarted(call)
	}
	return decision, nil
}

func (a *appWatcher) noteWholeFile(call llm.ToolCall) {
	var args struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal(call.Arguments, &args); err != nil || args.Content == "" {
		return
	}
	if a.wrote == nil {
		a.wrote = map[string]string{}
	}
	a.wrote[call.ID] = args.Content
}

func (a *appWatcher) childStarted(call llm.ToolCall) {
	if a.spawner == nil || call.Name != a.spawner.Name() {
		return
	}
	var args struct {
		Task string   `json:"task"`
		Owns []string `json:"owns"`
	}
	if err := json.Unmarshal(call.Arguments, &args); err != nil {
		return
	}
	a.children = append(a.children, watchedChild{
		child:   crew.Child{Name: "c" + strconv.Itoa(len(a.children)+1), Owns: args.Owns, Doing: args.Task, Total: konst.TurnMaxSteps},
		call:    call.ID,
		started: a.now(),
		rows:    len(a.spawner.Children()),
	})
	a.sendCrew()
}

func (a *appWatcher) childReturned(message llm.Message) {
	for index := range a.children {
		watched := &a.children[index]
		if watched.call != message.ToolCallID {
			continue
		}
		watched.child.State, watched.child.Report = crew.Done, message.Content
		watched.child.Since = a.now().Sub(watched.started)
		for _, row := range a.spawner.Children()[watched.rows:] {
			watched.child.Steps += len(row.Steps)
			for _, step := range row.Steps {
				for _, ran := range step.ToolCalls {
					watched.child.Calls = append(watched.child.Calls, crew.Call{Tool: ran.Tool, Text: ran.Command, Result: ran.Error})
				}
			}
		}
		a.sendCrew()
		return
	}
}

func (a *appWatcher) sendCrew() {
	children := make([]crew.Child, len(a.children))
	for index, watched := range a.children {
		children[index] = watched.child
		if watched.child.State == crew.Running {
			children[index].Since = a.now().Sub(watched.started)
		}
	}
	a.emit(tui.Event{Kind: tui.EventCrew, Children: children})
}

func callIntent(call llm.ToolCall) (string, string) {
	var fields map[string]any
	if err := json.Unmarshal(call.Arguments, &fields); err != nil {
		return "", ""
	}
	text := func(key string) string {
		value, _ := fields[key].(string)
		return oneLine(value)
	}
	command, pattern, path, task := text("command"), text("pattern"), text("path"), text("task")
	switch {
	case command != "":
		return shellIntent(command), command
	case pattern != "":
		return pattern + " in " + cmp.Or(text("glob"), path, workingDirectory), ""
	case path != "":
		return path, ""
	case task != "":
		return task, ""
	case text("handle") != "":
		return moreOfAStoredResult, ""
	}
	return "", ""
}

func shellIntent(command string) string {
	segments := shellSegments(command)
	if len(segments) == 0 {
		return command
	}
	if len(segments) > 1 && strings.HasPrefix(segments[0], changeDirectory) {
		segments = segments[1:]
	}
	if len(segments) == 1 {
		return segments[0]
	}
	return segments[0] + " +" + strconv.Itoa(len(segments)-1) + " more"
}

func shellSegments(command string) []string {
	var segments []string
	flattened := strings.NewReplacer("&&", ";", "||", ";", "\n", ";").Replace(command)
	for _, part := range strings.Split(flattened, ";") {
		if trimmed := oneLine(part); trimmed != "" {
			segments = append(segments, trimmed)
		}
	}
	return segments
}

func resultSummary(content string) string {
	if size, stored := storedWhole(content); stored {
		return byteSize(size) + storedNote
	}
	trimmed := strings.TrimRight(content, "\n")
	if trimmed == "" {
		return noOutput
	}
	lines := strings.Count(trimmed, "\n") + 1
	if lines == 1 && len([]rune(trimmed)) <= quotedColumns {
		return oneLine(trimmed)
	}
	if lines == 1 {
		return "1 line, " + byteSize(len(content))
	}
	return strconv.Itoa(lines) + " lines, " + byteSize(len(content))
}

func storedWhole(content string) (int, bool) {
	head, _, _ := strings.Cut(content, "\n")
	rest, ok := strings.CutPrefix(head, artifactPrefix)
	if !ok {
		return 0, false
	}
	_, after, ok := strings.Cut(rest, artifactHolds)
	if !ok {
		return 0, false
	}
	digits, _, ok := strings.Cut(after, artifactUnit)
	if !ok {
		return 0, false
	}
	size, err := strconv.Atoi(digits)
	return size, err == nil
}

func byteSize(bytes int) string {
	if bytes < kilobyte {
		return strconv.Itoa(bytes) + " bytes"
	}
	return strconv.FormatFloat(float64(bytes)/kilobyte, 'f', 1, 64) + " KB"
}

func oneLine(text string) string {
	return strings.Join(strings.Fields(text), " ")
}
