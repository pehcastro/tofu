package settings

import (
	"cmp"
	"errors"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"

	"tofu/internal/boardy"
	"tofu/internal/konst"
	"tofu/internal/llm/models"
	"tofu/internal/subagent"
)

type Kind int

const (
	Bool Kind = iota
	Int
	Text
)

type Spec struct {
	Key         string
	Label       string
	Description string
	Category    string
	Group       string
	Kind        Kind
	Default     int
	DefaultText string
	Choices     []string
	Offers      []string
	ListOf      []string
	Aliases     map[string]string
	Formerly    string
	Restart     bool
	Least, Most int
	Unit        string
}

const (
	Theme                       = "theme"
	Density                     = "density"
	Animations                  = "animations"
	StatusBar                   = "statusBar"
	ColorMode                   = "colorMode"
	Composer                    = "composer"
	GatePrompt                  = "gatePrompt"
	GateAskWaitSeconds          = "gateAskWaitSeconds"
	GateAskTimeout              = "gateAskTimeout"
	AskPersonWaitSeconds        = "askPersonWaitSeconds"
	ReadBeforeEdit              = "readBeforeEdit"
	AutoUpdate                  = "autoUpdate"
	ChatShowsTools              = "chatShowsTools"
	AgentFeeds                  = "agentFeeds"
	ShowThinking                = "showThinking"
	ThinkingSummary             = "thinkingSummary"
	Images                      = "images"
	ProjectInstructionsCap      = "projectInstructionsCap"
	ContextCeiling              = "contextCeiling"
	InstructionSources          = "instructionSources"
	Memory                      = "memory"
	AutoMemory                  = "autoMemory"
	Episodes                    = "episodes"
	MemoryModel                 = "memoryModel"
	MemoryFromAllUsers          = "memoryFromAllUsers"
	Learn                       = "learn"
	DiffContext                 = "diffContext"
	Hyperlinks                  = "hyperlinks"
	GroupByAgent                = "groupByAgent"
	PersistentRegistry          = "persistentRegistry"
	LogTail                     = "logTail"
	KillConfirm                 = "killConfirm"
	Shell                       = "shell"
	FoldHidesShell              = "foldHidesShell"
	DecisionCap                 = "decisionCap"
	TurnMaySpawn                = "turnMaySpawn"
	OneWorkingSessionPerProject = "oneWorkingSessionPerProject"
	PlanEveryStep               = "planEveryStep"
	SideChatAccess              = "sideChatAccess"
	SubAgentsPerTurn            = "subAgentsPerTurn"
	SubAgentDepth               = "subAgentDepth"
	LeadCache                   = "leadCache"
	SubAgentCache               = "subAgentCache"
	SubAgentCheckSeconds        = "subAgentCheckSeconds"
	SubAgentWatchSeconds        = "subAgentWatchSeconds"
	VerifySubAgents             = "verifySubAgents"
	AgentSources                = "agentSources"
	Skills                      = "skills"
	Browser                     = "browser"
	BrowserDriver               = "browserDriver"
	BrowserSteps                = "browserSteps"
	BrowserModel                = "browserModel"
	BrowserEffort               = "browserEffort"
	BrowserCursor               = "browserCursor"
	BrowserProfile              = "browserProfile"
	BrowserHeadless             = "browserHeadless"
	ContinueOnSubscription      = "continueOnSubscription"
	SubscriptionOrder           = "subscriptionOrder"
	ContinueOnAPIModel          = "continueOnAPIModel"
)

const ModelsCategory = "Models & roles"

const (
	BrowserOff   = "off"
	BrowserRead  = "read"
	BrowserDrive = "drive"
)

const (
	DriverSteps    = "steps"
	DriverGoal     = "goal"
	DriverSubagent = "subagent"
)

const (
	InstructionsAgentsFirst = "agents-first"
	InstructionsClaudeFirst = "claude-first"
	InstructionsBoth        = "both"
)

func InstructionFiles(choice string) []string {
	if choice == InstructionsClaudeFirst {
		return []string{"CLAUDE.md", "AGENTS.md"}
	}
	return []string{"AGENTS.md", "CLAUDE.md"}
}

