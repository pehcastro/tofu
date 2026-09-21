package settings

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
	ChatShowsTools = "chatShowsTools"
	FoldHidesShell = "foldHidesShell"
	DecisionCap    = "decisionCap"
)

func Default() []Spec {
	return []Spec{
		{Key: ChatShowsTools, Label: "chat shows every tool call", Group: "chat", Kind: Bool, Default: 0, Restart: false},
		{Key: DecisionCap, Label: "decision cap per turn, zero means no cap", Group: "turn", Kind: Int, Default: 0, Restart: true},
		{Key: FoldHidesShell, Label: "the running line drops the shell count", Group: "chat", Kind: Bool, Default: 0, Restart: false},
	}
}
