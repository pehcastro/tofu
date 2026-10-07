package settings

import (
	"cmp"
	"errors"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"

	"tofu/internal/konst"
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
	ListOf      []string
	Aliases     map[string]string
	Formerly    string
	Restart     bool
	Least, Most int
	Unit        string
}

const (
	Theme                  = "theme"
	Density                = "density"
	Animations             = "animations"
	StatusBar              = "statusBar"
	ColorMode              = "colorMode"
	Composer               = "composer"
	GatePrompt             = "gatePrompt"
	ReadBeforeEdit         = "readBeforeEdit"
	ChatShowsTools         = "chatShowsTools"
	AgentFeeds             = "agentFeeds"
	ShowThinking           = "showThinking"
	ThinkingSummary        = "thinkingSummary"
	Images                 = "images"
	ProjectInstructionsCap = "projectInstructionsCap"
	InstructionSources     = "instructionSources"
	Memory                 = "memory"
	AutoMemory             = "autoMemory"
	DiffContext            = "diffContext"
	Hyperlinks             = "hyperlinks"
	GroupByAgent           = "groupByAgent"
	PersistentRegistry     = "persistentRegistry"
	LogTail                = "logTail"
	KillConfirm            = "killConfirm"
	Shell                  = "shell"
	FoldHidesShell         = "foldHidesShell"
	DecisionCap            = "decisionCap"
	TurnMaySpawn           = "turnMaySpawn"
	OneTurnPerProject      = "oneTurnPerProject"
	SubAgentsPerTurn       = "subAgentsPerTurn"
	SubAgentDepth          = "subAgentDepth"
	SubAgentCheckSeconds   = "subAgentCheckSeconds"
	SubAgentWatchSeconds   = "subAgentWatchSeconds"
	VerifySubAgents        = "verifySubAgents"
	AgentSources           = "agentSources"
	Skills                 = "skills"
	Browser                = "browser"
	BrowserDriver          = "browserDriver"
	BrowserSteps           = "browserSteps"
	BrowserModel           = "browserModel"
	BrowserEffort          = "browserEffort"
	BrowserCursor          = "browserCursor"
)

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
	ThinkingSummarized = "summarized"
	ThinkingOmitted    = "omitted"
)

