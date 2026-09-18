package question

import "boji/internal/judge/jev"

type Kind string

const (
	KindNoul   Kind = "noul"
	KindChoice Kind = "choice"
	KindScore  Kind = "score"
)

type CriteriaKind int

const (
	CriteriaNone CriteriaKind = iota
	CriteriaText
	CriteriaFields
)

type CriteriaField struct {
	Name string
	Text string
}

type Criteria struct {
	Kind   CriteriaKind
	Text   string
	Fields []CriteriaField
}

func (c Criteria) Words() []string {
	switch c.Kind {
	case CriteriaNone:
		return nil
	case CriteriaText:
		return []string{c.Text}
	case CriteriaFields:
		var out []string
		for _, f := range c.Fields {
			out = append(out, f.Text)
		}
		return out
	}
	return nil
}

type Option struct {
	Name     string
	Criteria Criteria
}

type Question struct {
	Name         string
	Kind         Kind
	Instructions string
	True         Criteria
	False        Criteria
	Levels       []Criteria
	Options      []Option
	Escape       string
	File         string
	Line         int
}

func (q Question) Words() []string {
	out := []string{q.Instructions}
	out = append(out, q.True.Words()...)
	out = append(out, q.False.Words()...)
	for _, l := range q.Levels {
		out = append(out, l.Words()...)
	}
	for _, o := range q.Options {
		out = append(out, o.Criteria.Words()...)
	}
	return out
}

type Set struct {
	Name             string
	Version          int
	QuestionsVersion int
	State            []string
	Questions        []Question
	File             string
}

func (s Set) Question(name string) (Question, bool) {
	for _, q := range s.Questions {
		if q.Name == name {
			return q, true
		}
	}
	return Question{}, false
}

func (q Question) ToJev() jev.Question {
	jq := jev.Question{ID: q.Name, Instructions: q.Instructions}
	switch q.Kind {
	case KindNoul:
		jq.Kind = jev.QuestionNoul
		jq.True, jq.False = q.True.Text, q.False.Text
	case KindChoice:
		jq.Kind = jev.QuestionChoice
		for _, option := range q.Options {
			jq.Options = append(jq.Options, jev.Option{Name: option.Name, Criteria: option.Criteria.jevValue()})
		}
	case KindScore:
		jq.Kind = jev.QuestionScore
		for _, level := range q.Levels {
			jq.Levels = append(jq.Levels, level.Text)
		}
	default:
		panic("question: unknown question kind " + string(q.Kind))
	}
	return jq
}

func (c Criteria) jevValue() any {
	switch c.Kind {
	case CriteriaNone:
		return nil
	case CriteriaText:
		return c.Text
	case CriteriaFields:
		fields := make(map[string]string, len(c.Fields))
		for _, f := range c.Fields {
			fields[f.Name] = f.Text
		}
		return fields
	}
	panic("question: unknown criteria kind")
}
