package loop

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strings"
	"time"

	"tofu/internal/agent/context/recall"
	"tofu/internal/agent/hooks"
	"tofu/internal/agent/subagents"
	"tofu/internal/konst"
	"tofu/internal/librarian/rules"
	skill "tofu/internal/librarian/skills"
	"tofu/internal/models/llm/catalog"
	"tofu/internal/models/llm/core"
	"tofu/internal/platform/credential"
	"tofu/internal/platform/project"
	"tofu/internal/project/boardy"
	"tofu/internal/store/session"
	"tofu/internal/tools/contract"
	"tofu/library/changelog"
)

type Model interface {
	Ask(ctx context.Context, request core.Request) (core.Decision, error)
}

type runningModelKey struct{}

type RunningModel struct{}

func (RunningModel) Ask(ctx context.Context, request core.Request) (core.Decision, error) {
	model, running := ctx.Value(runningModelKey{}).(Model)
	if !running {
		return core.Decision{}, errors.New("no turn is running, so there is no turn model to ask")
	}
	return model.Ask(ctx, request)
}

type Caps struct {
	MaxSteps         int
	MaxForks         int
	LoopGuardRepeats int
	LoopGuardWindow  int
	WallClock        time.Duration
}

type CalledAsTheStepIsRecordedAndBeforeTheNextOneIsAsked func(StepRow)

type CalledAsEachToolCallAnswersAndBeforeTheNextRequest func(core.Message)

type AccountReason string

const (
	AccountPicked AccountReason = "picked"
	AccountMoved  AccountReason = "moved"
)

type AccountTaken struct {
	ID     int64
	From   int64
	Reason AccountReason
}

type CalledWhenTheTurnStartsSpendingAnAccount func(AccountTaken)

func (c Config) tookAccount(taken AccountTaken) {
	if c.AccountTaken != nil && taken.ID != 0 {
		c.AccountTaken(taken)
	}
}

type Config struct {
	Model           Model
	Accounts        Accounts
	AccountTaken    CalledWhenTheTurnStartsSpendingAnAccount
	Spend           Spend
	Tools           Registry
	ToolSource      func() Registry
	Gate            Gate
	GateMode        GateMode
	Proxy           *CommandProxy
	Boundary        *subagents.Boundary
	Person          Person
	GatePromptAsks  func() bool
	Task            string
	Notes           []string
	TaskOrigin      core.Origin
	Arrived         []core.Message
	Images          []core.Image
	History         []core.Message
	Wire            string
	Slug            string
	PromptCache     catalog.PromptCache
	SpawnedFrom     string
	AgentType       string
	Project         string
	SessionSource   hooks.SessionSource
	System          string
	Parts           []PromptPart
	References      map[string]string
	Skills          []skill.Skill
	Environment     string
	Instructions    string
	Memory          string
	Plan            func() string
	Prefix          *Prefix
	ImagesOf        func(said string) []core.Image
	Caps            Caps
	Sift            *ShellSift
	ResultBytesCap  int
	ArtifactDir     string
	Places          project.Places
	Redactor        *credential.KeyRedactor
	TruncateResults bool
	NoCompaction    bool
	NoFork          bool
	NoLastWord      bool
	Budget          recall.Budget
	Sessions        *session.Store
	Session         string
	Drops           *DropIndex
	Board           *boardy.Managed
	TicketGrant     func(board boardy.Managed, id, lead, agent string) ([]string, error)
	TicketTools     func(board boardy.Managed, actor string) ([]contract.Tool, error)
	Log             *session.Log
	Turn            string
	SpawnedBy       string
	Steering        func() []string
	Inbox           *Inbox
	Step            CalledAsTheStepIsRecordedAndBeforeTheNextOneIsAsked
	ToolResult      CalledAsEachToolCallAnswersAndBeforeTheNextRequest
	Appended        func(session.Event)
	Sent            CalledAsEachRequestIsSent
	EndedSession    func(Row) error
	Notify          func(string)
	Now             func() time.Time
	NewID           func() string
}

