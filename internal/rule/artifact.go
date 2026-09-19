package rule

type Artifact interface {
	artifact()
}

type GoFile struct {
	Path string
}

func (GoFile) artifact() {}

type TextFile struct {
	Path string
}

func (TextFile) artifact() {}

type OwnsWrite struct {
	Path string
	Owns []string
}

func (OwnsWrite) artifact() {}

type ShellCommand struct {
	Argv          []string
	HeredocBodies []string
}

func (ShellCommand) artifact() {}
