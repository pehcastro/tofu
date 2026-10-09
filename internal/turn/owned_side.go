package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"tofu/internal/llm"
	"tofu/internal/recall"
	"tofu/internal/session"
	"tofu/internal/subagent"
)

const (
	PresetRead  = "read"
	PresetNotes = "notes"
	PresetFiles = "files"
)

type Access struct {
	Owns   []string
	Preset string
}

func AccessOf(said string) (Access, error) {
	switch said {
	case PresetRead:
		return Access{Preset: said}, nil
	case PresetNotes:
		return Access{Owns: []string{"*.md", "*.html", "**/*.md", "**/*.html"}, Preset: said}, nil
	case PresetFiles:
		return Access{Owns: []string{"**"}, Preset: said}, nil
	}
	return OwnsAccess(strings.Split(said, ","))
}

func OwnsAccess(globs []string) (Access, error) {
	owns := make([]string, len(globs))
	for i, glob := range globs {
		owns[i] = strings.TrimSpace(glob)
		if _, err := subagent.Matches(".", owns[i:i+1]); err != nil {
			return Access{}, fmt.Errorf("side chat access %q is none of %s, %s and %s, and not a comma list of globs: %w", strings.Join(globs, ","), PresetRead, PresetNotes, PresetFiles, err)
		}
	}
	return Access{Owns: owns}, nil
}

type Seed string

const (
	SeedSummary Seed = "summary"
	SeedNone    Seed = "none"
)

func BranchSide(store *session.Store, parent session.Header, access Access, seed Seed, at time.Time) (session.Header, int, error) {
	var seeded []llm.Message
	switch seed {
	case SeedSummary:
		var err error
		if seeded, err = sideSeed(store, parent); err != nil {
			return session.Header{}, 0, err
		}
	case SeedNone:
	default:
		return session.Header{}, 0, fmt.Errorf("seed %q is neither %s nor %s", seed, SeedSummary, SeedNone)
	}
	id := session.NewEventID()
	log, err := store.Open(session.Header{ID: id, At: at, Root: id, Kind: session.KindSide, Owns: access.Owns, Preset: access.Preset, Wire: parent.Wire, Model: parent.Model,
		BranchedFrom: &session.Carried{Session: parent.ID, Event: parent.Head}})
	if err != nil {
		return session.Header{}, 0, err
	}
	written := &record{store: store, log: log, scope: id, turn: id, said: map[string]string{}}
	request := ""
	for _, message := range seeded {
		if message.Role == llm.RoleAssistant {
			request = session.NewEventID()
		}
		written.message(message, request, nil)
	}
	if len(written.failed) > 0 {
		err = errors.New(strings.Join(written.failed, "; "))
	}
	return log.Header(), len(seeded), errors.Join(err, log.Close())
}

func sideSeed(store *session.Store, parent session.Header) ([]llm.Message, error) {
	events, err := store.Body(parent.ID)
	if err != nil {
		return nil, err
	}
	messages, err := ConversationFrom(events)
	if err != nil {
		return nil, err
	}
	recorded, err := store.Events(parent.ID)
	if err != nil {
		return nil, err
	}
	task := ""
	for _, event := range recorded {
		var start session.TurnStart
		if event.Kind == session.EventTurnStart && event.Agent == "" && json.Unmarshal(event.Body, &start) == nil {
			task = start.Task
		}
	}
	if task == "" || len(messages) == 0 {
		return nil, nil
	}
	artifacts, err := NewArtifacts(filepath.Join(store.State(), "artifacts"), true)
	if err != nil {
		return nil, err
	}
	artifacts.preview = artifacts.preview.OnWire(parent.Wire)
	fork, begun, err := forkHistory(artifacts, recall.Budget{Bands: recall.ShippedBands()}, task, messages, ForkContinuation, 0, 0)
	if err != nil {
		return nil, err
	}
	carryState(fork, begun, "", parent.ID)
	begun = slices.DeleteFunc(begun, standsAtTheHead)
	at := slices.IndexFunc(begun, func(message llm.Message) bool { return message.Origin.Source == sourceForkCarry })
	_, rest, _ := strings.Cut(begun[at].Content, "\n")
	begun[at].Content = "this is a side chat beside session " + parent.ID + ", which keeps running and has not ended: " +
		"the summary below is the one tofu writes when a session forks, and every session it names is that one.\n" + rest
	return begun, nil
}