const (
	GatePromptAuto = "auto"
	GatePromptAsk  = "ask"
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
		{Key: Composer, Label: "Composer", Description: "Enter sends; Shift+Enter inserts a line break", Category: "Interaction", Kind: Text, DefaultText: "standard",
			Choices: []string{"standard", "compact", "expanded"}},
		{Key: GatePrompt, Label: "Confirmations", Description: "auto lets jev decide at every gate and work never stops for you: only what it denies is refused, and what it would ask about runs and shows its verdict in the chat. ask stops on what jev would ask about until you answer. A project hook, a rule override and the model changing a setting ask you in both", Category: "Interaction", Kind: Text, DefaultText: GatePromptAuto,
			Choices: []string{GatePromptAuto, GatePromptAsk}, Aliases: map[string]string{"run": GatePromptAuto}},
		{Key: ReadBeforeEdit, Label: "Read before edit", Description: "an edit or a write to a file this session has not read, or that changed since it was read, is refused", Category: "Interaction", Kind: Bool, Default: 1},
		{Key: ChatShowsTools, Label: "Tool detail", Description: "chat shows every tool call", Category: "Context", Kind: Bool},
		{Key: AgentFeeds, Label: "Agent feeds", Description: "full keeps every sub-agents event, summary the latest 200, off shows only the list of agents", Category: "Context", Kind: Text, DefaultText: FeedsFull,
			Choices: []string{FeedsFull, FeedsSummary, FeedsOff}},
		{Key: ShowThinking, Label: "Thinking", Description: "thinking shows in each agent's sub-agents feed; t there toggles it", Category: "Context", Kind: Bool, Default: 1},
		{Key: ThinkingSummary, Label: "Thinking summary", Description: "summarized asks Claude for a summary of its thinking; omitted asks for none, so no thinking reaches the sub-agents feed", Category: "Context", Kind: Text, DefaultText: ThinkingSummarized,
			Choices: []string{ThinkingSummarized, ThinkingOmitted}},
		{Key: Images, Label: "Images", Description: "auto sends a pasted image only to a model that can see, inline always sends, off never attaches", Category: "Context", Kind: Text, DefaultText: ImagesAuto,
			Choices: []string{ImagesAuto, ImagesInline, ImagesOff}},
		{Key: ProjectInstructionsCap, Label: "Project instructions", Description: "bytes of your instruction files sent each turn", Category: "Context", Kind: Int, Default: konst.ProjectInstructionsBytesDefault,
			Least: 1, Most: konst.ProjectInstructionsBytesMost, Unit: "bytes of AGENTS.md and CLAUDE.md sent each turn"},
		{Key: InstructionSources, Label: "Instruction files", Description: "a folder with both AGENTS.md and CLAUDE.md sends one: agents-first sends AGENTS.md, claude-first sends CLAUDE.md, both sends both and costs more tokens. Read from the working directory up to its git root; the home directory gives only ~/.tofu/AGENTS.md", Category: "Context", Kind: Text, DefaultText: InstructionsAgentsFirst,
			Choices: []string{InstructionsAgentsFirst, InstructionsClaudeFirst, InstructionsBoth},
			Aliases: map[string]string{"AGENTS.md,CLAUDE.md": InstructionsAgentsFirst, "AGENTS.md": InstructionsAgentsFirst, "CLAUDE.md,AGENTS.md": InstructionsClaudeFirst, "CLAUDE.md": InstructionsClaudeFirst}},
		{Key: Skills, Label: "Skills", Description: "on lists the skills in .tofu, .agents and .claude skill folders to the model and offers the skill tool; off sends neither. The project gives all three, the home directory only ~/.tofu/skills", Category: "Context", Kind: Text, DefaultText: SkillsOn,
			Choices: []string{SkillsOn, SkillsOff}},
		{Key: Memory, Label: "Memory", Description: "on sends what you asked tofu to remember to the lead on every request and offers to keep a message that says remember; off sends none and offers nothing. tofu memory lists the entries", Category: "Context", Kind: Bool, Default: 1},
		{Key: AutoMemory, Label: "Auto memory", Description: "on keeps a message that says remember at once and prints the undo, instead of asking first; it turns on by itself once you accepted 7 of the first 10 offers, or when you answer always on one", Category: "Context", Kind: Bool},
		{Key: DiffContext, Label: "Diff context", Description: "Lines around changed hunks, at most the " + strconv.Itoa(konst.DiffContextLinesDefault) + " each diff carries", Category: "Files", Kind: Text, DefaultText: strconv.Itoa(konst.DiffContextLinesDefault),
			Choices: lineCounts(konst.DiffContextLinesTight, konst.DiffContextLinesDefault, konst.DiffContextLinesWide, konst.DiffContextLinesWidest)},
		{Key: Hyperlinks, Label: "Hyperlinks", Description: "OSC 8 terminal file links; off prints the bare path", Category: "Files", Kind: Text, DefaultText: LinksAuto,
			Choices: []string{LinksAuto, LinksOn, LinksOff}},
		{Key: GroupByAgent, Label: "Group by agent", Description: "Author filters in edit feeds", Category: "Files", Kind: Bool, Default: 1},
		{Key: PersistentRegistry, Label: "Persistent registry", Description: "a running shell outlives tofu and the next launch lists it; off stops every shell on exit, even when tofu is killed", Category: "Shell", Kind: Bool, Restart: true},
		{Key: LogTail, Label: "Log tail", Description: "Lines retained per shell", Category: "Shell", Kind: Text, DefaultText: strconv.Itoa(konst.ShellLogTailLinesDefault),
			Choices: lineCounts(konst.ShellLogTailLinesShort, konst.ShellLogTailLinesDefault, konst.ShellLogTailLinesLong)},
		{Key: KillConfirm, Label: "Kill confirm", Description: "Confirm process termination", Category: "Shell", Kind: Bool, Default: 1},
		{Key: Shell, Label: "Shell", Description: "a path, or the word wsl; empty resolves the person's own shell automatically", Category: "Shell", Kind: Text},
		{Key: FoldHidesShell, Label: "Fold hides shell", Description: "the running line drops the shell count", Category: "Shell", Kind: Bool},
		{Key: DecisionCap, Label: "Decision cap", Description: "decision cap per turn, zero means no cap", Category: "Turn", Kind: Int,
			Least: 0, Most: konst.DecisionCapMost, Unit: "decisions a turn, where 0 means no cap"},
		{Key: TurnMaySpawn, Label: "Sub-agents", Description: "a turn may spawn a sub-agent", Category: "Turn", Kind: Bool, Default: 1},
		{Key: OneTurnPerProject, Label: "One turn per project", Description: "while one session runs a turn in this project, a turn from any other session is refused; off lets several sessions run turns in one repository at once", Category: "Turn", Kind: Bool, Default: 1},
		{Key: SubAgentsPerTurn, Label: "Sub-agents running at once", Description: "how many sub-agents may run at once; a spawn past it is refused until one ends, and the next spawn reads the new value", Category: "Turn", Kind: Int, Default: konst.SubAgentsPerTurnDefault,
			Least: 1, Most: math.MaxInt32, Unit: "sub-agents running at once"},
		{Key: SubAgentDepth, Label: "Sub-agent depth", Description: "how deep a sub-agent may spawn its own sub-agents; the next spawn reads the new value", Category: "Turn", Kind: Int, Default: konst.SubAgentDepthDefault,
			Least: 1, Most: math.MaxInt32, Unit: "levels of sub-agents"},
		{Key: SubAgentCheckSeconds, Label: "Sub-agent check", Description: "every this many seconds the lead is sent a check on each running sub-agent, built from what tofu records with no model call: its steps, the files it changed, its gate and its last tool. 0 sends none; the next spawn reads the new value", Category: "Turn", Kind: Int, Default: konst.SubAgentCheckSecondsDefault,
			Least: 0, Most: math.MaxInt32, Unit: "seconds between checks on a running sub-agent, where 0 sends none"},
		{Key: SubAgentWatchSeconds, Label: "Sub-agent watch", Description: "a sub-agent call open this many seconds gets a line in the chat saying what it is doing: waiting for the orchestrator's answer, running bash or building; a sub-agent with no new output, step or request for this long is shown as stalled", Category: "Turn", Kind: Int, Default: konst.SubAgentWatchSecondsDefault,
			Least: 1, Most: math.MaxInt32, Unit: "seconds before a long call is named, and before a sub-agent with no progress is stalled"},
		{Key: VerifySubAgents, Label: "Check sub-agents", Description: "the lead checks each sub-agent's work itself before it reports, reading the files and running the build or the tests again; off takes the report as the result unless you ask for a check. A text override of the rule verify_sub_agents in tofu rules turns it on too", Category: "Turn", Kind: Bool},
		{Key: AgentSources, Label: "Agent folders", Description: "the folders sub-agents are read from, in order, as a comma list of tofu, agents and claude, inside the project; the home directory gives only ~/.tofu/agents, and the library is always read", Category: "Turn", Kind: Text, DefaultText: "tofu,agents,claude",
			ListOf: []string{"tofu", "agents", "claude"}},
		{Key: subagent.TierGenius.Setting(), Label: "Genius tier", Description: "the model @genius names, and opus in a shared agent file; empty runs the orchestrator's model", Category: "Turn", Kind: Text},
		{Key: subagent.TierSmart.Setting(), Label: "Smart tier", Description: "the model @smart names, and sonnet in a shared agent file; empty runs the orchestrator's model", Category: "Turn", Kind: Text},
		{Key: subagent.TierWorker.Setting(), Label: "Worker tier", Description: "the model @worker names, and haiku in a shared agent file; empty runs the orchestrator's model", Category: "Turn", Kind: Text},
		{Key: subagent.TierDumb.Setting(), Label: "Dumb tier", Description: "the model @dumb names; empty runs the orchestrator's model", Category: "Turn", Kind: Text},
		{Key: Browser, Label: "Browser", Description: "drive lets the model read your Chrome tabs and click and type in them; read lets it only read them; off offers no browser tool", Category: "Browser", Kind: Text, DefaultText: BrowserDrive,
			Choices: []string{BrowserOff, BrowserRead, BrowserDrive}},
		{Key: BrowserDriver, Label: "Browser driver", Description: "subagent gives the browser steps to a browser sub-agent on browserModel, which the model spawns with a task and a tab; steps gives the model browser_observe and browser_act itself; goal gives it browser_do, where jev picks each step toward a whole goal", Category: "Browser", Kind: Text, DefaultText: DriverSubagent,
			Choices: []string{DriverSubagent, DriverSteps, DriverGoal}, Aliases: map[string]string{"jev": DriverGoal, "model": DriverSteps}, Formerly: "browserChooser"},
		{Key: BrowserSteps, Label: "Browser steps", Description: "how many actions one browser task may take", Category: "Browser", Kind: Int, Default: konst.BrowserStepsDefault,
			Least: 1, Most: konst.BrowserActionCeiling, Unit: "actions a browser task"},
		{Key: BrowserModel, Label: "Browser model", Description: "the subscription model a browser task asks for the text to type and for its answer, as source/model; empty takes the dumb tier, then the worker tier, then the turn's own model", Category: "Browser", Kind: Text},
		{Key: BrowserEffort, Label: "Browser effort", Description: "the effort the browser sub-agent runs at, when its model lists it; empty runs it at the model's own default, the lowest it lists at or above low", Category: "Browser", Kind: Text,
			Choices: []string{"", "low", "medium", "high", "xhigh", "max"}},
		{Key: BrowserCursor, Label: "Browser cursor", Description: "a small cursor labelled tofu glides to each click in tofu's own tab, so you can see where it acts; it is drawn only, and never changes what tofu reads or clicks; read when Chrome starts the relay", Category: "Browser", Kind: Bool, Default: 1},
	}
	for i := range specs {
		specs[i].Group = specs[i].Category
	}
	return specs
}