func (c Config) ResolvedPlaces() (project.Places, error) {
	if c.Places != (project.Places{}) {
		return c.Places, nil
	}
	return project.ResolvePlaces(".")
}

const (
	environmentBeforeTheTask = "<env>"
	TheTaskFollows           = "\n\n[the task]\n"
	taskItemsApart           = "\n\n"
	NotesAfterTheTask        = "\n\n[tofu's notes on this task, not the person's words]\n"
)

func (c Config) upgradeNote() string {
	if c.Sessions == nil || c.Session == "" || c.SpawnedFrom != "" {
		return ""
	}
	last, seen := "", map[string]bool{}
	for id := c.Session; last == "" && id != "" && !seen[id]; {
		seen[id] = true
		_ = c.Sessions.EachEvent(id, func(agent string) bool { return agent == "" }, func(event session.Event) bool {
			var start session.TurnStart
			if event.Kind == session.EventTurnStart && json.Unmarshal(event.Body, &start) == nil {
				last = start.Tofu
			}
			return true
		})
		header, err := c.Sessions.Header(id)
		id = ""
		if err == nil && header.CarriedFrom != nil {
			id = header.CarriedFrom.Session
		}
	}
	if last == "" || last == konst.Version {
		return ""
	}
	changed := changelog.Between(changelog.Markdown, last, konst.Version, konst.UpgradeNoteBytes)
	if changed == "" {
		return ""
	}
	return "tofu was upgraded from " + last + " to " + konst.Version + " since this session's last turn. what changed, newest first:\n\n" + changed
}

func (c Config) FirstUserMessage(notes ...string) string {
	first := c.Task
	if c.Environment != "" {
		first = c.Environment + TheTaskFollows + c.Task
	}
	return first + notesOn(slices.Concat(c.Notes, notes))
}

func (c Config) personSaid(session string, conversation []core.Message, budget int) []string {
	var recent []string
	size := 0
	room := func(said []string) bool {
		for _, words := range slices.Backward(said) {
			switch {
			case len(recent) > 0 && words == recent[len(recent)-1]:
			case len(recent) > 0 && size+len(words) > budget:
				return false
			default:
				recent, size = append(recent, words), size+len(words)
			}
		}
		return len(recent) == 0 || size < budget
	}
	if room(personWords(conversation)) && c.Sessions != nil && session != "" {
		_, _ = cmp.Or(c.Inbox, &Inbox{}).saidNewestFirst(c.Sessions, session, func(said []PersonSaid) bool {
			words := make([]string, len(said))
			for at, one := range said {
				words[at] = strings.TrimSpace(one.Words)
			}
			return room(words)
		})
	}
	slices.Reverse(recent)
	return recent
}

func (c Config) lastSaid(session string, conversation []core.Message) string {
	said := c.personSaid(session, conversation, 0)
	if len(said) == 0 {
		return ""
	}
	return said[len(said)-1]
}

func notesOn(notes []string) string {
	notes = slices.DeleteFunc(notes, func(note string) bool { return strings.TrimSpace(note) == "" })
	if len(notes) == 0 {
		return ""
	}
	return NotesAfterTheTask + strings.Join(notes, "\n\n")
}

func (c Config) taskOrigin(started time.Time) core.Origin {
	task := c.TaskOrigin
	switch {
	case task.Source != "" || len(c.Arrived) > 0:
	case c.SpawnedFrom != "":
		task.Source = core.SourceBrief
	default:
		task.Source = core.SourceTask
	}
	task.TakenAt = started
	return task
}

func (c Config) forkOpening(task core.Origin) core.Message {
	if task.Kind() == core.OriginPerson {
		task.Source = core.SourceForkTask
	}
	if len(c.Arrived) == 0 || c.environmentInHistory() {
		return core.Message{Role: core.RoleUser, Content: c.FirstUserMessage(), Origin: task}
	}
	return core.Message{Role: core.RoleUser, Content: c.Task, Origin: task}
}

