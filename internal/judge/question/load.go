package question

import (
	"fmt"
	"strconv"
	"strings"

	"tofu/internal/sys"
)

func Load(path string) (Set, error) {
	data, err := sys.ReadFile(path)
	if err != nil {
		return Set{}, err
	}
	base := path
	if i := strings.LastIndexAny(base, "/\\"); i >= 0 {
		base = base[i+1:]
	}
	name, version, ok := splitFileName(base)
	if !ok {
		return Set{}, fmt.Errorf("%s: a question file is named <set>@<version>.yaml", path)
	}
	root, err := parse(path, data)
	if err != nil {
		return Set{}, err
	}
	return decode(root, name, version, path)
}

func decode(root *node, name string, version int, file string) (Set, error) {
	if root == nil || root.kind != nodeMap {
		return Set{}, fmt.Errorf("%s: a question file is a map", file)
	}
	set := Set{Name: name, Version: version, File: file}
	if n, ok := root.child("name"); ok && n.kind == nodeScalar && n.text != "" {
		set.Name = n.text
	}
	if n, ok := root.child("questions_version"); ok && n.kind == nodeScalar && n.text != "" {
		v, err := strconv.Atoi(n.text)
		if err != nil {
			return Set{}, fmt.Errorf("%s:%d: questions_version is a whole number, found %q", n.file, n.line, n.text)
		}
		set.QuestionsVersion = v
	}
	if n, ok := root.child("state"); ok && n.kind == nodeList {
		for _, item := range n.items {
			if item.kind != nodeScalar {
				return Set{}, fmt.Errorf("%s:%d: a state field is a name", item.file, item.line)
			}
			set.State = append(set.State, item.text)
		}
	}
	qs, ok := root.child("questions")
	if !ok || qs.kind != nodeMap {
		return Set{}, fmt.Errorf("%s: the file declares no questions", file)
	}
	for _, key := range qs.keys {
		q, err := decodeQuestion(key, qs.fields[key])
		if err != nil {
			return Set{}, err
		}
		set.Questions = append(set.Questions, q)
	}
	return set, nil
}

func decodeQuestion(name string, n *node) (Question, error) {
	if n == nil || n.kind != nodeMap {
		return Question{}, fmt.Errorf("%s:%d: the question %q is a map", n.file, n.line, name)
	}
	q := Question{Name: name, File: n.file, Line: n.line}
	t, ok := n.child("type")
	if !ok || t.kind != nodeScalar {
		return Question{}, fmt.Errorf("%s:%d: the question %q declares no type", n.file, n.line, name)
	}
	switch Kind(t.text) {
	case KindNoul:
		q.Kind = KindNoul
	case KindChoice:
		q.Kind = KindChoice
	case KindScore:
		q.Kind = KindScore
	default:
		return Question{}, fmt.Errorf("%s:%d: the question %q has the unknown type %q", t.file, t.line, name, t.text)
	}
	if i, ok := n.child("instructions"); ok && i.kind == nodeScalar {
		q.Instructions = i.text
	}
	crit, hasCrit := n.child("criteria")
	switch q.Kind {
	case KindNoul:
		if hasCrit {
			if crit.kind != nodeMap {
				return Question{}, fmt.Errorf("%s:%d: the criteria of the noul %q is a true and false map", crit.file, crit.line, name)
			}
			if c, ok := crit.child("true"); ok {
				q.True = decodeCriteria(c)
			}
			if c, ok := crit.child("false"); ok {
				q.False = decodeCriteria(c)
			}
		}
	case KindScore:
		if hasCrit {
			if crit.kind != nodeList {
				return Question{}, fmt.Errorf("%s:%d: the criteria of the score %q is a list of levels", crit.file, crit.line, name)
			}
			for _, item := range crit.items {
				q.Levels = append(q.Levels, decodeCriteria(item))
			}
		}
	case KindChoice:
		opts, ok := n.child("options")
		if ok {
			if opts.kind != nodeList {
				return Question{}, fmt.Errorf("%s:%d: the options of the choice %q are a list", opts.file, opts.line, name)
			}
			for _, item := range opts.items {
				o, err := decodeOption(name, item)
				if err != nil {
					return Question{}, err
				}
				q.Options = append(q.Options, o)
			}
		}
		if e, ok := n.child("escape"); ok && e.kind == nodeScalar {
			q.Escape = e.text
		}
	}
	return q, nil
}

func decodeOption(question string, n *node) (Option, error) {
	if n.kind == nodeScalar {
		return Option{Name: n.text}, nil
	}
	if n.kind != nodeMap {
		return Option{}, fmt.Errorf("%s:%d: an option of %q is a name or a map", n.file, n.line, question)
	}
	name, ok := n.child("name")
	if !ok || name.kind != nodeScalar || name.text == "" {
		return Option{}, fmt.Errorf("%s:%d: an option of %q has no name", n.file, n.line, question)
	}
	o := Option{Name: name.text}
	if c, ok := n.child("criteria"); ok {
		o.Criteria = decodeCriteria(c)
	}
	return o, nil
}

func decodeCriteria(n *node) Criteria {
	switch n.kind {
	case nodeScalar:
		if n.text == "" {
			return Criteria{Kind: CriteriaNone}
		}
		return Criteria{Kind: CriteriaText, Text: n.text}
	case nodeMap:
		c := Criteria{Kind: CriteriaFields}
		for _, k := range n.keys {
			f := n.fields[k]
			if f.kind != nodeScalar {
				continue
			}
			c.Fields = append(c.Fields, CriteriaField{Name: k, Text: f.text})
		}
		return c
	case nodeList:
		var parts []string
		for _, item := range n.items {
			if item.kind == nodeScalar {
				parts = append(parts, item.text)
			}
		}
		return Criteria{Kind: CriteriaText, Text: strings.Join(parts, " ")}
	}
	return Criteria{Kind: CriteriaNone}
}
