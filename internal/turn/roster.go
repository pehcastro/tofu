package turn

import (
	"cmp"
	"context"
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

	"tofu/internal/konst"
	"tofu/internal/llm"
	"tofu/internal/session"
	"tofu/internal/shell"
	"tofu/internal/subagent"
	"tofu/internal/sys"
)

const (
	listChangeFork = "fork"
	backupsDir     = "backups"
	backupSuffix   = ".json"
	backupStamp    = "20060102-150405"
)

type subAgentBackup struct {
	ID      string            `json:"id"`
	Agent   string            `json:"agent,omitempty"`
	Model   string            `json:"model,omitempty"`
	Mission string            `json:"mission,omitempty"`
	Brief   string            `json:"brief"`
	Owns    []string          `json:"owns,omitempty"`
	Depth   int               `json:"depth"`
	Taken   time.Time         `json:"taken"`
	History []MessageRow      `json:"history"`
	Files   map[string]string `json:"files,omitempty"`
}

func (t *SpawnTool) artifactDir() (string, error) {
	if t.base.ArtifactDir != "" {
		return t.base.ArtifactDir, nil
	}
	state, err := sys.ProjectStateDir()
	return filepath.Join(state, "artifacts"), err
}

func freedWords(owns []string) string {
	if len(owns) == 0 {
		return "it held no paths"
	}
	return "its paths " + strings.Join(owns, ", ") + " are free"
}

const resumeWords = " message with text resumes it, taking its paths back if no other sub-agent holds them."

func (t *SpawnTool) release(to, why string) (Result, error) {
	if _, running := t.Inbox.find(to); running {
		return Result{}, fmt.Errorf("message refused: %s is running, and a release would leave it writing to paths it no longer holds: do stop to end it after the call it is in, or do kill to end it now", to)
	}
	agent, found := t.roster.Release(to)
	if !found {
		return Result{}, t.unknown(to)
	}
	return Result{Content: to + " " + why + ", so it is released with no run, and " + freedWords(agent.Owns) + "." + resumeWords, Command: "release " + to, SubAgent: to}, nil
}

func ownedBy(owner string, ids []string) bool {
	return slices.ContainsFunc(ids, func(id string) bool { return owner == id || strings.HasPrefix(owner, id+"-f") })
}

func (t *SpawnTool) kill(ctx context.Context, to string) (Result, error) {
	held, ran := t.Inbox.halt(to, func(held *heldSubAgent) { held.cancel() })
	agent, found := t.roster.Release(to)
	if !found {
		return Result{}, t.unknown(to)
	}
	owners := []string{to}
	if held != nil {
		owners = held.lineage()
	}
	for _, nested := range owners[1:] {
		t.roster.Release(nested)
	}
	var killed []string
	var failed []error
	if registry := ShellRegistryFrom(ctx); registry != nil {
		for _, running := range registry.Own() {
			if !ownedBy(running.Owner, owners) {
				continue
			}
			if err := registry.Kill(running.Name); err != nil && !errors.Is(err, shell.ErrNotRunning) {
				failed = append(failed, err)
				continue
			}
			killed = append(killed, running.Name)
		}
	}
	ended, shells := "was not running", "no shell of its was running"
	if ran {
		ended = "is killed and its run ends now"
	}
	if len(killed) > 0 {
		shells = "its shells " + strings.Join(killed, ", ") + " are killed"
	}
	said := to + " " + ended + ", " + shells + ", and " + freedWords(agent.Owns) + "."
	if ran {
		said += " its report, saying where it stopped, comes to you as a message."
	}
	if err := errors.Join(failed...); err != nil {
		return Result{}, fmt.Errorf("message: %s a shell of its was not killed: %w", said, err)
	}
	return Result{Content: said + resumeWords, Command: "kill " + to, SubAgent: to}, nil
}

func filesWritten(history []llm.Message) []string {
	failed := map[string]bool{}
	for _, message := range history {
		if message.Role == llm.RoleTool && message.ToolOutcome == llm.ToolOutcomeFailed {
			failed[message.ToolCallID] = true
		}
	}
	var wrote []string
	for _, message := range history {
		for _, call := range message.ToolCalls {
			if path := writtenPath(ToolCallRow{Tool: call.Name, Args: call.Arguments}); path != "" && !failed[call.ID] && !slices.Contains(wrote, path) {
				wrote = append(wrote, path)
			}
		}
	}
	return wrote
}