const (
	SkillsOn  = "on"
	SkillsOff = "off"
)

const (
	AllUsersOn  = "on"
	AllUsersOff = "off"
)

const (
	EpisodesOn  = "on"
	EpisodesOff = "off"
)

const (
	ThinkingSummarized = "summarized"
	ThinkingOmitted    = "omitted"
)

const (
	GatePromptAuto = "auto"
	GatePromptAsk  = "ask"
)

const (
	GateAskRefuse = "refuse"
	GateAskAllow  = "allow"
)

const (
	FeedsFull    = "full"
	FeedsSummary = "summary"
	FeedsOff     = "off"
)

const (
	ImagesAuto   = "auto"
	ImagesInline = "inline"
	ImagesOff    = "off"
)

const (
	LinksAuto = "auto"
	LinksOn   = "on"
	LinksOff  = "off"
)

func DeclaredDefault(key string) int {
	for _, spec := range Default() {
		if spec.Key == key {
			return spec.Default
		}
	}
	return 0
}

func DeclaredDefaultText(key string) string {
	for _, spec := range Default() {
		if spec.Key == key {
			return spec.DefaultText
		}
	}
	return ""
}

func (s Spec) allows(value string) bool {
	if s.ListOf == nil {
		return len(s.Choices) == 0 || slices.Contains(s.Choices, value)
	}
	items := strings.Split(value, ",")
	for i, item := range items {
		if !slices.Contains(s.ListOf, item) || slices.Contains(items[:i], item) {
			return false
		}
	}
	return true
}

func Refusal(typed string, least, most int) string {
	return fmt.Sprintf("refused %s: the value is a whole number from %d to %d", cmp.Or(typed, "nothing"), least, most)
}

func (s Spec) refuses(value int) error {
	if s.Kind != Int || (value >= s.Least && value <= s.Most) {
		return nil
	}
	return errors.New(Refusal(strconv.Itoa(value), s.Least, s.Most))
}

func checkTable(specs []Spec) error {
	for _, spec := range specs {
		if spec.Kind == Int && spec.Most <= spec.Least {
			return fmt.Errorf("settings: %s is a number with no range", spec.Key)
		}
	}
	return nil
}

func subscriptionSources() []string {
	var sources []string
	for _, source := range models.AllSubscriptions() {
		sources = append(sources, string(source))
	}
	return sources
}

func lineCounts(counts ...int) []string {
	choices := make([]string, len(counts))
	for i, count := range counts {
		choices[i] = strconv.Itoa(count)
	}
	return choices
}

