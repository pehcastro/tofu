package turn

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"tofu/internal/llm"
	"tofu/internal/session"
	"tofu/internal/subagent"
	"tofu/internal/sys"
)

const (
	backupsDir   = "backups"
	backupSuffix = ".json"
	backupStamp  = "20060102-150405"
)

type subAgentBackup struct {
	ID      string            `json:"id"`
	Agent   string            `json:"agent,omitempty"`
	Model   string            `json:"model,omitempty"`
	Mission string            `json:"mission,omitempty"`
	Brief   string            `json:"brief"`
	Owns    []string          `json:"owns,omitempty"`
	Ticket  string            `json:"ticket,omitempty"`
	Depth   int               `json:"depth"`
	Taken   time.Time         `json:"taken"`
	History []MessageRow      `json:"history"`
	Files   map[string]string `json:"files,omitempty"`
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
	kept := subAgentBackup{ID: to, Agent: held.agent.Agent, Model: held.agent.Model, Mission: held.agent.Mission, Brief: held.agent.Brief, Owns: held.boundary.Owns(), Ticket: held.agent.Ticket,
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
	agent := subagent.SubAgent{ID: kept.ID, Agent: kept.Agent, Model: kept.Model, Mission: kept.Mission, Brief: kept.Brief, Owns: kept.Owns, Ticket: kept.Ticket, Started: t.clock(), State: subagent.Parked}
	if _, known := t.roster.SubAgent(to); !known {
		t.roster.Restore(agent)
	}
	t.Inbox.adopt(restoredHeld(agent, kept.Depth, nil))
	return history, nil
}
