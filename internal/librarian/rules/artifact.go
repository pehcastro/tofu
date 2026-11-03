package rule

type Subject string

const (
	SubjectGoFile       Subject = "go_file"
	SubjectGoPackage    Subject = "go_package"
	SubjectTextFile     Subject = "text_file"
	SubjectOwnsWrite    Subject = "owns_write"
	SubjectShellCommand Subject = "shell_command"
)

type Artifact interface {
	artifact()
}

type GoFile struct {
	Path string
}

func (GoFile) artifact() {}

type GoPackage struct {
	Dir string
}

func (GoPackage) artifact() {}

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