func (c Config) environmentInHistory() bool {
	return c.SpawnedFrom != "" && len(c.History) > 0
}

func (c Config) opening(task core.Origin, notes ...string) []core.Message {
	if c.environmentInHistory() {
		c.Environment = ""
	}
	if len(c.Arrived) == 0 {
		return []core.Message{{Role: core.RoleUser, Content: c.FirstUserMessage(notes...), Images: c.Images, Origin: task}}
	}
	var opened []core.Message
	if framing := strings.TrimSpace(c.Environment + notesOn(slices.Concat(c.Notes, notes))); framing != "" {
		opened = append(opened, core.Message{Role: core.RoleUser, Content: framing, Origin: core.Origin{Source: core.SourceEnvironment, TakenAt: task.TakenAt}})
	}
	for _, item := range c.Arrived {
		item.Origin.TakenAt = task.TakenAt
		opened = append(opened, item)
	}
	return opened
}

func environmentBefore(messages []core.Message) string {
	if len(messages) == 0 || messages[len(messages)-1].Origin.Source != core.SourceEnvironment {
		return ""
	}
	environment, _, _ := strings.Cut(messages[len(messages)-1].Content, NotesAfterTheTask)
	if !strings.HasPrefix(environment, environmentBeforeTheTask) {
		return ""
	}
	return environment
}

type Prefix struct {
	held     bool
	first    heldPrefix
	tools    []core.Tool
	offered  []core.Tool
	inForce  []keptPart
	appended []keptPart
	removed  []keptPart
	unloaded []keptPart
	sent     messagesKept

	memoryDropped bool
}

func (p *Prefix) memoryTurnedBackOn(fresh, was []core.Message) bool {
	if p == nil {
		return false
	}
	back := p.memoryDropped && len(was) == 0 && len(fresh) > 0
	p.memoryDropped = len(fresh) == 0 && (len(was) > 0 || p.memoryDropped)
	return back
}

type heldPrefix struct {
	head, instructions, system string
	parts                      []keptPart
	changed                    string
	composedNow                bool
}

func HeldPrefix(store *session.Store, id, agent string) (*Prefix, error) {
	events, err := store.Events(id)
	if errors.Is(err, fs.ErrNotExist) {
		return &Prefix{}, nil
	}
	if err != nil {
		return nil, err
	}
	var prompts []session.PromptBody
	for _, event := range events {
		if event.Kind != session.EventPrompt || event.Agent != agent {
			continue
		}
		var prompt session.PromptBody
		if err := json.Unmarshal(event.Body, &prompt); err != nil {
			return nil, err
		}
		prompts = append(prompts, prompt)
	}
	if len(prompts) == 0 {
		header, err := store.Header(id)
		if err != nil || header.CarriedFrom == nil {
			return &Prefix{}, err
		}
		from := header.CarriedFrom.Session
		if _, err := store.Header(from); err != nil {
			return &Prefix{}, fmt.Errorf("%s carries on from %s, which was not read: %w", id, from, err)
		}
		return HeldPrefix(store, from, agent)
	}
	blobs, err := store.Blobs(id)
	if err != nil {
		return nil, err
	}
	first, last := prompts[0], prompts[len(prompts)-1]
	tools, err := heldTools(prompts)
	if err != nil {
		return nil, err
	}
	offered, err := toldTools(events, agent, tools)
	if err != nil {
		return nil, err
	}
	held := &Prefix{held: true, tools: tools, offered: offered, first: heldPrefix{head: first.System, instructions: first.Instructions, system: Config{System: first.System, Instructions: first.Instructions}.SystemMessage()}}
	for _, recorded := range last.Parts {
		part := keptPart{PromptPart: recorded}
		if part.Sent() {
			_ = json.Unmarshal(blobs[part.Blob], &part.text)
		}
		switch why, appended := strings.CutSuffix(part.Why, PartAppendedWhy); {
		case part.Why == partRemovedWhy:
			held.removed = append(held.removed, part)
			held.inForce = slices.DeleteFunc(held.inForce, part.samePart)
		case appended:
			held.appended = append(held.appended, part)
			part.PerTurn, part.Why = false, strings.TrimSuffix(why, "; ")
			held.inForce = append(slices.DeleteFunc(held.inForce, part.samePart), part)
		case inTheSystem(part):
			held.first.parts = append(held.first.parts, part)
			held.inForce = append(held.inForce, part)
		case unloadedSkill(part):
			held.unloaded = append(held.unloaded, part)
		}
	}
	return held, nil
}