func (t *SpawnTool) conversationSoFar(held *heldSubAgent, running bool) ([]llm.Message, error) {
	began, err := held.conversation()
	t.Inbox.mu.Lock()
	log := held.log
	t.Inbox.mu.Unlock()
	if err != nil || !running || log == nil || t.base.Sessions == nil {
		return began, err
	}
	unread := *t.base.Sessions
	unread.ForgetRead()
	events, err := unread.Events(log.ID())
	if err != nil {
		return nil, err
	}
	ours := func(event session.Event, kind session.EventKind) bool {
		return event.Agent == held.agent.ID && event.Kind == kind
	}
	started := -1
	for i, event := range events {
		if ours(event, session.EventTurnStart) {
			started = i
		}
	}
	if started < 0 {
		return began, nil
	}
	thisRun := events[started+1:]
	since, err := subAgentHistory(&unread, []recordedSession{{id: log.ID(), events: thisRun}}, held.agent.ID)
	if err != nil || slices.ContainsFunc(thisRun, func(event session.Event) bool { return ours(event, session.EventListChange) }) {
		return since, err
	}
	return append(slices.Clone(began), since...), nil
}

func (t *SpawnTool) backup(to string) (Result, error) {
	held, running := t.Inbox.find(to)
	if held == nil {
		return Result{}, t.unknown(to)
	}
	history, err := t.conversationSoFar(held, running)
	if err != nil {
		return Result{}, fmt.Errorf("message: %s's conversation did not read back: %w", to, err)
	}
	dir, err := t.artifactDir()
	if err != nil {
		return Result{}, err
	}
	kept := subAgentBackup{ID: to, Agent: held.agent.Agent, Model: held.agent.Model, Mission: held.agent.Mission, Brief: held.agent.Brief, Owns: held.boundary.Owns(),
		Depth: held.trace.depth, Taken: t.clock(), Files: map[string]string{}}
	for _, message := range history {
		kept.History = append(kept.History, messageRowOf(message))
	}
	var gone []string
	for _, path := range filesWritten(history) {
		full := path
		if !filepath.IsAbs(full) {
			full = filepath.Join(t.Project, full)
		}
		content, err := os.ReadFile(full)
		if err != nil {
			gone = append(gone, path)
			continue
		}
		kept.Files[path] = string(content)
	}
	raw, err := json.Marshal(kept)
	if err != nil {
		return Result{}, err
	}
	name := to + "-" + kept.Taken.Format(backupStamp)
	if err := sys.WriteFile(filepath.Join(dir, backupsDir, name+backupSuffix), raw, 0o644); err != nil {
		return Result{}, fmt.Errorf("message: the backup of %s was not written: %w", to, err)
	}
	said := fmt.Sprintf("%s is backed up as %s: %d messages, and %d files it wrote as they are now. message with text and from %s resumes it from this conversation.",
		to, name, len(history), len(kept.Files), name)
	if running {
		said += " it is running, so the conversation is the one it has so far, up to its last finished call."
	}
	if len(gone) > 0 {
		said += " these files it wrote are gone, so the backup has no copy: " + strings.Join(gone, ", ")
	}
	return Result{Content: said, Command: "backup " + to, SubAgent: to}, nil
}

func (t *SpawnTool) restoreBackup(to, from string) ([]llm.Message, error) {
	if from != filepath.Base(from) || from == "." || from == ".." {
		return nil, fmt.Errorf("from names a backup the way the backup's result does, such as %s-%s, and %q is a path", to, t.clock().Format(backupStamp), from)
	}
	dir, err := t.artifactDir()
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(filepath.Join(dir, backupsDir, strings.TrimSuffix(from, backupSuffix)+backupSuffix))
	if err != nil {
		return nil, fmt.Errorf("no backup is named %s: %w", from, err)
	}
	var kept subAgentBackup
	if err := json.Unmarshal(raw, &kept); err != nil {
		return nil, fmt.Errorf("the backup %s does not read back: %w", from, err)
	}
	if kept.ID != to {
		return nil, fmt.Errorf("the backup %s is of %s, not of %s", from, kept.ID, to)
	}
	if _, running := t.Inbox.find(to); running {
		return nil, fmt.Errorf("%s is running: do stop or do kill before it resumes from a backup", to)
	}
	history := make([]llm.Message, 0, len(kept.History))
	for _, row := range kept.History {
		message, err := messageOf(row)
		if err != nil {
			return nil, fmt.Errorf("the backup %s: %w", from, err)
		}
		history = append(history, message)
	}
	agent := subagent.SubAgent{ID: kept.ID, Agent: kept.Agent, Model: kept.Model, Mission: kept.Mission, Brief: kept.Brief, Owns: kept.Owns, Started: t.clock(), State: subagent.Parked}
	if _, known := t.roster.SubAgent(to); !known {
		t.roster.Restore(agent)
	}
	t.Inbox.adopt(restoredHeld(agent, kept.Depth, nil))
	return history, nil
}

