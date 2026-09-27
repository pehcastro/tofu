package settings

import (
	"cmp"
	"errors"
	"fmt"
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
	Compaction             = "compaction"
	ChatShowsTools         = "chatShowsTools"
	AgentFeeds             = "agentFeeds"
	Images                 = "images"
	ProjectInstructionsCap = "projectInstructionsCap"
	InstructionSources     = "instructionSources"
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
	AgentSources           = "agentSources"
	HideIntroduction       = "hideIntroduction"
	Skills                 = "skills"
)

const (
	SkillsOn  = "on"
	SkillsOff = "off"
)

const (
	GatePromptRun = "run"
	GatePromptAsk = "ask"
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
		{Key: GatePrompt, Label: "Confirmations", Description: "run lets a gated call go, ask waits for you; a rule's shadow or enforced is a different switch", Category: "Interaction", Kind: Text, DefaultText: GatePromptRun,
			Choices: []string{GatePromptRun, GatePromptAsk}, Restart: true},
		{Key: ReadBeforeEdit, Label: "Read before edit", Description: "an edit or a write to a file this session has not read, or that changed since it was read, is refused", Category: "Interaction", Kind: Bool, Default: 1, Restart: true},
		{Key: Compaction, Label: "Compaction", Description: "Automatic transcript compaction; not built yet, so no choice changes a turn", Category: "Context", Kind: Text, DefaultText: "adaptive",
			Choices: []string{"adaptive", "manual", "off"}},
		{Key: ChatShowsTools, Label: "Tool detail", Description: "chat shows every tool call", Category: "Context", Kind: Bool},
		{Key: AgentFeeds, Label: "Agent feeds", Description: "full keeps every sub-agents event, summary the latest 200, off shows only the list of agents", Category: "Context", Kind: Text, DefaultText: FeedsFull,
			Choices: []string{FeedsFull, FeedsSummary, FeedsOff}},
		{Key: Images, Label: "Images", Description: "auto sends a pasted image only to a model that can see, inline always sends, off never attaches", Category: "Context", Kind: Text, DefaultText: ImagesAuto,
			Choices: []string{ImagesAuto, ImagesInline, ImagesOff}},
		{Key: ProjectInstructionsCap, Label: "Project instructions", Description: "bytes of your instruction files sent each turn", Category: "Context", Kind: Int, Default: konst.ProjectInstructionsBytesDefault, Restart: true,
			Least: 1, Most: konst.ProjectInstructionsBytesMost, Unit: "bytes of AGENTS.md and CLAUDE.md sent each turn"},
		{Key: InstructionSources, Label: "Instruction files", Description: "in each folder the first of these that exists is sent and the rest are skipped, as a comma list of AGENTS.md and CLAUDE.md", Category: "Context", Kind: Text, DefaultText: "AGENTS.md,CLAUDE.md", Restart: true,
			ListOf: []string{"AGENTS.md", "CLAUDE.md"}},
		{Key: Skills, Label: "Skills", Description: "on lists the skills in .tofu, .agents and .claude skill folders to the model and offers the skill tool; off sends neither", Category: "Context", Kind: Text, DefaultText: SkillsOn, Restart: true,
			Choices: []string{SkillsOn, SkillsOff}},
		{Key: DiffContext, Label: "Diff context", Description: "Lines around changed hunks, at most the " + strconv.Itoa(konst.DiffContextLinesDefault) + " each diff carries", Category: "Files", Kind: Text, DefaultText: strconv.Itoa(konst.DiffContextLinesDefault),
			Choices: lineCounts(konst.DiffContextLinesTight, konst.DiffContextLinesDefault, konst.DiffContextLinesWide, konst.DiffContextLinesWidest)},
		{Key: Hyperlinks, Label: "Hyperlinks", Description: "OSC 8 terminal file links; off prints the bare path", Category: "Files", Kind: Text, DefaultText: LinksAuto,
			Choices: []string{LinksAuto, LinksOn, LinksOff}},
		{Key: GroupByAgent, Label: "Group by agent", Description: "Author filters in edit feeds", Category: "Files", Kind: Bool, Default: 1},
		{Key: PersistentRegistry, Label: "Persistent registry", Description: "a running shell outlives tofu and the next launch lists it; off stops every shell on exit", Category: "Shell", Kind: Bool, Default: 1, Restart: true},
		{Key: LogTail, Label: "Log tail", Description: "Lines retained per shell", Category: "Shell", Kind: Text, DefaultText: strconv.Itoa(konst.ShellLogTailLinesDefault),
			Choices: lineCounts(konst.ShellLogTailLinesShort, konst.ShellLogTailLinesDefault, konst.ShellLogTailLinesLong)},
		{Key: KillConfirm, Label: "Kill confirm", Description: "Confirm process termination", Category: "Shell", Kind: Bool, Default: 1},
		{Key: Shell, Label: "Shell", Description: "a path, or the word wsl; empty resolves the person's own shell automatically", Category: "Shell", Kind: Text, Restart: true},
		{Key: FoldHidesShell, Label: "Fold hides shell", Description: "the running line drops the shell count", Category: "Shell", Kind: Bool},
		{Key: DecisionCap, Label: "Decision cap", Description: "decision cap per turn, zero means no cap", Category: "Turn", Kind: Int, Restart: true,
			Least: 0, Most: konst.DecisionCapMost, Unit: "decisions a turn, where 0 means no cap"},
		{Key: TurnMaySpawn, Label: "Sub-agents", Description: "a turn may spawn a sub-agent", Category: "Turn", Kind: Bool, Default: 1, Restart: true},
		{Key: AgentSources, Label: "Agent folders", Description: "the folders sub-agents are read from, in order, as a comma list of tofu, agents and claude; the library is always read", Category: "Turn", Kind: Text, DefaultText: "tofu,agents,claude",
			ListOf: []string{"tofu", "agents", "claude"}},
		{Key: subagent.TierGenius.Setting(), Label: "Genius tier", Description: "the model @genius names, and opus in a shared agent file; empty runs the orchestrator's model", Category: "Turn", Kind: Text},
		{Key: subagent.TierSmart.Setting(), Label: "Smart tier", Description: "the model @smart names, and sonnet in a shared agent file; empty runs the orchestrator's model", Category: "Turn", Kind: Text},
		{Key: subagent.TierWorker.Setting(), Label: "Worker tier", Description: "the model @worker names, and haiku in a shared agent file; empty runs the orchestrator's model", Category: "Turn", Kind: Text},
		{Key: subagent.TierDumb.Setting(), Label: "Dumb tier", Description: "the model @dumb names; empty runs the orchestrator's model", Category: "Turn", Kind: Text},
		{Key: HideIntroduction, Label: "Hide introduction", Description: "Skip the new-project welcome and open chat directly", Category: "Startup", Kind: Bool},
	}
	for i := range specs {
		specs[i].Group = specs[i].Category
	}
	return specs
}
