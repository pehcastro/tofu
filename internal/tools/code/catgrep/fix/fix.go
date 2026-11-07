package fix

import (
	"tofu/internal/tools/code/catgrep/pattern"
	"tofu/internal/tools/code/catgrep/replace"
	"tofu/internal/tools/code/catgrep/transform"
)

type Fixer struct {
	Template   string
	Transforms transform.Set
}

type Edit struct {
	Start, End uint32
	Text       string
}

func (f Fixer) Edit(p pattern.Pattern, m pattern.Match) (Edit, error) {
	if err := f.Transforms.Apply(m.Env); err != nil {
		return Edit{}, err
	}
	text, err := replace.Text(f.Template, m.Env)
	if err != nil {
		return Edit{}, err
	}
	start := m.Node.Start()
	end, ok := p.MatchEnd(m.Node)
	if !ok {
		end = m.Node.End()
	}
	return Edit{Start: start, End: end, Text: replace.Reindent(text, 0, replace.IndentAt(m.Node.Source()[:start]))}, nil
}