func conversationText(history []llm.Message) string {
	var text strings.Builder
	for i, message := range history {
		fmt.Fprintf(&text, "\n[%d] %s", i+1, message.Role)
		if message.ToolCallID != "" {
			text.WriteString(" " + message.ToolCallID)
		}
		text.WriteString(": " + message.Content)
		for _, call := range message.ToolCalls {
			fmt.Fprintf(&text, "\n    calls %s %s %s", call.ID, call.Name, call.Arguments)
		}
	}
	return text.String()
}

func (t *SpawnTool) read(name string) (Result, error) {
	agent, found := t.roster.SubAgent(name)
	if !found {
		return Result{}, t.unknown(name)
	}
	said := name + " is " + agent.State.String()
	if agent.Released {
		said += " and released"
	}
	said += ". its report:\n" + cmp.Or(agent.Report, "none yet")
	held, running := t.Inbox.find(name)
	if held == nil {
		return Result{Content: said + "\n\nits conversation is kept by the sub-agent that spawned it, not by you.", Command: "read " + name, SubAgent: name}, nil
	}
	history, err := t.conversationSoFar(held, running)
	if err != nil {
		return Result{}, fmt.Errorf("subagents: %s's conversation did not read back: %w", name, err)
	}
	said += "\n\nfiles it changed: " + cmp.Or(strings.Join(filesWritten(history), ", "), "none")
	said += fmt.Sprintf("\n\nits conversation, %d messages", len(history))
	if running {
		said += ", so far, since it is running"
	}
	return Result{Content: said + ":" + conversationText(history), Command: "read " + name, SubAgent: name}, nil
}

func (t *SpawnTool) failedCalls(held *heldSubAgent) []string {
	t.Inbox.mu.Lock()
	check := held.check
	t.Inbox.mu.Unlock()
	if check == nil {
		return nil
	}
	check.mu.Lock()
	defer check.mu.Unlock()
	var failed []string
	for _, step := range check.steps {
		for _, call := range step.ToolCalls {
			switch call.Outcome() {
			case llm.ToolOutcomeRan:
				failed = nil
			case llm.ToolOutcomeFailed:
				why := call.Error
				if why == "" && call.ExitCode != nil {
					why = "exit code " + strconv.Itoa(*call.ExitCode)
				}
				failed = append(failed, call.Tool+" "+call.Command+": "+why)
			case llm.ToolOutcomeUnset, llm.ToolOutcomeAborted:
			}
		}
	}
	return failed
}

func (t *SpawnTool) requestsSeen(held *heldSubAgent) []string {
	t.Inbox.mu.Lock()
	log := held.log
	t.Inbox.mu.Unlock()
	if log == nil || t.base.Sessions == nil {
		return []string{"no request of its is recorded in this session yet"}
	}
	exchanges, err := t.base.Sessions.Exchanges(log.ID())
	if err != nil {
		return []string{"its requests did not read back: " + err.Error()}
	}
	var last, failed []string
	for _, exchange := range exchanges {
		if exchange.Agent != held.agent.ID {
			continue
		}
		line := fmt.Sprintf("request %s, %s, at %s, took %s", exchange.Request, exchange.Why, exchange.At.Format(time.TimeOnly), (time.Duration(exchange.DurationMS) * time.Millisecond).Round(time.Millisecond))
		last = []string{"last " + line}
		if exchange.Error == "" {
			failed = nil
			continue
		}
		failed = append(failed, "failed "+line+": "+exchange.Error)
	}
	return append(failed, last...)
}