func Default() []Spec {
	specs := []Spec{
		{Key: Theme, Label: "Theme", Description: "tofu uses Zed Nu Disco; choose a palette", Category: "Appearance", Kind: Text, DefaultText: "tofu",
			Choices: []string{"tofu", "tofu light", "tofu dusk", "Solarized Light", "Dracula", "Nord", "Gruvbox", "terminal"}},
		{Key: Density, Label: "Density", Description: "Spacing across lists and panels", Category: "Appearance", Kind: Text, DefaultText: "comfortable",
			Choices: []string{"compact", "comfortable", "spacious"}},
		{Key: Animations, Label: "Animations", Description: "Motion for conversational states", Category: "Appearance", Kind: Text, DefaultText: "subtle",
			Choices: []string{"off", "subtle", "full"}},
		{Key: StatusBar, Label: "Status bar", Description: "Context, quotas, and runtime counters", Category: "Appearance", Kind: Text, DefaultText: "compact",
			Choices: []string{"compact", "detailed", "hidden"}},
		{Key: ColorMode, Label: "Color mode", Description: "Terminal color capability", Category: "Appearance", Kind: Text, DefaultText: "auto",
			Choices: []string{"auto", "truecolor", "256 colors", "ANSI 16"}},
		{Key: ContinueOnSubscription, Label: "Continue on another subscription", Description: "Where work continues, on another subscription, once every account on this one is at its limit. Empty stops it", Category: ModelsCategory, Kind: Text},
		{Key: SubscriptionOrder, Label: "Subscription order", Description: "The order continueOnSubscription tries providers in, as a comma list of sources. Empty keeps the order picked", Category: ModelsCategory, Kind: Text,
			ListOf: subscriptionSources()},
		{Key: ContinueOnAPIModel, Label: "Continue on an API model", Description: "The model, paid by its key, that work continues on when no subscription has an account left. Empty stops it", Category: ModelsCategory, Kind: Text},
		{Key: Composer, Label: "Composer", Description: "Enter sends; Shift+Enter inserts a line break", Category: "Interaction", Kind: Text, DefaultText: "standard",
			Choices: []string{"standard", "compact", "expanded"}},
		{Key: GatePrompt, Label: "Confirmations", Description: "auto lets Jev decide at every gate, so work never stops for you. ask waits for you on what Jev would ask about", Category: "Interaction", Kind: Text, DefaultText: GatePromptAuto,
			Choices: []string{GatePromptAuto, GatePromptAsk}, Aliases: map[string]string{"run": GatePromptAuto}},
		{Key: GateAskWaitSeconds, Label: "Ask wait", Description: "Seconds a gate ask waits for your answer before gateAskTimeout decides it", Category: "Interaction", Kind: Int, Default: konst.GateAskWaitSeconds,
			Least: 1, Most: math.MaxInt32, Unit: "seconds an ask waits for you"},
		{Key: GateAskTimeout, Label: "Unanswered ask", Description: "What an ask nobody answered in time does. refuse turns it down and tells the lead why, allow runs it", Category: "Interaction", Kind: Text, DefaultText: GateAskRefuse,
			Choices: []string{GateAskRefuse, GateAskAllow}},
		{Key: AskPersonWaitSeconds, Label: "Question wait", Description: "Under auto, seconds before a question the lead asks you takes its recommended option", Category: "Interaction", Kind: Int, Default: konst.AskPersonWaitSeconds,
			Least: 1, Most: math.MaxInt32, Unit: "seconds before the recommended option is taken under auto"},
		{Key: ReadBeforeEdit, Label: "Read before edit", Description: "an edit or a write to a file this session has not read, or that changed since it was read, is refused", Category: "Interaction", Kind: Bool, Default: 1},
		{Key: AutoUpdate, Label: "Auto update", Description: "on installs a newer release in the background for the next start. off only says one is out", Category: "Interaction", Kind: Bool, Default: 1},
		{Key: ChatShowsTools, Label: "Tool detail", Description: "chat shows every tool call", Category: "Context", Kind: Bool},
		{Key: AgentFeeds, Label: "Agent feeds", Description: "full keeps every sub-agents event, summary the latest 200, off shows only the list of agents", Category: "Context", Kind: Text, DefaultText: FeedsFull,
			Choices: []string{FeedsFull, FeedsSummary, FeedsOff}},
		{Key: ShowThinking, Label: "Thinking", Description: "thinking shows in each agent's sub-agents feed; t there toggles it", Category: "Context", Kind: Bool, Default: 1},
		{Key: ThinkingSummary, Label: "Thinking summary", Description: "summarized asks Claude for a summary of its thinking. omitted asks for none", Category: "Context", Kind: Text, DefaultText: ThinkingSummarized,
			Choices: []string{ThinkingSummarized, ThinkingOmitted}},
		{Key: Images, Label: "Images", Description: "auto sends a pasted image only to a model that can see, inline always sends, off never attaches", Category: "Context", Kind: Text, DefaultText: ImagesAuto,
			Choices: []string{ImagesAuto, ImagesInline, ImagesOff}},
		{Key: ProjectInstructionsCap, Label: "Project instructions", Description: "bytes of your instruction files sent each turn", Category: "Context", Kind: Int, Default: konst.ProjectInstructionsBytesDefault,
			Least: 1, Most: konst.ProjectInstructionsBytesMost, Unit: "bytes of AGENTS.md and CLAUDE.md sent each turn"},
		{Key: ContextCeiling, Label: "Context ceiling", Description: "The most tokens one request may carry. The conversation forks before it", Category: "Context", Kind: Int, Default: konst.ContextCeilingTokens,
			Least: 1, Most: math.MaxInt32, Unit: "tokens a request may carry"},
		{Key: InstructionSources, Label: "Instruction files", Description: "Which of AGENTS.md and CLAUDE.md a folder with both sends. both sends both and costs more tokens", Category: "Context", Kind: Text, DefaultText: InstructionsAgentsFirst,
			Choices: []string{InstructionsAgentsFirst, InstructionsClaudeFirst, InstructionsBoth},
			Aliases: map[string]string{"AGENTS.md,CLAUDE.md": InstructionsAgentsFirst, "AGENTS.md": InstructionsAgentsFirst, "CLAUDE.md,AGENTS.md": InstructionsClaudeFirst, "CLAUDE.md": InstructionsClaudeFirst}},
		{Key: Skills, Label: "Skills", Description: "on lists your skills to the model and offers the skill tool. off sends neither", Category: "Context", Kind: Text, DefaultText: SkillsOn,
			Choices: []string{SkillsOn, SkillsOff}},
		{Key: Memory, Label: "Memory", Description: "on sends what you asked tofu to remember on every request. off sends none", Category: "Context", Kind: Bool, Default: 1},
		{Key: AutoMemory, Label: "Auto memory", Description: "on keeps a rule the lead offers at once and prints the undo, instead of asking first", Category: "Context", Kind: Bool},
		{Key: Episodes, Label: "Episodes", Description: "on keeps this project's conversation as episodes and sends their view. Needs memory on", Category: "Context", Kind: Text, DefaultText: EpisodesOn,
			Choices: []string{EpisodesOn, EpisodesOff}},
		{Key: MemoryFromAllUsers, Label: "Memory from all users", Description: "on applies the user local memory every author kept in this repository. off applies only yours", Category: "Context", Kind: Text, DefaultText: AllUsersOff,
			Choices: []string{AllUsersOn, AllUsersOff}},
		{Key: MemoryModel, Label: "Memory model", Description: "The model that writes memory summaries, as source/model. Empty takes the library's memory model", Category: "Context", Kind: Text},
		{Key: Learn, Label: "Learn", Description: "on lets tofu learn send windows of your messages to Jev for labels. off runs it on this machine only", Category: "Context", Kind: Bool},
		{Key: DiffContext, Label: "Diff context", Description: "Lines around changed hunks, at most the " + strconv.Itoa(konst.DiffContextLinesDefault) + " each diff carries", Category: "Files", Kind: Text, DefaultText: strconv.Itoa(konst.DiffContextLinesDefault),
			Choices: lineCounts(konst.DiffContextLinesTight, konst.DiffContextLinesDefault, konst.DiffContextLinesWide, konst.DiffContextLinesWidest)},
		{Key: Hyperlinks, Label: "Hyperlinks", Description: "OSC 8 terminal file links; off prints the bare path", Category: "Files", Kind: Text, DefaultText: LinksAuto,
			Choices: []string{LinksAuto, LinksOn, LinksOff}},
		{Key: GroupByAgent, Label: "Group by agent", Description: "Author filters in edit feeds", Category: "Files", Kind: Bool, Default: 1},
		{Key: PersistentRegistry, Label: "Persistent registry", Description: "A running shell outlives tofu and the next launch lists it. off stops every shell on exit", Category: "Shell", Kind: Bool, Restart: true},
		{Key: LogTail, Label: "Log tail", Description: "Lines retained per shell", Category: "Shell", Kind: Text, DefaultText: strconv.Itoa(konst.ShellLogTailLinesDefault),
			Choices: lineCounts(konst.ShellLogTailLinesShort, konst.ShellLogTailLinesDefault, konst.ShellLogTailLinesLong)},
		{Key: KillConfirm, Label: "Kill confirm", Description: "Confirm process termination", Category: "Shell", Kind: Bool, Default: 1},
		{Key: Shell, Label: "Shell", Description: "The shell commands run in: wsl or a path. Empty finds your own shell", Category: "Shell", Kind: Text,
			Offers: []string{"", "wsl"}},
		{Key: FoldHidesShell, Label: "Fold hides shell", Description: "the running line drops the shell count", Category: "Shell", Kind: Bool},
		{Key: DecisionCap, Label: "Decision cap", Description: "decision cap per turn, zero means no cap", Category: "Turn", Kind: Int,
			Least: 0, Most: konst.DecisionCapMost, Unit: "decisions a turn, where 0 means no cap"},
		{Key: TurnMaySpawn, Label: "Sub-agents", Description: "a turn may spawn a sub-agent", Category: "Turn", Kind: Bool, Default: 1},
		{Key: OneWorkingSessionPerProject, Label: "One working session per project", Description: "While one session in this project works, any other waits. off lets several work at once", Category: "Turn", Kind: Bool, Default: 1, Formerly: "oneTurnPerProject"},
		{Key: PlanEveryStep, Label: "Plan every step", Description: "The lead sees its plan and each step's status before every step. off shows it only on the plan tool", Category: "Turn", Kind: Bool, Default: 1},
		{Key: SideChatAccess, Label: "Side chat access", Description: "What a new side chat may write: read, notes for .md and .html, files for anywhere, or a comma list of globs", Category: "Turn", Kind: Text, DefaultText: "read",
			Offers: []string{"read", "notes", "files"}},
		{Key: SubAgentsPerTurn, Label: "Sub-agents running at once", Description: "How many sub-agents may run at once. A spawn past it is refused until one ends", Category: "Turn", Kind: Int, Default: konst.SubAgentsPerTurnDefault,
			Least: 1, Most: math.MaxInt32, Unit: "sub-agents running at once"},
		{Key: SubAgentDepth, Label: "Sub-agent depth", Description: "How deep a sub-agent may spawn its own sub-agents", Category: "Turn", Kind: Int, Default: konst.SubAgentDepthDefault,
			Least: 1, Most: math.MaxInt32, Unit: "levels of sub-agents"},
		{Key: LeadCache, Label: "Lead cache", Description: "How long the lead's prompt cache lives on a subscription. 5m costs less, 1h lasts, history-5m mixes them", Category: "Turn", Kind: Text, DefaultText: konst.SubscriptionCacheTTL,
			Choices: []string{konst.SubscriptionCacheTTL, "5m", konst.SubscriptionCacheHistory5m}},
		{Key: SubAgentCache, Label: "Sub-agent cache", Description: "How long a sub-agent's prompt cache lives. 5m writes cost less, 1h writes last an hour", Category: "Turn", Kind: Text, DefaultText: konst.SubscriptionCacheTTL,
			Choices: []string{"1h", "5m"}},
		{Key: SubAgentCheckSeconds, Label: "Sub-agent check", Description: "Seconds between the lead's checks on each running sub-agent, made with no model call. 0 sends none", Category: "Turn", Kind: Int, Default: konst.SubAgentCheckSecondsDefault,
			Least: 0, Most: math.MaxInt32, Unit: "seconds between checks on a running sub-agent, where 0 sends none"},
		{Key: SubAgentWatchSeconds, Label: "Sub-agent watch", Description: "Seconds before a long sub-agent call is named in the chat, and a sub-agent with no progress shows stalled", Category: "Turn", Kind: Int, Default: konst.SubAgentWatchSecondsDefault,
			Least: 1, Most: math.MaxInt32, Unit: "seconds before a long call is named, and before a sub-agent with no progress is stalled"},
		{Key: VerifySubAgents, Label: "Check sub-agents", Description: "The lead checks each sub-agent's work itself before it reports. off takes the report as the result", Category: "Turn", Kind: Bool, Default: 1},
		{Key: AgentSources, Label: "Agent folders", Description: "The project folders sub-agents are read from, in order: tofu, agents and claude", Category: "Turn", Kind: Text, DefaultText: "tofu,agents,claude",
			ListOf: []string{"tofu", "agents", "claude"}},
		{Key: subagent.TierGenius.Setting(), Label: "Genius tier", Description: "the model @genius names, and opus in a shared agent file; empty runs the orchestrator's model", Category: "Turn", Kind: Text},
		{Key: subagent.TierSmart.Setting(), Label: "Smart tier", Description: "the model @smart names, and sonnet in a shared agent file; empty runs the orchestrator's model", Category: "Turn", Kind: Text},
		{Key: subagent.TierWorker.Setting(), Label: "Worker tier", Description: "the model @worker names, and haiku in a shared agent file; empty runs the orchestrator's model", Category: "Turn", Kind: Text},
		{Key: subagent.TierDumb.Setting(), Label: "Dumb tier", Description: "the model @dumb names; empty runs the orchestrator's model", Category: "Turn", Kind: Text},
		{Key: boardy.ManagementSetting, Label: "Boardy", Description: "boardy runs the lead and its sub-agents from board tickets. off keeps owns a grant per spawn", Category: "Projects", Kind: Text, DefaultText: string(boardy.ManagementOff),
			Choices: []string{string(boardy.ManagementOff), string(boardy.ManagementBoardy)}},
		{Key: boardy.FlowSetting, Label: "Boardy flow", Description: "The words tofu boardy uses. jira says ticket and sprint, linear says issue and cycle", Category: "Projects", Kind: Text, DefaultText: string(boardy.Jira),
			Choices: []string{string(boardy.Jira), string(boardy.Linear)}},
		{Key: "scratchCleanupDays", Label: "Scratchpad cleanup", Description: "Days a session's scratch folder may sit untouched before it is deleted. shared is never cleaned", Category: "Projects", Kind: Int, Default: konst.ScratchCleanupDays,
			Least: 0, Most: math.MaxInt32, Unit: "days a session's scratch folder is kept untouched"},
		{Key: "scratchMaxGB", Label: "Scratchpad size", Description: "past this many gigabytes in one project's scratchpad, the oldest session folders are deleted first", Category: "Projects", Kind: Int, Default: konst.ScratchMaxGB,
			Least: 1, Most: math.MaxInt32, Unit: "gigabytes one project's scratchpad may hold"},
		{Key: "sessionStaleDays", Label: "Session cleanup", Description: "Days a session with no activity is kept before it is deleted. 0 never deletes", Category: "Projects", Kind: Int, Default: konst.SessionStaleDays,
			Least: 0, Most: math.MaxInt32, Unit: "days a session is kept with no activity"},
		{Key: Browser, Label: "Browser", Description: "drive lets the model read and act in your Chrome tabs, read only reads them. off offers no browser tool", Category: "Browser", Kind: Text, DefaultText: BrowserDrive,
			Choices: []string{BrowserOff, BrowserRead, BrowserDrive}},
		{Key: BrowserDriver, Label: "Browser driver", Description: "Who takes the browser steps: subagent hands them to a browser sub-agent, steps to the model, goal to Jev", Category: "Browser", Kind: Text, DefaultText: DriverSubagent,
			Choices: []string{DriverSubagent, DriverSteps, DriverGoal}, Aliases: map[string]string{"jev": DriverGoal, "model": DriverSteps}, Formerly: "browserChooser"},
		{Key: BrowserSteps, Label: "Browser steps", Description: "how many actions one browser task may take", Category: "Browser", Kind: Int, Default: konst.BrowserStepsDefault,
			Least: 1, Most: konst.BrowserActionCeiling, Unit: "actions a browser task"},
		{Key: BrowserModel, Label: "Browser model", Description: "The model a browser task writes its text and answer with, as source/model. Empty takes the dumb tier", Category: "Browser", Kind: Text},
		{Key: BrowserEffort, Label: "Browser effort", Description: "The effort the browser sub-agent runs at. Empty takes the model's own default", Category: "Browser", Kind: Text,
			Choices: []string{"", "low", "medium", "high", "xhigh", "max"}},
		{Key: BrowserCursor, Label: "Browser cursor", Description: "A cursor labelled tofu glides to each click in tofu's tab. It is drawn only and changes nothing", Category: "Browser", Kind: Bool, Default: 1},
		{Key: BrowserProfile, Label: "Browser profile", Description: "The browser profile tofu drives, from tofu browser profile new. Empty drives your Chrome through the extension", Category: "Browser", Kind: Text},
		{Key: BrowserHeadless, Label: "Browser headless", Description: "A Chrome tofu starts on a browser profile runs with no window. false shows it", Category: "Browser", Kind: Bool, Default: 1},
	}
	for i := range specs {
		specs[i].Group = specs[i].Category
	}
	return specs
}
