package crew

import (
	"strconv"
	"strings"
)

type Kind int

const (
	Blocking Kind = iota
	Assumption
	Deferred
	Grant
)

func (k Kind) String() string {
	switch k {
	case Blocking:
		return "blocking"
	case Assumption:
		return "assumption"
	case Deferred:
		return "deferred"
	case Grant:
		return "grant"
	}
	panic("crew: unknown question kind " + strconv.Itoa(int(k)))
}

type UnknownKindError struct {
	Name string
}

func (e UnknownKindError) Error() string {
	return "question kind " + strconv.Quote(e.Name) + " is not one of blocking, assumption, deferred, grant"
}

func ParseKind(name string) (Kind, error) {
	for _, kind := range []Kind{Blocking, Assumption, Deferred, Grant} {
		if kind.String() == name {
			return kind, nil
		}
	}
	return 0, UnknownKindError{Name: name}
}

type Question struct {
	Ticket  string
	Kind    Kind
	Where   string
	Ask     string
	Default string
	Answer  string
}

const questionHeading = "### question · "

var questionFieldOrder = []string{"where", "ask", "default", "answer"}

func (q Question) Block() string {
	block := &strings.Builder{}
	block.WriteString(questionHeading + q.Kind.String() + " · " + q.Ticket + "\n\n")
	for i, value := range []string{q.Where, q.Ask, q.Default, q.Answer} {
		block.WriteString(questionFieldOrder[i] + ":")
		if value != "" {
			block.WriteString(" " + value)
		}
		block.WriteString("\n")
	}
	return block.String()
}

type MalformedBlockError struct {
	Reason string
}

func (e MalformedBlockError) Error() string {
	return "question block is malformed: " + e.Reason
}

func ParseBlock(block string) (Question, error) {
	lines := strings.Split(strings.TrimRight(block, "\n"), "\n")
	heading, rest := lines[0], lines[1:]
	if !strings.HasPrefix(heading, questionHeading) {
		return Question{}, MalformedBlockError{Reason: "the heading does not start with " + strconv.Quote(questionHeading)}
	}
	named := strings.Split(strings.TrimPrefix(heading, questionHeading), " · ")
	if len(named) != 2 || named[1] == "" {
		return Question{}, MalformedBlockError{Reason: "the heading does not name a kind and a ticket"}
	}
	kind, err := ParseKind(named[0])
	if err != nil {
		return Question{}, err
	}

	values := make([]string, len(questionFieldOrder))
	next := 0
	for _, line := range rest {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if next == len(questionFieldOrder) {
			return Question{}, MalformedBlockError{Reason: "there is a line after the answer: " + strconv.Quote(line)}
		}
		want := questionFieldOrder[next] + ":"
		if !strings.HasPrefix(line, want) {
			return Question{}, MalformedBlockError{Reason: strconv.Quote(line) + " is not the expected " + strconv.Quote(want) + " line"}
		}
		values[next] = strings.TrimPrefix(strings.TrimPrefix(line, want), " ")
		next++
	}
	if next != len(questionFieldOrder) {
		return Question{}, MalformedBlockError{Reason: "the block has no " + questionFieldOrder[next] + " line"}
	}
	return Question{
		Ticket:  named[1],
		Kind:    kind,
		Where:   values[0],
		Ask:     values[1],
		Default: values[2],
		Answer:  values[3],
	}, nil
}