func (t *SpawnTool) diagnose(ctx context.Context, name string) (Result, error) {
	agent, found := t.roster.SubAgent(name)
	if !found {
		return Result{}, t.unknown(name)
	}
	held, running := t.Inbox.find(name)
	runs := "not running"
	if running {
		runs = "running"
	}
	lines := []string{fmt.Sprintf("%s is %s and %s, %d steps, the last %s ago", name, agent.State, runs, agent.Steps, t.clock().Sub(agent.Active).Round(time.Second))}
	if len(agent.Calling) > 0 {
		lines = append(lines, "its last tools: "+strings.Join(agent.Calling, ", "))
	}
	if held != nil {
		open := held.openCalls()
		if running && len(open) == 0 {
			lines = append(lines, "no tool call is open, so it waits on its model or is between steps")
		}
		for _, call := range open {
			lines = append(lines, fmt.Sprintf("open call: %s %s, for %s", call.tool, call.target, time.Since(call.since).Round(time.Second)))
		}
		for _, failure := range t.failedCalls(held) {
			lines = append(lines, "failed call since the last that worked: "+failure)
		}
		lines = append(lines, t.requestsSeen(held)...)
	}
	if registry := ShellRegistryFrom(ctx); registry != nil {
		for _, running := range registry.Own() {
			if !ownedBy(running.Owner, []string{name}) {
				continue
			}
			tail, err := registry.Tail(running.Name, konst.SubAgentDiagnoseTailLines)
			if err != nil {
				tail = "its output did not read back: " + err.Error()
			}
			lines = append(lines, fmt.Sprintf("shell %s runs %s as pid %d since %s, and its last lines are:\n%s", running.Name, running.Command, running.PID, running.Started.Format(time.TimeOnly), tail))
		}
	}
	return Result{Content: strings.Join(lines, "\n"), Command: "diagnose " + name, SubAgent: name}, nil
}

type recordedSession struct {
	id     string
	events []session.Event
}

type recordedSubAgent struct {
	agent  subagent.SubAgent
	call   string
	byLead bool
	depth  int
	state  subagent.State
	steps  int
	calls  []string
	active time.Time
	report string
}

func RestoreSubAgents(store *session.Store, id string, roster *subagent.Roster, inbox *Inbox) error {
	chain, err := sessionChain(store, id)
	if err != nil {
		return err
	}
	unread := *store
	unread.ForgetRead()
	for _, run := range recordedSubAgents(chain) {
		agentID, restored := run.agent.ID, run.agent
		restored.State, restored.Report = run.state, run.report
		roster.Restore(restored)
		roster.Stepped(agentID, run.steps, run.active, run.calls...)
		if !run.byLead {
			continue
		}
		inbox.adopt(restoredHeld(run.agent, run.depth, func() ([]llm.Message, error) {
			chain, err := sessionChain(&unread, id)
			if err != nil {
				return nil, err
			}
			return subAgentHistory(&unread, chain, agentID)
		}))
	}
	return nil
}

func restoredHeld(agent subagent.SubAgent, depth int, reload func() ([]llm.Message, error)) *heldSubAgent {
	return &heldSubAgent{agent: agent, restored: true, reload: reload, inbox: NewInbox(),
		boundary: subagent.NewBoundary(agent.ID, "", agent.Owns),
		trace:    spawnTrace{definition: agent.Agent, model: agent.Model, mission: agent.Mission, owns: agent.Owns, depth: depth}}
}

func recordedSubAgents(chain []recordedSession) []*recordedSubAgent {
	var order []*recordedSubAgent
	ran, briefs := map[string]*recordedSubAgent{}, map[string]string{}
	for _, recorded := range chain {
		var leadReports []string
		reportEvents := false
		for _, event := range recorded.events {
			run := ran[event.Agent]
			switch {
			case event.Kind == session.EventSpawn:
				var body session.SpawnBody
				if json.Unmarshal(event.Body, &body) != nil || ran[body.Agent] != nil {
					continue
				}
				ran[body.Agent] = &recordedSubAgent{call: event.Call, byLead: event.Agent == "", depth: body.Depth, state: subagent.Parked, active: event.At,
					agent: subagent.SubAgent{ID: body.Agent, Agent: body.Definition, Model: body.Model, Mission: body.Mission, Owns: body.Owns, Started: event.At}}
				order = append(order, ran[body.Agent])
			case event.Kind == session.EventToolCall:
				var call session.CallBody
				_ = json.Unmarshal(event.Body, &call)
				var args spawnArgs
				if event.Agent == "" && call.Tool == "spawn" && json.Unmarshal(call.Args, &args) == nil {
					briefs[event.Call] = args.Task
				}
				if run != nil {
					run.calls = append(run.calls, call.Tool)
				}
			case event.Kind == session.EventMessage && event.Agent == "":
				var message session.MessageBody
				if json.Unmarshal(event.Body, &message) == nil && strings.Contains(message.Origin, sourceReport) {
					leadReports = append(leadReports, message.Content)
				}
			case run == nil:
			case event.Kind == session.EventReport:
				var report session.ReportBody
				_ = json.Unmarshal(event.Body, &report)
				run.report, reportEvents = report.Text, true
			case event.Kind == session.EventAgentEnd:
				var ended session.AgentEndBody
				_ = json.Unmarshal(event.Body, &ended)
				run.state, run.active = stateAfterResume(ended.Status), event.At
			case event.Kind == session.EventRequest && event.Attempt <= session.FirstAttempt:
				run.steps, run.active = run.steps+1, event.At
			}
		}
		if !reportEvents {
			reportsWrittenBeforeReportEvents(leadReports, order)
		}
	}
	for _, run := range order {
		run.agent.Brief = briefs[run.call]
	}
	return order
}

