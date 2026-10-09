package host

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"maps"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"tofu/internal/konst"
	"tofu/internal/llm"
	"tofu/internal/session"
	roster "tofu/internal/subagent"
	"tofu/internal/turn"
)

const (
	kilobyte            = 1024
	quotedColumns       = 32
	workingDirectory    = "the working directory"
	changeDirectory     = "cd "
	moreOfAStoredResult = "more of a stored result"
	earlierCallsHidden  = " earlier calls hidden"
	storedNote          = ", first and last part kept"
	noOutput            = "no output"
	artifactPrefix      = "artifact "
	artifactHolds       = " holds this result whole: "
	artifactUnit        = " bytes,"
	unifiedDiffHeader   = "--- "
	createdFilePrefix   = "created "
	subAgentVerdict     = "sub-agent "
	askTool             = "ask"
	messageTool         = "message"
	subAgentsTool       = "subagents"
	askAnswered         = "the orchestrator answers: "
	askAssumed          = "the orchestrator did not answer, so your default stands, assumed and not confirmed: "
	answeredReply       = "orchestrator: "
	assumedReply        = "orchestrator did not answer, assumed: "
	envOpen             = "<env>"
	envClose            = "</env>"
)

type watcher struct {
	inner     turn.Model
	decisions func() int
	spawner   *turn.SpawnTool
	held      *roster.Roster
	emit      func(Event)
	now       func() time.Time
	turnID    string
	maxSteps  int
	seen      map[string]bool
	wrote     map[string]string
	in        int
	out       int
	cacheRead int
	marks     sync.Mutex
	shows     sync.Mutex
	spent     *tokenTally
	asked     map[string][]Call
	calls     map[string][]Call
	spawns    []string
	thoughts  atomic.Int64
	stop      *leadStop
	ran       func(agent string) session.AgentRun
	ends      map[string]session.AgentRun
	parents   map[string]string
}

type tokenTally struct {
	mu sync.Mutex
	by map[string]int
}

func (t *tokenTally) add(agent string, tokens int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.by == nil {
		t.by = map[string]int{}
	}
	t.by[agent] += tokens
}

func (t *tokenTally) counts() map[string]int {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return maps.Clone(t.by)
}

type watchedSubAgent struct {
	watch *watcher
	inner turn.Model
}

func (c watchedSubAgent) Ask(ctx context.Context, request llm.Request) (llm.Decision, error) {
	return c.watch.askThrough(ctx, c.inner, request)
}

func (a *watcher) Ask(ctx context.Context, request llm.Request) (llm.Decision, error) {
	if a.stop == nil || turn.SubAgentAsking(ctx) != "" {
		return a.askThrough(ctx, a.inner, request)
	}
	ctx, release := a.stop.during(ctx)
	defer release()
	if err := ctx.Err(); err != nil {
		return llm.Decision{}, err
	}
	decision, err := a.askThrough(ctx, a.inner, request)
	if err != nil && errors.Is(context.Cause(ctx), turn.SentNow{}) {
		a.emit(Event{Kind: EventStreamReset})
		return decision, turn.SentNow{}
	}
	return decision, err
}

type leadStop struct {
	mu      sync.Mutex
	stopped bool
	cancel  context.CancelCauseFunc
}

func (l *leadStop) during(ctx context.Context) (context.Context, func()) {
	ctx, cancel := context.WithCancelCause(ctx)
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.stopped {
		cancel(nil)
	}
	l.cancel = cancel
	return ctx, func() { cancel(nil) }
}

func (l *leadStop) stop() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.stopped = true
	if l.cancel != nil {
		l.cancel(nil)
	}
}

func (l *leadStop) sendNow() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.cancel != nil {
		l.cancel(turn.SentNow{})
	}
}

func (l *leadStop) reset() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.stopped, l.cancel = false, nil
}

func (l *leadStop) listen(stops, sendNow <-chan struct{}) (quiet func()) {
	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-stops:
				l.stop()
			case <-sendNow:
				l.sendNow()
			case <-done:
				return
			}
		}
	}()
	return func() { close(done) }
}

