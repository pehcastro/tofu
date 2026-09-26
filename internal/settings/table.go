package settings

import (
	"slices"
	"strconv"

	"tofu/internal/konst"
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
	Restart     bool
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
	HideIntroduction       = "hideIntroduction"
)

const (
	GatePromptRun = "run"
	GatePromptAsk = "ask"
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
	return len(s.Choices) == 0 || slices.Contains(s.Choices, value)
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
		{Key: Composer, Label: "Composer", Description: "Enter sends; Ctrl+J inserts newline", Category: "Interaction", Kind: Text, DefaultText: "standard",
			Choices: []string{"standard", "compact", "expanded"}},
		{Key: GatePrompt, Label: "Confirmations", Description: "run lets a gated call go, ask waits for you; a rule's shadow or enforced is a different switch", Category: "Interaction", Kind: Text, DefaultText: GatePromptRun,
			Choices: []string{GatePromptRun, GatePromptAsk}, Restart: true},
		{Key: ReadBeforeEdit, Label: "Read before edit", Description: "an edit or a write to a file this turn has not read is refused", Category: "Interaction", Kind: Bool, Default: 1, Restart: true},
		{Key: Compaction, Label: "Compaction", Description: "Automatic transcript compaction", Category: "Context", Kind: Text, DefaultText: "adaptive",
			Choices: []string{"adaptive", "manual", "off"}},
		{Key: ChatShowsTools, Label: "Tool detail", Description: "chat shows every tool call", Category: "Context", Kind: Bool},
		{Key: AgentFeeds, Label: "Agent feeds", Description: "Retain authored activity history", Category: "Context", Kind: Text, DefaultText: "full",
			Choices: []string{"full", "summary", "off"}},
		{Key: Images, Label: "Images", Description: "Inline terminal image protocol", Category: "Context", Kind: Text, DefaultText: "auto",
			Choices: []string{"auto", "inline", "off"}},
		{Key: ProjectInstructionsCap, Label: "Project instructions", Description: "bytes of your instruction files sent each turn, below one restores the default", Category: "Context", Kind: Int, Default: konst.ProjectInstructionsBytesDefault, Restart: true},
		{Key: DiffContext, Label: "Diff context", Description: "Lines around changed hunks", Category: "Files", Kind: Text, DefaultText: strconv.Itoa(konst.DiffContextLinesDefault),
			Choices: lineCounts(konst.DiffContextLinesTight, konst.DiffContextLinesDefault, konst.DiffContextLinesWide, konst.DiffContextLinesWidest)},
		{Key: Hyperlinks, Label: "Hyperlinks", Description: "OSC 8 terminal file links", Category: "Files", Kind: Text, DefaultText: "auto",
			Choices: []string{"auto", "on", "off"}},
		{Key: GroupByAgent, Label: "Group by agent", Description: "Author filters in edit feeds", Category: "Files", Kind: Bool, Default: 1},
		{Key: PersistentRegistry, Label: "Persistent registry", Description: "Keep process metadata", Category: "Shell", Kind: Bool, Default: 1},
		{Key: LogTail, Label: "Log tail", Description: "Lines retained per shell", Category: "Shell", Kind: Text, DefaultText: strconv.Itoa(konst.ShellLogTailLinesDefault),
			Choices: lineCounts(konst.ShellLogTailLinesShort, konst.ShellLogTailLinesDefault, konst.ShellLogTailLinesLong)},
		{Key: KillConfirm, Label: "Kill confirm", Description: "Confirm process termination", Category: "Shell", Kind: Bool, Default: 1},
		{Key: Shell, Label: "Shell", Description: "a path, or the word wsl; empty resolves the person's own shell automatically", Category: "Shell", Kind: Text, Restart: true},
		{Key: FoldHidesShell, Label: "Fold hides shell", Description: "the running line drops the shell count", Category: "Shell", Kind: Bool},
		{Key: DecisionCap, Label: "Decision cap", Description: "decision cap per turn, zero means no cap", Category: "Turn", Kind: Int, Restart: true},
		{Key: TurnMaySpawn, Label: "Sub-agents", Description: "a turn may spawn a sub-agent", Category: "Turn", Kind: Bool, Default: 1, Restart: true},
		{Key: HideIntroduction, Label: "Hide introduction", Description: "Skip the new-project welcome and open chat directly", Category: "Startup", Kind: Bool},
	}
	for i := range specs {
		specs[i].Group = specs[i].Category
	}
	return specs
}