func (t *SpawnTool) recompose(held *heldSubAgent) error {
	if !held.restored {
		return nil
	}
	definition, err := t.SubAgents.Named(held.agent.Agent)
	if err != nil {
		return err
	}
	owns := held.boundary.Owns()
	system, environment, err := t.SubAgents.prompt(t.base, definition, held.agent.Brief, owns)
	if err != nil {
		return err
	}
	if _, err := held.conversation(); err != nil {
		return fmt.Errorf("%s's conversation did not read back: %w", held.agent.ID, err)
	}
	scratch, err := t.scratch(held.agent.ID)
	if err != nil {
		return fmt.Errorf("%s's scratch folder was not made: %w", held.agent.ID, err)
	}
	held.definition, held.system, held.environment, held.restored = definition, system, environment+scratchWords(scratch), false
	held.boundary = subagent.NewBoundary(held.agent.ID, scratch, owns)
	return nil
}

func sessionChain(store *session.Store, id string) ([]recordedSession, error) {
	var chain []recordedSession
	for at := id; at != "" && !slices.ContainsFunc(chain, func(seen recordedSession) bool { return seen.id == at }); {
		events, err := store.Events(at)
		if errors.Is(err, fs.ErrNotExist) {
			break
		}
		if err != nil {
			return nil, err
		}
		header, err := store.Header(at)
		if err != nil {
			return nil, err
		}
		chain = append([]recordedSession{{id: at, events: events}}, chain...)
		at = header.Parent
	}
	return chain, nil
}

func stateAfterResume(status string) subagent.State {
	for _, state := range []subagent.State{subagent.Finished, subagent.Errored} {
		if state.String() == status {
			return state
		}
	}
	return subagent.Parked
}

func reportsWrittenBeforeReportEvents(leadReports []string, order []*recordedSubAgent) {
	for _, message := range leadReports {
		var current *recordedSubAgent
		for _, paragraph := range strings.Split(message, "\n\n") {
			for _, run := range order {
				if (strings.HasPrefix(paragraph, "sub-agent "+run.agent.ID+" is ") || strings.HasPrefix(paragraph, run.agent.ID+" ran")) && run != current {
					run.report, current = "", run
				}
			}
			if current != nil {
				current.report = strings.TrimPrefix(current.report+"\n\n"+paragraph, "\n\n")
			}
		}
	}
}

func subAgentHistory(store *session.Store, chain []recordedSession, agentID string) ([]llm.Message, error) {
	var since []session.Event
	var change session.ListChangeBody
	changedIn := ""
	for _, recorded := range chain {
		part, err := store.Part(recorded.events, agentID)
		if err != nil {
			return nil, err
		}
		since = append(since, part...)
		for i, event := range part {
			var changed session.ListChangeBody
			if event.Kind == session.EventListChange && json.Unmarshal(event.Body, &changed) == nil {
				since, change, changedIn = part[i+1:], changed, recorded.id
			}
		}
	}
	var conversation []llm.Message
	if changedIn != "" && change.Kind != listChangeFork {
		blobs, err := store.Blobs(changedIn)
		if err != nil {
			return nil, err
		}
		for _, hash := range change.After {
			var row MessageRow
			if err := json.Unmarshal(blobs[hash], &row); err != nil {
				return nil, fmt.Errorf("the message %s carried into %s's %s is not in session %s: %w", hash, agentID, change.Kind, changedIn, err)
			}
			message, err := messageOf(row)
			if err != nil {
				return nil, err
			}
			if message.Role != llm.RoleSystem {
				conversation = append(conversation, message)
			}
		}
	}
	after, err := ConversationFrom(since)
	return resumable(append(conversation, after...)), err
}