func heldTools(prompts []session.PromptBody) ([]core.Tool, error) {
	var held []core.Tool
	for i, prompt := range slices.Backward(prompts) {
		var sent []core.Tool
		if err := json.Unmarshal([]byte(cmp.Or(prompt.ToolSchemas, "null")), &sent); err != nil {
			return nil, err
		}
		at := slices.IndexFunc(sent, func(tool core.Tool) bool { return tool.Name == AskPersonToolName })
		switch {
		case i == len(prompts)-1:
			held = sent
			if at >= 0 {
				return held, nil
			}
		case at >= 0:
			after := min(at, len(held))
			if before := slices.IndexFunc(held, func(tool core.Tool) bool { return at > 0 && tool.Name == sent[at-1].Name }); before >= 0 {
				after = before + 1
			}
			return slices.Insert(held, after, sent[at]), nil
		}
	}
	return held, nil
}

func toldTools(events []session.Event, agent string, held []core.Tool) ([]core.Tool, error) {
	told := slices.Clone(held)
	for _, event := range events {
		if event.Kind != session.EventMessage || event.Agent != agent {
			continue
		}
		var message session.MessageBody
		if err := json.Unmarshal(event.Body, &message); err != nil {
			return nil, err
		}
		if message.Role != core.RoleUser.String() || !strings.HasPrefix(message.Content, thePromptChangedSince) {
			continue
		}
		for _, change := range strings.Split(message.Content, "\n\n") {
			described, isDescription := strings.CutPrefix(change, aToolDescriptionChanged)
			name, description, _ := strings.Cut(described, " now reads: ")
			if at := slices.IndexFunc(told, func(tool core.Tool) bool { return tool.Name == name }); isDescription && at >= 0 {
				told[at].Description = description
			}
		}
	}
	return told, nil
}

func (p *Prefix) pending() *Prefix {
	if p == nil {
		return nil
	}
	next := *p
	next.appended, next.removed = slices.Clone(p.appended), slices.Clone(p.removed)
	return &next
}

func (p *Prefix) lastSent() *messagesKept {
	if p == nil {
		return &messagesKept{}
	}
	return &p.sent
}

const (
	thePromptChangedSince   = "the system prompt above is kept as this session's first turn sent it, so the provider can reuse its cache. what changed in it since then:"
	noLongerApplies         = "no longer applies, so disregard it in the system prompt above: "
	PartAppendedWhy         = "sent in a message after the cached system prompt, because it changed after this session's first turn"
	aToolIsNew              = "a new tool is offered, and the tool list holds it: "
	aToolIsGone             = "a tool is no longer offered, so do not call it: "
	aToolChanged            = "a tool's definition changed, and the tool list holds the current one: "
	aToolDescriptionChanged = "a tool's description changed, and the tool list keeps the first one so the provider can reuse its cache; "
	aSkillIsNotLoaded       = "a skill is not loaded, so the skill tool does not offer it: "
	partRemovedWhy          = "removed after this session's first turn, and a message after the cached system prompt said so"
)

func inTheSystem(part keptPart) bool { return part.Sent() && !part.PerTurn }

