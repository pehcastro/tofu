package tea

type TerminalVersionMsg struct {
	Name string
}

func (t TerminalVersionMsg) String() string {
	return t.Name
}

type terminalVersion struct{}

func RequestTerminalVersion() Msg {
	return terminalVersion{}
}
