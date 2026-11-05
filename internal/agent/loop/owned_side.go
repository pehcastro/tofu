package loop

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

	"tofu/internal/agent/context/recall"
	"tofu/internal/agent/subagents"
	"tofu/internal/konst"
	"tofu/internal/models/llm/core"
	"tofu/internal/store/session"
	"tofu/internal/tools/contract"
	"tofu/internal/tools/files/edit"
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
		if _, err := subagents.Matches(".", owns[i:i+1]); err != nil {
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
	Chosen *session.Chosen
	Effort core.Effort
	From   string
}

type SideOpened struct {
	Header  session.Header
	Carried int
	NoTask  bool
}

const SideNoTask = "the parent carries no task, so nothing of it is seeded"

func BranchSide(store *session.Store, parent session.Header, side SideBranch, at time.Time) (SideOpened, error) {
	events, err := store.Body(parent.ID)
	if err != nil {
		return SideOpened{}, err
	}
	if side.From != "" {
		upTo := slices.IndexFunc(events, func(event session.Event) bool { return event.ID == side.From })
		if upTo < 0 {
			return SideOpened{}, fmt.Errorf("event %q is not in session %s, so there is no point to branch from", side.From, parent.ID)
		}
		events = events[:upTo+1]
	}
	var seeded []core.Message
	noTask := false
	switch side.Seed {
	case SeedSummary:
		if seeded, noTask, err = sideSeed(store, parent, events); err != nil {
			return SideOpened{}, err
		}
	case SeedNone:
	default:
		return SideOpened{}, fmt.Errorf("seed %q is neither %s nor %s", side.Seed, SeedSummary, SeedNone)
	}
	id, chosen, model := session.NewEventID(), cmp.Or(side.Chosen, parent.Chosen), ""
	if chosen != nil {
		model = chosen.Model
	}
	log, err := store.Open(session.Header{ID: id, At: at, Root: id, Kind: session.KindSide, Owns: side.Access.Owns, Preset: side.Access.Preset, Wire: parent.Wire,
		Model: model, Chosen: chosen, Effort: cmp.Or(string(side.Effort), parent.Effort), BranchedFrom: &session.Carried{Session: parent.ID, Event: cmp.Or(side.From, parent.Head)}})
	if err != nil {
		return SideOpened{}, err
	}
	written := &record{store: store, log: log, scope: id, turn: id, said: map[string]string{}}
	request := ""
	for _, message := range seeded {
		if message.Role == core.RoleAssistant {
			request = session.NewEventID()
		}
		written.message(message, request, nil)
	}
	if len(written.failed) > 0 {
		err = errors.New(strings.Join(written.failed, "; "))
	}
	return SideOpened{Header: log.Header(), Carried: len(seeded), NoTask: noTask}, errors.Join(err, log.Close())
}

func sideSeed(store *session.Store, parent session.Header, events []session.Event) ([]core.Message, bool, error) {
	if len(events) == 0 {
		return nil, true, nil
	}
	messages, err := ConversationFrom(events)
	if err != nil {
		return nil, false, err
	}
	task, err := sideTask(store, parent, events[len(events)-1].ID)
	if err != nil || task == "" || len(messages) == 0 {
		return nil, task == "", err
	}
	artifacts, err := NewArtifacts(filepath.Join(store.State(), "artifacts"), true)
	if err != nil {
		return nil, false, err
	}
	artifacts.preview = artifacts.preview.OnWire(parent.Wire)
	budget, err := sideBudget(store, parent, artifacts.preview)
	if err != nil {
		return nil, false, err
	}
	opening := core.Message{Role: core.RoleUser, Content: task, Origin: core.Origin{Source: core.SourceForkTask}}
	fork, begun, err := forkHistory(artifacts, budget, opening, messages, ForkContinuation, 0, 0)
	if err != nil {
		return nil, false, err
	}
	carryState(fork, begun, "", parent.ID)
	begun = slices.DeleteFunc(begun, standsAtTheHead)
	at := slices.IndexFunc(begun, func(message core.Message) bool { return message.Origin.Source == core.SourceForkCarry })
	_, rest, _ := strings.Cut(begun[at].Content, "\n")
	begun[at].Content = "this is a side chat beside session " + parent.ID + ", which keeps running and has not ended: " +
		"the summary below is the one tofu writes when a session forks, and every session it names is that one.\n" + rest
	return begun, false, nil
}

func sideBudget(store *session.Store, parent session.Header, preview recall.Config) (recall.Budget, error) {
	budget, err := recall.BudgetUnder(parent.Model, 0, konst.ContextCeilingTokens)
	if err != nil {
		return budget, err
	}
	if parent.ContextCeiling > 0 {
		budget = budget.At(parent.ContextCeiling, fmt.Sprintf("the %d token ceiling session %s last ran under", parent.ContextCeiling, parent.ID))
	}
	budget.Bands = budget.Bands.Less(stepHold(preview, budget.Bands, 0))
	held, err := HeldPrefix(store, parent.ID, "")
	if err != nil {
		return budget, err
	}
	schemas, err := json.Marshal(held.tools)
	return budget.Sending(preview, string(schemas)), err
}

