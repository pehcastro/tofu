package subagent

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
	panic("subagent: unknown question kind " + strconv.Itoa(int(k)))
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
