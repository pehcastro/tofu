package turn

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"

	"tofu/internal/boardy"
	"tofu/internal/konst"
	"tofu/internal/llm"
	"tofu/internal/recall"
	"tofu/internal/rule"
	"tofu/internal/session"
	"tofu/internal/subagent"
	"tofu/internal/sys"
	"tofu/library/changelog"
)

type Model interface {
	Ask(ctx context.Context, request llm.Request) (llm.Decision, error)
}

type runningModelKey struct{}

type RunningModel struct{}

func (RunningModel) Ask(ctx context.Context, request llm.Request) (llm.Decision, error) {
	model, running := ctx.Value(runningModelKey{}).(Model)
	if !running {
		return llm.Decision{}, errors.New("no turn is running, so there is no turn model to ask")
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

type CalledAsEachToolCallAnswersAndBeforeTheNextRequest func(llm.Message)

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
	Boundary        *subagent.Boundary
	Person          Person
	GatePromptAsks  func() bool
	Task            string
	TaskOrigin      llm.Origin
	Arrived         []llm.Message
	Images          []llm.Image
	History         []llm.Message
	Wire            string
	Slug            string
	SpawnedFrom     string
	AgentType       string
	Project         string
	SessionSource   string
	System          string
	Parts           []PromptPart
	References      map[string]string
	Environment     string
	Instructions    string
	Memory          string
	Plan            func() string
	Prefix          *Prefix
	ImagesOf        func(said string) []llm.Image
	Caps            Caps
	Sift            *ShellSift
	Thrift          *ThriftSift
	ResultBytesCap  int
	ArtifactDir     string
	Places          sys.Places
	Redactor        *sys.KeyRedactor
	TruncateResults bool
	NoCompaction    bool
	NoFork          bool
	NoLastWord      bool
	Budget          recall.Budget
	Sessions        *session.Store
	Session         string
	Board           *boardy.Managed
	TicketGrant     func(board boardy.Managed, id, lead, agent string) ([]string, error)
	TicketTools     func(board boardy.Managed, actor string) ([]Tool, error)
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

func (c Config) ResolvedPlaces() (sys.Places, error) {
	if c.Places != (sys.Places{}) {
		return c.Places, nil
	}
	return sys.ResolvePlaces(".")
}

const (
	environmentBeforeTheTask = "<env>"
	TheTaskFollows           = "\n\n[the task]\n"
	NotesAfterTheTask        = "\n\n[tofu's notes on this task, not the person's words]\n"
)

func (c Config) upgradeNote() string {
	if c.Sessions == nil || c.Session == "" || c.SpawnedFrom != "" {
		return ""
	}
	last := ""
	_ = c.Sessions.EachEvent(c.Session, func(agent string) bool { return agent == "" }, func(event session.Event) bool {
		var start session.TurnStart
		if event.Kind == session.EventTurnStart && json.Unmarshal(event.Body, &start) == nil {
			last = start.Tofu
		}
		return true
	})
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
	return first + notesOn(notes)
}

func notesOn(notes []string) string {
	notes = slices.DeleteFunc(notes, func(note string) bool { return strings.TrimSpace(note) == "" })
	if len(notes) == 0 {
		return ""
	}
	return NotesAfterTheTask + strings.Join(notes, "\n\n")
}

func (c Config) taskOrigin(started time.Time) llm.Origin {
	task := c.TaskOrigin
	switch {
	case task.Source != "" || len(c.Arrived) > 0:
	case c.SpawnedFrom != "":
		task.Source = llm.SourceBrief
	default:
		task.Source = llm.SourceTask
	}
	task.TakenAt = started
	return task
}

func (c Config) forkOpening(task llm.Origin) llm.Message {
	if task.Kind() == llm.OriginPerson {
		task.Source = llm.SourceForkTask
	}
	if len(c.Arrived) == 0 {
		return llm.Message{Role: llm.RoleUser, Content: c.FirstUserMessage(), Origin: task}
	}
	return llm.Message{Role: llm.RoleUser, Content: c.Task, Origin: task}
}

func (c Config) opening(task llm.Origin, notes ...string) []llm.Message {
	if len(c.Arrived) == 0 {
		return []llm.Message{{Role: llm.RoleUser, Content: c.FirstUserMessage(notes...), Images: c.Images, Origin: task}}
	}
	var opened []llm.Message
	if framing := strings.TrimSpace(c.Environment + notesOn(notes)); framing != "" {
		opened = append(opened, llm.Message{Role: llm.RoleUser, Content: framing, Origin: llm.Origin{Source: llm.SourceEnvironment, TakenAt: task.TakenAt}})
	}
	for _, item := range c.Arrived {
		item.Origin.TakenAt = task.TakenAt
		opened = append(opened, item)
	}
	return opened
}

type Prefix struct {
	held   bool
	system string
	tools  []llm.Tool
	sent   messagesKept
}

func (p *Prefix) lastSent() *messagesKept {
	if p == nil {
		return &messagesKept{}
	}
	return &p.sent
}

func (p *Prefix) hold(system string, tools []llm.Tool) string {
	if p == nil {
		return system
	}
	if !p.held {
		p.held, p.system, p.tools = true, system, tools
	}
	return p.system
}

func (p *Prefix) toolsOr(tools []llm.Tool) []llm.Tool {
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
		{PromptPart: session.PromptPart{Kind: string(rule.ConcernEnvironment), PerTurn: true}, text: strings.TrimSpace(strings.TrimSuffix(c.Environment, byTask))},
		{PromptPart: session.PromptPart{Kind: partInstructions}, text: c.Instructions},
	}
	for _, part := range c.Parts {
		if part.Concern != rule.ConcernEnvironment {
			parts = append(parts, keptPart{PromptPart: session.PromptPart{Kind: string(part.Concern), Rule: part.RuleID, Agent: part.Agent, File: part.File, Why: part.Why,
				PerTurn: part.ByTask, Off: part.Off}, text: part.rendered()})
		}
	}
	parts = slices.DeleteFunc(parts, func(part keptPart) bool { return strings.TrimSpace(part.text) == "" })
	for i := range parts {
		parts[i].Tokens = preview.Tokens(parts[i].text)
	}
	return parts
}

func (c Config) MemoryMessage() []llm.Message {
	if strings.TrimSpace(c.Memory) == "" {
		return nil
	}
	return []llm.Message{{Role: llm.RoleUser, Content: c.Memory, Origin: llm.Origin{Source: llm.SourceMemoryView}}}
}

func (c Config) opensGeneration() bool {
	return len(c.History) == 0 || c.SessionSource != ""
}

func withoutMemoryViews(messages []llm.Message) []llm.Message {
	return slices.DeleteFunc(slices.Clone(messages), func(message llm.Message) bool { return message.Origin.Source == llm.SourceMemoryView })
}

func (c Config) withFreshMemoryView(head []llm.Message) []llm.Message {
	head = withoutMemoryViews(head)
	at := slices.IndexFunc(head, func(message llm.Message) bool { return !standsAtTheHead(message) })
	if at < 0 {
		at = len(head)
	}
	return slices.Insert(head, at, c.MemoryMessage()...)
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
