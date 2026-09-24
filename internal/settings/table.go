package settings

import "tofu/internal/konst"

type Kind int

const (
	Bool Kind = iota
	Int
	Text
)

type Spec struct {
	Key         string
	Label       string
	Group       string
	Kind        Kind
	Default     int
	DefaultText string
	Restart     bool
}

const (
	ChatShowsTools         = "chatShowsTools"
	FoldHidesShell         = "foldHidesShell"
	DecisionCap            = "decisionCap"
	ProjectInstructionsCap = "projectInstructionsCap"
	TurnMaySpawn           = "turnMaySpawn"
	ReadBeforeEdit         = "readBeforeEdit"
	Shell                  = "shell"
	GatePrompt             = "gatePrompt"
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

func Default() []Spec {
	return []Spec{
		{Key: ChatShowsTools, Label: "chat shows every tool call", Group: "chat", Kind: Bool, Default: 0, Restart: false},
		{Key: DecisionCap, Label: "decision cap per turn, zero means no cap", Group: "turn", Kind: Int, Default: 0, Restart: true},
		{Key: FoldHidesShell, Label: "the running line drops the shell count", Group: "chat", Kind: Bool, Default: 0, Restart: false},
		{Key: ProjectInstructionsCap, Label: "bytes of your instruction files sent each turn, below one restores the default", Group: "turn", Kind: Int, Default: konst.ProjectInstructionsBytesDefault, Restart: true},
		{Key: TurnMaySpawn, Label: "a turn may spawn a sub-agent", Group: "turn", Kind: Bool, Default: 1, Restart: true},
		{Key: ReadBeforeEdit, Label: "an edit or a write to a file this turn has not read is refused", Group: "turn", Kind: Bool, Default: 1, Restart: true},
		{Key: Shell, Label: "shell override: a path, or the word wsl; empty resolves the person's own shell automatically", Group: "turn", Kind: Text, Restart: true},
		{Key: GatePrompt, Label: "run lets a gated call go, ask waits for you; a rule's shadow or enforced is a different switch", Group: "turn", Kind: Text, DefaultText: GatePromptRun, Restart: true},
	}
}