func unloadedSkill(part keptPart) bool {
	return part.Kind == string(ConcernSkills) && part.State == session.PartDropped && part.Why != partRemovedWhy
}

func promptChange(message core.Message) bool {
	return message.Role == core.RoleUser && message.Origin.Source == core.SourceEnvironment && strings.HasPrefix(message.Content, thePromptChangedSince)
}

func (part keptPart) samePart(other keptPart) bool {
	return part.Kind == other.Kind && part.Rule == other.Rule && part.Agent == other.Agent && part.Name == other.Name && part.File == other.File && part.Off == other.Off
}

func (c Config) holdPrefix(tools []core.Tool, preview recall.Config) heldPrefix {
	now := heldPrefix{head: c.System, instructions: c.Instructions, system: c.SystemMessage(), parts: c.promptParts(preview), composedNow: true}
	p := c.Prefix
	if p == nil {
		return now
	}
	head := slices.DeleteFunc(slices.Clone(now.parts), func(part keptPart) bool { return !inTheSystem(part) })
	unloaded := slices.DeleteFunc(slices.Clone(now.parts), func(part keptPart) bool { return !unloadedSkill(part) })
	if !p.held {
		p.held, p.first, p.tools, p.inForce, p.unloaded = true, heldPrefix{head: now.head, instructions: now.instructions, system: now.system, parts: head}, tools, head, unloaded
		return now
	}
	var changes []string
	for _, part := range head {
		if slices.ContainsFunc(p.inForce, func(was keptPart) bool { return was.samePart(part) && was.text == part.text }) {
			continue
		}
		text := part.text
		if part.Kind == partInstructions {
			text = "[" + partInstructions + "]\n" + text
		}
		changes = append(changes, text)
		part.PerTurn, part.Why = true, strings.TrimPrefix(part.Why+"; "+PartAppendedWhy, "; ")
		p.appended = append(p.appended, part)
	}
	for _, was := range p.inForce {
		if !slices.ContainsFunc(head, was.samePart) {
			changes = append(changes, strings.TrimSpace(noLongerApplies+cmp.Or(was.Rule, was.Agent, was.Name, was.Kind)+" "+was.File))
			was.State, was.Why = session.PartDropped, partRemovedWhy
			p.removed = append(p.removed, was)
		}
	}
	p.inForce = head
	p.removed = slices.DeleteFunc(p.removed, func(gone keptPart) bool { return slices.ContainsFunc(head, gone.samePart) })
	for _, part := range unloaded {
		if !slices.ContainsFunc(p.unloaded, func(was keptPart) bool { return was.samePart(part) && was.Why == part.Why }) {
			changes = append(changes, aSkillIsNotLoaded+part.Name+" in "+part.File+", because "+part.Why)
		}
	}
	p.unloaded = unloaded
	changes = append(changes, p.holdTools(tools)...)
	held := p.first
	held.parts = slices.Concat(p.first.parts, p.appended, p.removed, slices.DeleteFunc(now.parts, inTheSystem))
	if len(changes) > 0 {
		held.changed = thePromptChangedSince + "\n\n" + strings.Join(changes, "\n\n")
	}
	return held
}

func (p *Prefix) holdTools(now []core.Tool) []string {
	var kept []core.Tool
	var changes []string
	for _, was := range p.tools {
		at := slices.IndexFunc(now, func(tool core.Tool) bool { return tool.Name == was.Name })
		switch {
		case at < 0:
			changes = append(changes, aToolIsGone+was.Name)
		case !sameDefinition(core.Tool{Parameters: was.Parameters}, core.Tool{Parameters: now[at].Parameters}):
			kept = append(kept, now[at])
			changes = append(changes, aToolChanged+was.Name)
		default:
			kept = append(kept, was)
			said := was
			if before := slices.IndexFunc(p.offered, func(tool core.Tool) bool { return tool.Name == was.Name }); before >= 0 {
				said = p.offered[before]
			}
			if said.Description != now[at].Description {
				changes = append(changes, aToolDescriptionChanged+was.Name+" now reads: "+now[at].Description)
			}
		}
	}
	for _, tool := range now {
		if !slices.ContainsFunc(p.tools, func(was core.Tool) bool { return was.Name == tool.Name }) {
			kept = append(kept, tool)
			changes = append(changes, aToolIsNew+tool.Name)
		}
	}
	p.tools, p.offered = kept, now
	return changes
}

