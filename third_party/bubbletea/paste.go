package tea

type PasteMsg struct {
	Content string
}

func (p PasteMsg) String() string {
	return p.Content
}

type PasteStartMsg struct{}

type PasteEndMsg struct{}