func sideChat(config Config) (Config, error) {
	if config.Sessions == nil || config.Session == "" {
		return config, nil
	}
	side, found, err := config.Sessions.Side(config.Session)
	if err != nil || !found {
		return config, err
	}
	boundary := subagent.NewBoundary(side.ID, "", side.Owns)
	config.Tools = sideTools(config.Tools, boundary)
	if source := config.ToolSource; source != nil {
		config.ToolSource = func() Registry { return sideTools(source(), boundary) }
	}
	config.Instructions = strings.TrimSpace(config.Instructions + "\n\n" + "This is a side chat beside session " + side.BranchedFrom.Session +
		", for talking with the person, not for doing that session's work: it never spawns, messages sub-agents, schedules, remembers, overrides a rule or changes a setting. " +
		"It " + sideWrites(side.Owns) + ".")
	return config, nil
}

func sideWrites(owns []string) string {
	if len(owns) == 0 {
		return "is read only: it changes no file and runs no command that changes anything"
	}
	return "writes only " + strings.Join(owns, ", ") + ", through write and edit, and every other file is read only"
}

func orchestratorOnly() []string {
	return []string{"spawn", "message", "subagents", ShellToolName, "settings", "cron", "remember", "zoom", "recall", "rule_override", "browser_act", "browser_do", "browser_motion"}
}

func sideTools(registry Registry, boundary *subagent.Boundary) Registry {
	var kept []Tool
	for _, tool := range registry.tools {
		switch name := tool.Name(); {
		case slices.Contains(orchestratorOnly(), name):
		case name == "write" || name == "edit":
			kept = append(kept, sideWrite{tool: tool, boundary: boundary})
		case name == bashToolName:
			kept = append(kept, sideShell{ownedShell{tool: tool, boundary: boundary}})
		default:
			kept = append(kept, tool)
		}
	}
	return NewRegistry(kept...)
}

type sideWrite struct {
	tool     Tool
	boundary *subagent.Boundary
}

func (t sideWrite) Name() string { return t.tool.Name() }

func (t sideWrite) Definition() llm.Tool {
	definition := t.tool.Definition()
	definition.Description += ". This side chat " + sideWrites(t.boundary.Owns())
	return definition
}

func (t sideWrite) Run(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, fmt.Errorf("%s: arguments are not the expected shape: %w", t.Name(), err)
	}
	err := t.boundary.Write(args.Path)
	if errors.As(err, &subagent.DeniedError{}) {
		return Result{}, SideRefusedError{Tool: t.Name(), What: args.Path + " is left as it was", Owns: t.boundary.Owns()}
	}
	if err != nil {
		return Result{}, fmt.Errorf("%s: %w", t.Name(), err)
	}
	return t.tool.Run(ctx, raw)
}

type sideShell struct {
	ownedShell
}

func (t sideShell) Run(ctx context.Context, raw json.RawMessage) (Result, error) {
	result, err := t.ownedShell.Run(ctx, raw)
	var denied subagent.DeniedError
	var unread subagent.ReadListError
	switch {
	case errors.As(err, &denied):
		return result, SideRefusedError{Tool: t.Name(), What: denied.Path + " is left as it was", Owns: t.boundary.Owns()}
	case errors.As(err, &unread):
		return result, SideRefusedError{Tool: t.Name(), What: unread.Program + " " + unread.Why + ", and bash here runs only commands that read, list, search or inspect, or run the project's own checks", Owns: t.boundary.Owns()}
	}
	return result, err
}

type SideRefusedError struct {
	Tool string
	What string
	Owns []string
}

func (e SideRefusedError) Error() string {
	return e.Tool + " refused: " + e.What + ". This side chat " + sideWrites(e.Owns) + ": say the change in your answer, and the person hands it to the orchestrator or gives this chat more access"
}