func (a *watcher) askThrough(ctx context.Context, inner turn.Model, request llm.Request) (llm.Decision, error) {
	asker := turn.SubAgentAsking(ctx)
	for _, message := range request.Messages {
		if message.Role == llm.RoleTool {
			a.result(message, asker)
		}
	}
	thinking := a.eventID(asker, "thinking "+a.turnID+" "+strconv.FormatInt(a.thoughts.Add(1), 10))
	request.OnThinking = func(text string) {
		a.emit(Event{Kind: EventThinking, ID: thinking, Agent: asker, Text: text})
	}
	streamed := false
	request.OnRetry = func() {
		streamed = false
		a.emit(Event{Kind: EventStreamReset, ID: thinking, Agent: asker})
	}
	a.sendSubAgents()
	if asker == "" {
		a.emit(Event{Kind: EventRequesting})
	}
	inChat := asker == "" && !turn.AskedQuietly(ctx)
	if inChat {
		request.OnDelta = func(text string) {
			streamed = true
			a.emit(Event{Kind: EventTextDelta, Text: text})
		}
	}
	decision, err := inner.Ask(ctx, request)
	if err != nil {
		return decision, err
	}
	fresh := decision.PromptAccounting.FreshTokens(decision.Usage.InputTokens, decision.CacheReadTokens)
	a.marks.Lock()
	a.in += fresh
	a.out += decision.Usage.OutputTokens
	a.cacheRead += decision.CacheReadTokens
	stats := Event{Kind: EventStats, Agent: asker, Model: decision.Build, TokensIn: a.in, TokensOut: a.out, CacheRead: a.cacheRead}
	a.marks.Unlock()
	a.noteSubAgentsAsk(asker, fresh+decision.Usage.OutputTokens, decision.ToolCalls)
	if a.decisions != nil {
		stats.Decisions = a.decisions()
	}
	a.emit(stats)
	if text := strings.TrimSpace(decision.Content); text != "" && !streamed && inChat {
		a.emit(Event{Kind: EventText, Text: text})
	}
	for _, call := range decision.ToolCalls {
		a.called(call, asker)
	}
	return decision, nil
}

func (a *watcher) spawning(tool string) bool {
	return a.spawner != nil && tool == a.spawner.Name()
}

func (a *watcher) eventID(asker, call string) string {
	return session.EventIDFor(cmp.Or(asker, a.turnID), call)
}

func (a *watcher) called(call llm.ToolCall, asker string) {
	intent, detail := callIntent(call)
	id, promotes := a.eventID(asker, call.ID), a.spawning(call.Name)
	a.marks.Lock()
	if promotes {
		a.spawns = append(a.spawns, id)
	}
	a.noteWholeFile(call)
	a.marks.Unlock()
	a.emit(Event{Kind: EventToolCall, ID: id, Tool: call.Name, Text: intent, Detail: detail, Promote: promotes, Agent: asker, Args: call.Arguments})
}

