package keyfield

import (
	"io"

	tea "charm.land/bubbletea/v2"
)

const promptHints = "enter saves · esc cancels"

type Cancelled struct{}

func (Cancelled) Error() string { return "no key was entered" }

type prompt struct {
	field     Field
	title     string
	entered   bool
	cancelled bool
}

func (p prompt) Init() tea.Cmd { return nil }

func (p prompt) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.PasteMsg:
		p.field.Paste(msg.Content)
	case tea.KeyPressMsg:
		switch key := msg.String(); key {
		case "esc", "ctrl+c":
			p.cancelled = true
			return p, tea.Quit
		case "enter":
			if p.entered = p.field.Enter(); p.entered {
				return p, tea.Quit
			}
		default:
			p.field.Type(key)
		}
	}
	return p, nil
}

func (p prompt) View() tea.View {
	text := p.title + "\n" + p.field.Line() + "\n"
	switch {
	case p.cancelled, p.entered:
		return tea.NewView(text)
	case p.field.Warning() != "":
		text += p.field.Warning()
	default:
		text += promptHints
	}
	return tea.NewView(text + "\n")
}

func Read(in io.Reader, out io.Writer, title, variable string) (string, error) {
	final, err := tea.NewProgram(prompt{field: New(variable), title: title}, tea.WithInput(in), tea.WithOutput(out)).Run()
	if err != nil {
		return "", err
	}
	if answered := final.(prompt); answered.entered {
		return answered.field.Value(), nil
	}
	return "", Cancelled{}
}
