package turn

import (
	"cmp"
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

type SideBranch struct {
	Access Access
	Seed   Seed
	Model  string
	Effort llm.Effort
	From   string
}

func BranchSide(store *session.Store, parent session.Header, side SideBranch, at time.Time) (session.Header, int, error) {
	events, err := store.Body(parent.ID)
	if err != nil {
		return session.Header{}, 0, err
	}
	if side.From != "" {
		upTo := slices.IndexFunc(events, func(event session.Event) bool { return event.ID == side.From })
		if upTo < 0 {
			return session.Header{}, 0, fmt.Errorf("event %q is not in session %s, so there is no point to branch from", side.From, parent.ID)
		}
		events = events[:upTo+1]
	}
	var seeded []llm.Message
	switch side.Seed {
	case SeedSummary:
		if seeded, err = sideSeed(store, parent, events); err != nil {
			return session.Header{}, 0, err
		}
	case SeedNone:
	default:
		return session.Header{}, 0, fmt.Errorf("seed %q is neither %s nor %s", side.Seed, SeedSummary, SeedNone)
	}
	id := session.NewEventID()
	log, err := store.Open(session.Header{ID: id, At: at, Root: id, Kind: session.KindSide, Owns: side.Access.Owns, Preset: side.Access.Preset, Wire: parent.Wire,
		Model: cmp.Or(side.Model, parent.Model), Effort: string(side.Effort), BranchedFrom: &session.Carried{Session: parent.ID, Event: cmp.Or(side.From, parent.Head)}})
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

func sideSeed(store *session.Store, parent session.Header, events []session.Event) ([]llm.Message, error) {
	if len(events) == 0 {
		return nil, nil
	}
	messages, err := ConversationFrom(events)
	if err != nil {
		return nil, err
	}
	recorded, err := store.Events(parent.ID)
	if err != nil {
		return nil, err
	}
	task, last := "", events[len(events)-1].ID
	for _, event := range recorded {
		var start session.TurnStart
		if event.Kind == session.EventTurnStart && event.Agent == "" && json.Unmarshal(event.Body, &start) == nil {
			task = start.Task
		}
		if event.ID == last {
			break
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
	if err != nil {
		return config, err
	}
	store, own := config.Sessions, config.Session
	boundary := subagent.NewBoundary(side.ID, "", side.Owns)
	wrap := func(registry Registry) Registry {
		if found {
			registry = sideTools(registry, boundary)
		}
		return claimedTools(registry, store, own)
	}
	config.Tools = wrap(config.Tools)
	if source := config.ToolSource; source != nil {
		config.ToolSource = func() Registry { return wrap(source()) }
	}
	if !found {
		return config, nil
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

func pathArg(tool string, raw json.RawMessage) (string, error) {
	var args struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", fmt.Errorf("%s: arguments are not the expected shape: %w", tool, err)
	}
	return args.Path, nil
}

func (t sideWrite) refusal(raw json.RawMessage) error {
	path, err := pathArg(t.Name(), raw)
	if err != nil {
		return err
	}
	err = t.boundary.Write(path)
	if errors.As(err, &subagent.DeniedError{}) {
		return SideRefusedError{Tool: t.Name(), What: path + " is left as it was", Owns: t.boundary.Owns()}
	}
	if err != nil {
		return fmt.Errorf("%s: %w", t.Name(), err)
	}
	return nil
}

func (t sideWrite) Run(ctx context.Context, raw json.RawMessage) (Result, error) {
	if err := t.refusal(raw); err != nil {
		return Result{}, err
	}
	return t.tool.Run(ctx, raw)
}

type sideShell struct {
	ownedShell
}

func (t sideShell) refusal(raw json.RawMessage) error {
	err := t.ownedShell.refusal(raw)
	var denied subagent.DeniedError
	var unread subagent.ReadListError
	switch {
	case errors.As(err, &denied):
		return SideRefusedError{Tool: t.Name(), What: denied.Path + " is left as it was", Owns: t.boundary.Owns()}
	case errors.As(err, &unread):
		return SideRefusedError{Tool: t.Name(), What: unread.Program + " " + unread.Why + ", and bash here runs only commands that read, list, search or inspect, or run the project's own checks", Owns: t.boundary.Owns()}
	}
	return err
}

func (t sideShell) Run(ctx context.Context, raw json.RawMessage) (Result, error) {
	if err := t.refusal(raw); err != nil {
		return Result{}, err
	}
	return t.tool.Run(ctx, raw)
}

type SideRefusedError struct {
	Tool string
	What string
	Owns []string
}

func (e SideRefusedError) Error() string {
	return e.Tool + " refused: " + e.What + ". This side chat " + sideWrites(e.Owns) + ": say the change in your answer, and the person hands it to the orchestrator or gives this chat more access"
}