func (a *watcher) result(message llm.Message, asker string) {
	killedWithNothingToShow := message.ToolOutcome == llm.ToolOutcomeAborted && message.ToolResultBytes == 0
	a.marks.Lock()
	if a.seen[message.ToolCallID] || killedWithNothingToShow {
		a.marks.Unlock()
		return
	}
	a.seen[message.ToolCallID] = true
	result := Event{
		Kind:     EventToolResult,
		ID:       a.eventID(asker, message.ToolCallID),
		Text:     resultSummary(message.Content),
		Detail:   message.Content,
		Bytes:    message.ToolResultBytes,
		ExitCode: message.ToolExitCode,
		Failed:   message.ToolOutcome.Failed(),
		Agent:    asker,
	}
	if verdict, spawned := a.verdictOf(result.ID, message.Content); spawned {
		result.Text = verdict
	}
	if reply, asked := answerOf(message.Content); asked {
		result.Text = reply
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
	a.marks.Unlock()
	ended := byteSize(message.ToolResultBytes)
	if result.Failed {
		ended = cmp.Or(result.Text, ended)
	}
	a.shows.Lock()
	for index, call := range a.asked[asker] {
		if call.ID == result.ID {
			a.asked[asker][index].Result = ended
		}
	}
	a.shows.Unlock()
	a.emit(result)
}

func (a *watcher) noteWholeFile(call llm.ToolCall) {
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

func (a *watcher) anyWorking() bool {
	return a.held != nil && slices.ContainsFunc(a.held.SubAgents(), func(agent roster.SubAgent) bool { return agent.State == roster.Working })
}

func (a *watcher) noteSubAgentsAsk(subAgent string, tokens int, calls []llm.ToolCall) {
	if subAgent == "" {
		return
	}
	a.shows.Lock()
	if a.asked == nil {
		a.asked = map[string][]Call{}
	}
	if a.spent == nil {
		a.spent = &tokenTally{}
	}
	a.spent.add(subAgent, tokens)
	for _, call := range calls {
		intent, _ := callIntent(call)
		a.asked[subAgent] = append(a.asked[subAgent], Call{ID: a.eventID(subAgent, call.ID), At: a.now(), Tool: call.Name, Text: intent})
	}
	a.shows.Unlock()
	a.sendSubAgents()
}

func (a *watcher) sendSubAgents() {
	a.readCalls()
	a.draw()
}

func (a *watcher) readCalls() {
	if a.held == nil {
		return
	}
	var rows []turn.Row
	if a.spawner != nil {
		rows = a.spawner.SubAgentRows()
	}
	agents := a.held.SubAgents()
	a.shows.Lock()
	defer a.shows.Unlock()
	if a.calls == nil {
		a.calls = map[string][]Call{}
	}
	for _, agent := range agents {
		a.calls[agent.ID] = recordedOrCalling(recordedCalls(rows, agent.ID), a.asked[agent.ID], agent.Calling, agent.CallsDropped)
	}
}

func (a *watcher) draw() {
	if subAgents := a.subAgents(); len(subAgents) > 0 {
		a.emit(Event{Kind: EventSubAgent, SubAgents: subAgents})
	}
}

func (a *watcher) subAgents() []SubAgentRow {
	if a.held == nil {
		return nil
	}
	agents := a.held.SubAgents()
	for index := range agents {
		if agents[index].State == roster.InReview && (a.spawner == nil || a.spawner.Review == nil) {
			agents[index].State = roster.Finished
		}
	}
	a.shows.Lock()
	defer a.shows.Unlock()
	rows := SubAgentRows(agents, a.now(), a.maxSteps, a.spent.counts(), func(agent roster.SubAgent) []Call { return a.calls[agent.ID] })
	if a.ran == nil {
		return rows
	}
	if a.ends == nil {
		a.ends = map[string]session.AgentRun{}
	}
	if a.parents == nil {
		a.parents = map[string]string{}
	}
	for index := range rows {
		row := &rows[index]
		parent, known := a.parents[row.Name]
		if !known {
			if run := a.ran(row.Name); run.Agent != "" {
				parent, a.parents[row.Name] = run.ParentAgent, run.ParentAgent
			}
		}
		row.Parent = parent
		if !ended(row.State) {
			delete(a.ends, row.Name)
			continue
		}
		run, read := a.ends[row.Name]
		if !read {
			run = a.ran(row.Name)
			a.ends[row.Name] = run
		}
		row.endedAs(run)
	}
	return rows
}

func (a *watcher) verdictOf(id, report string) (string, bool) {
	if !slices.Contains(a.spawns, id) {
		return "", false
	}
	for _, line := range strings.Split(report, "\n") {
		if strings.HasPrefix(line, subAgentVerdict) {
			return line, true
		}
	}
	return "", false
}

func (a *watcher) clockRunningSubAgents() (stop func()) {
	ticking, stopped := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(stopped)
		every := time.NewTicker(konst.SubAgentRedrawMillis * time.Millisecond)
		defer every.Stop()
		for {
			select {
			case <-ticking:
				return
			case <-every.C:
				if a.anyWorking() {
					a.draw()
				}
			}
		}
	}()
	return func() {
		close(ticking)
		<-stopped
	}
}

func recordedOrCalling(recorded, asked []Call, calling []string, dropped int) []Call {
	for index := range recorded {
		if at := slices.IndexFunc(asked, func(call Call) bool { return call.ID == recorded[index].ID }); at >= 0 {
			recorded[index].At = asked[at].At
		}
	}
	switch {
	case len(recorded) > 0:
		for _, call := range asked {
			if !slices.ContainsFunc(recorded, func(done Call) bool { return done.ID == call.ID }) {
				recorded = append(recorded, call)
			}
		}
		return inThePane(recorded)
	case len(asked) > 0:
		return inThePane(asked)
	}
	watched := make([]Call, len(calling))
	for index, tool := range calling {
		watched[index] = Call{Tool: tool}
	}
	return hidingEarlier(watched, dropped)
}

func hidingEarlier(kept []Call, hidden int) []Call {
	if hidden <= 0 {
		return kept
	}
	return append([]Call{{Tool: strconv.Itoa(hidden) + earlierCallsHidden}}, kept...)
}

func recordedCalls(rows []turn.Row, id string) []Call {
	spawnedBy := ""
	for _, row := range rows {
		if row.ID == id {
			spawnedBy = row.SpawnedBy
		}
	}
	var calls []Call
	for _, row := range rows {
		if row.ID != id && (spawnedBy == "" || row.SpawnedBy != spawnedBy) {
			continue
		}
		for _, step := range row.Steps {
			for _, ran := range step.ToolCalls {
				calls = append(calls, Call{ID: ran.ID, Tool: ran.Tool, Text: ran.Command, Result: cmp.Or(ran.Error, byteSize(ran.ResultBytes))})
			}
		}
	}
	return calls
}

func inThePane(calls []Call) []Call {
	if len(calls) <= konst.SubAgentCallsWatched {
		return calls
	}
	hidden := len(calls) - konst.SubAgentCallsWatched + 1
	return hidingEarlier(calls[hidden:], hidden)
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
	switch call.Name {
	case askTool:
		return text("question"), ""
	case messageTool:
		return text("to") + ": " + text("text"), ""
	case subAgentsTool:
		return "what each sub-agent is doing", ""
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
		brief, _ := fields["task"].(string)
		first, _, _ := strings.Cut(strings.TrimSpace(brief), "\n")
		return cmp.Or(text("mission"), strings.TrimSpace(first)), brief
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

func answerOf(content string) (string, bool) {
	first, _, _ := strings.Cut(content, "\n\n")
	if answer, answered := strings.CutPrefix(first, askAnswered); answered {
		return answeredReply + oneLine(answer), true
	}
	if assumed, wasAssumed := strings.CutPrefix(first, askAssumed); wasAssumed {
		return assumedReply + oneLine(assumed), true
	}
	return "", false
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