func sideTask(store *session.Store, parent session.Header, last string) (string, error) {
	task, generation, carried := "", parent.ID, parent.CarriedFrom
	for {
		recorded, err := store.Events(generation)
		if err != nil {
			return "", err
		}
		for _, event := range recorded {
			var start session.TurnStart
			if event.Kind == session.EventTurnStart && event.Agent == "" && json.Unmarshal(event.Body, &start) == nil {
				task = start.Task
			}
			if event.ID == last {
				break
			}
		}
		if task != "" || carried == nil {
			return task, nil
		}
		generation, last = carried.Session, carried.Event
		header, err := store.Header(generation)
		if err != nil {
			return "", err
		}
		carried = header.CarriedFrom
	}
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
	boundary := subagents.NewBoundary(side.ID, "", side.Owns)
	shellRules := ""
	if bash, offered := config.Tools.byName[bashToolName]; offered && found {
		shell := ownedShell{tool: bash, boundary: boundary}
		shellRules = " Its bash runs a command under these rules: " + strings.TrimPrefix(shell.Definition().Description, bash.Definition().Description+", and ")
	}
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
	if config.Prefix == nil {
		config.Prefix = &Prefix{}
	}
	if !config.Prefix.held {
		parent, err := HeldPrefix(store, side.BranchedFrom.Session, "")
		if err != nil {
			return config, err
		}
		*config.Prefix = *parent
	}
	config.Instructions = strings.TrimSpace(config.Instructions + "\n\n" + "This is a side chat beside session " + side.BranchedFrom.Session +
		", for talking with the person, not for doing that session's work: " + sideNever + ". " +
		"Every tool of that session is listed, so the provider can reuse its cache, and " + strings.Join(orchestratorOnly(), ", ") + " refuse when called. " +
		"It " + sideWrites(side.Owns) + "." + shellRules)
	return config, nil
}

const sideNever = "it never spawns, messages sub-agents, schedules, remembers, overrides a rule or changes a setting"

func sideWrites(owns []string) string {
	if len(owns) == 0 {
		return "is read only: it changes no file and runs no command that changes anything"
	}
	return "writes only " + strings.Join(owns, ", ") + ", through write and edit, and every other file is read only"
}

func orchestratorOnly() []string {
	return []string{"spawn", "message", "subagents", ShellToolName, "settings", "cron", "remember", "zoom", "recall", "rule_override", "browser_act", "browser_do", "browser_motion"}
}

func sideTools(registry Registry, boundary *subagents.Boundary) Registry {
	var kept []contract.Tool
	refused := func(tool contract.Tool) sideRefused { return sideRefused{tool: tool, owns: boundary.Owns()} }
	for _, tool := range registry.tools {
		spawner, spawning := tool.(*SpawnTool)
		switch name := tool.Name(); {
		case spawning:
			kept = append(kept, refused(spawner), refused(messageTool{orchestrator: spawner}), refused(subAgentsTool{orchestrator: spawner}))
		case slices.Contains(orchestratorOnly(), name):
			kept = append(kept, refused(tool))
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
	tool     contract.Tool
	boundary *subagents.Boundary
}

func (t sideWrite) Name() string { return t.tool.Name() }

func (t sideWrite) Definition() core.Tool { return t.tool.Definition() }

func (t sideWrite) refusal(raw json.RawMessage) error {
	paths, err := edit.TargetPaths(t.Name(), raw)
	if err != nil {
		return err
	}
	for _, path := range paths {
		err = t.boundary.Write(path)
		if errors.As(err, &subagents.DeniedError{}) {
			return SideRefusedError{Tool: t.Name(), What: path + " is left as it was" + edit.NoneWritten(len(paths)), Owns: t.boundary.Owns()}
		}
		if err != nil {
			return fmt.Errorf("%s: %w", t.Name(), err)
		}
	}
	return nil
}

func (t sideWrite) Run(ctx context.Context, raw json.RawMessage) (contract.Result, error) {
	if err := t.refusal(raw); err != nil {
		return contract.Result{}, err
	}
	return t.tool.Run(ctx, raw)
}

type sideShell struct {
	ownedShell
}

func (t sideShell) refusal(raw json.RawMessage) error {
	err := t.ownedShell.refusal(raw)
	var denied subagents.DeniedError
	var unread subagents.ReadListError
	switch {
	case errors.As(err, &denied):
		return SideRefusedError{Tool: t.Name(), What: denied.Path + " is left as it was", Owns: t.boundary.Owns()}
	case errors.As(err, &unread):
		return SideRefusedError{Tool: t.Name(), What: unread.Program + " " + unread.Why + ", and bash here runs only commands that read, list, search or inspect, or run the project's own checks", Owns: t.boundary.Owns()}
	}
	return err
}

func (t sideShell) Definition() core.Tool { return t.tool.Definition() }

func (t sideShell) Run(ctx context.Context, raw json.RawMessage) (contract.Result, error) {
	if err := t.refusal(raw); err != nil {
		return contract.Result{}, err
	}
	return t.tool.Run(ctx, raw)
}

type sideRefused struct {
	tool contract.Tool
	owns []string
}

func (t sideRefused) Name() string { return t.tool.Name() }

func (t sideRefused) Definition() core.Tool { return t.tool.Definition() }

func (t sideRefused) refusal(json.RawMessage) error {
	return SideRefusedError{Tool: t.Name(), What: "it is the orchestrator's, offered here only so the tool list matches the session this chat sits beside, and " + sideNever, Owns: t.owns}
}

func (t sideRefused) Run(_ context.Context, raw json.RawMessage) (contract.Result, error) {
	return contract.Result{}, t.refusal(raw)
}

type SideRefusedError struct {
	Tool string
	What string
	Owns []string
}

func (e SideRefusedError) Error() string {
	return e.Tool + " refused: " + e.What + ". This side chat " + sideWrites(e.Owns) + ": say the change in your answer, and the person hands it to the orchestrator or gives this chat more access"
}