func sameDefinition(was, now core.Tool) bool {
	before, errBefore := json.Marshal(was)
	after, errAfter := json.Marshal(now)
	return errBefore == nil && errAfter == nil && string(before) == string(after)
}

func (p *Prefix) toolsOr(tools []core.Tool) []core.Tool {
	if p == nil || !p.held {
		return tools
	}
	return p.tools
}

func (c Config) SystemMessage() string {
	return strings.TrimSpace(c.System + "\n\n" + c.Instructions)
}

type CalledAsEachRequestIsSent func(session.Event)

const partInstructions = "instructions"

func (c Config) promptParts(preview recall.Config) []keptPart {
	composed := Composed{Parts: c.Parts}
	if len(c.Parts) == 0 || composed.Head() != c.System {
		return nil
	}
	byTask := composed.render(func(part PromptPart) bool { return part.ByTask })
	parts := []keptPart{
		{PromptPart: session.PromptPart{Kind: string(rules.ConcernEnvironment), PerTurn: true}, text: strings.TrimSpace(strings.Replace(c.Environment, byTask, "", 1))},
		{PromptPart: session.PromptPart{Kind: partInstructions}, text: c.Instructions},
	}
	for _, part := range c.Parts {
		if part.Concern == rules.ConcernEnvironment {
			continue
		}
		kept := keptPart{PromptPart: session.PromptPart{Kind: string(part.Concern), Rule: part.RuleID, Agent: part.Agent, Name: part.Name, File: part.File, State: part.State, Why: part.Why,
			Reason: string(part.Reason), Mode: string(part.Mode), PerTurn: part.ByTask, Off: part.Off}}
		if kept.Sent() {
			kept.text = part.rendered()
		}
		parts = append(parts, kept)
	}
	parts = slices.DeleteFunc(parts, func(part keptPart) bool { return part.Sent() && strings.TrimSpace(part.text) == "" })
	for i := range parts {
		parts[i].Tokens = preview.Tokens(parts[i].text)
	}
	return parts
}

func (c Config) MemoryMessage() []core.Message {
	if strings.TrimSpace(c.Memory) == "" {
		return nil
	}
	return []core.Message{{Role: core.RoleUser, Content: c.Memory, Origin: core.Origin{Source: core.SourceMemoryView}}}
}

func (c Config) opensGeneration() bool {
	return len(c.History) == 0 || c.SessionSource != ""
}

func withoutMemoryViews(messages []core.Message) []core.Message {
	return slices.DeleteFunc(slices.Clone(messages), func(message core.Message) bool { return message.Origin.Source == core.SourceMemoryView })
}

func withMemoryViewAtHead(head, view []core.Message) []core.Message {
	head = withoutMemoryViews(head)
	at := slices.IndexFunc(head, func(message core.Message) bool { return !standsAtTheHead(message) })
	if at < 0 {
		at = len(head)
	}
	return slices.Insert(head, at, view...)
}

func newestMemoryView(messages []core.Message) []core.Message {
	for _, message := range slices.Backward(messages) {
		if message.Origin.Source == core.SourceMemoryView {
			return []core.Message{message}
		}
	}
	return nil
}

func TaskIn(first string) (string, bool) {
	if _, task, follows := strings.Cut(first, TheTaskFollows); follows {
		first = task
	} else if strings.HasPrefix(first, environmentBeforeTheTask) {
		return "", false
	}
	task, _, _ := strings.Cut(first, NotesAfterTheTask)
	return task, true
}
