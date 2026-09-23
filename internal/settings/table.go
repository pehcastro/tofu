package settings

import "tofu/internal/konst"

type Kind int

const (
	Bool Kind = iota
	Int
)

type Spec struct {
	Key     string
	Label   string
	Group   string
	Kind    Kind
	Default int
	Restart bool
}

const (
	ChatShowsTools         = "chatShowsTools"
	FoldHidesShell         = "foldHidesShell"
	DecisionCap            = "decisionCap"
	ProjectInstructionsCap = "projectInstructionsCap"
	TurnMaySpawn           = "turnMaySpawn"
	ReadBeforeEdit         = "readBeforeEdit"
)

func DeclaredDefault(key string) int {
	for _, spec := range Default() {
		if spec.Key == key {
			return spec.Default
		}
	}
	return 0
}

func Default() []Spec {
	return []Spec{
		{Key: ChatShowsTools, Label: "chat shows every tool call", Group: "chat", Kind: Bool, Default: 0, Restart: false},
		{Key: DecisionCap, Label: "decision cap per turn, zero means no cap", Group: "turn", Kind: Int, Default: 0, Restart: true},
		{Key: FoldHidesShell, Label: "the running line drops the shell count", Group: "chat", Kind: Bool, Default: 0, Restart: false},
		{Key: ProjectInstructionsCap, Label: "bytes of your instruction files sent each turn, below one restores the default", Group: "turn", Kind: Int, Default: konst.ProjectInstructionsBytesDefault, Restart: true},
		{Key: TurnMaySpawn, Label: "a turn may spawn a sub-agent", Group: "turn", Kind: Bool, Default: 1, Restart: true},
		{Key: ReadBeforeEdit, Label: "an edit or a write to a file this turn has not read is refused", Group: "turn", Kind: Bool, Default: 1, Restart: true},
	}
}
