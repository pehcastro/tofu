package tea

type ClipboardMsg struct {
	Content   string
	Selection byte
}

func (e ClipboardMsg) Clipboard() byte {
	return e.Selection
}

func (e ClipboardMsg) String() string {
	return e.Content
}

type setClipboardMsg string

func SetClipboard(s string) Cmd {
	return func() Msg {
		return setClipboardMsg(s)
	}
}

type readClipboardMsg struct{}

func ReadClipboard() Msg {
	return readClipboardMsg{}
}

type setPrimaryClipboardMsg string

func SetPrimaryClipboard(s string) Cmd {
	return func() Msg {
		return setPrimaryClipboardMsg(s)
	}
}

type readPrimaryClipboardMsg struct{}

func ReadPrimaryClipboard() Msg {
	return readPrimaryClipboardMsg{}
}
