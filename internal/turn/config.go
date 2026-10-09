package turn

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"tofu/internal/llm"
	"tofu/internal/recall"
	"tofu/internal/session"
	"tofu/internal/subagent"
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

type Config struct {
	Model           Model
	Accounts        Accounts
	Spend           Spend
	Tools           Registry
	ToolSource      func() Registry
	Gate            Gate
	GateMode        GateMode
	Proxy           *CommandProxy
	Boundary        *subagent.Boundary
	Person          Person
	Task            string
	TaskOrigin      llm.Origin
	Images          []llm.Image
	History         []llm.Message
	Wire            string
	SpawnedFrom     string
	AgentType       string
	Project         string
	SessionSource   string
	System          string
	References      map[string]string
	Environment     string
	Instructions    string
	Memory          string
	Prefix          *Prefix
	ImagesOf        func(said string) []llm.Image
	Caps            Caps
	Sift            *ShellSift
	Thrift          *ThriftSift
	ResultBytesCap  int
	ArtifactDir     string
	TruncateResults bool
	NoCompaction    bool
	NoFork          bool
	NoLastWord      bool
	Budget          recall.Budget
	Sessions        *session.Store
	Session         string
	Log             *session.Log
	Turn            string
	SpawnedBy       string
	Steering        func() []string
	Inbox           *Inbox
	Step            CalledAsTheStepIsRecordedAndBeforeTheNextOneIsAsked
	ToolResult      CalledAsEachToolCallAnswersAndBeforeTheNextRequest
	Appended        func(session.Event)
	EndedSession    func(Row) error
	Notify          func(string)
	Now             func() time.Time
	NewID           func() string
}

const (
	environmentBeforeTheTask = "<env>"
	TheTaskFollows           = "\n\n[the task]\n"
	NotesAfterTheTask        = "\n\n[tofu's notes on this task, not the person's words]\n"
)

func (c Config) FirstUserMessage(notes ...string) string {
	first := c.Task
	if c.Environment != "" {
		first = c.Environment + TheTaskFollows + c.Task
	}
	notes = slices.DeleteFunc(notes, func(note string) bool { return strings.TrimSpace(note) == "" })
	if len(notes) == 0 {
		return first
	}
	return first + NotesAfterTheTask + strings.Join(notes, "\n\n")
}

type Prefix struct {
	held   bool
	system string
	tools  []llm.Tool
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

const sourceMemoryView = "memory view"

func (c Config) MemoryMessage() []llm.Message {
	if strings.TrimSpace(c.Memory) == "" || len(c.History) > 0 {
		return nil
	}
	return []llm.Message{{Role: llm.RoleUser, Content: c.Memory, Origin: llm.Origin{Source: sourceMemoryView}}}
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
