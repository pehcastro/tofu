package ask

import "tofu/internal/subagent"

type Label string

const (
	LabelAsked   Label = "asked"
	LabelWidened Label = "widened"
)

type Moment struct {
	Ticket string
	Cite   string
	Label  Label
	Ideal  string
	State  subagent.AskState
}

func Corpus() []Moment {
	return []Moment{
		{
			Ticket: "BOJI-006", Cite: "BOJI-006:185", Label: LabelAsked, Ideal: subagent.ActionDefer,
			State: subagent.AskState{
				Path: "cmd/boji/main.go", InOwns: false, Answered: false, Precedent: false,
				Reversibility: "a Log note naming the one dispatch line to add, cheap to correct",
			},
		},
		{
			Ticket: "BOJI-011", Cite: "BOJI-011:257", Label: LabelAsked, Ideal: subagent.ActionDefer,
			State: subagent.AskState{
				Path: "bench/cost/labels.go", InOwns: true, Answered: false, Precedent: false,
				Reversibility: "a Log note naming the file to change if the reading is wrong, cheap to correct",
			},
		},
		{
			Ticket: "BOJI-015", Cite: "BOJI-015:383", Label: LabelAsked, Ideal: subagent.ActionDefer,
			State: subagent.AskState{
				Path: "internal/konst/konst.go", InOwns: true, Answered: false, Precedent: false,
				Reversibility: "a Log note naming both numbers, cheap to correct",
			},
		},
		{
			Ticket: "BOJI-020", Cite: "BOJI-020:234", Label: LabelAsked, Ideal: subagent.ActionDefer,
			State: subagent.AskState{
				Path: "internal/judge/question/load.go", InOwns: false, Answered: false, Precedent: false,
				Reversibility: "a Log note naming the file and asking whether it is now or its own ticket, cheap to correct",
			},
		},
		{
			Ticket: "BOJI-006", Cite: "BOJI-006:430", Label: LabelWidened, Ideal: subagent.ActionAskNow,
			State: subagent.AskState{
				Path: "cmd/boji/bench.go", InOwns: false, Answered: false, Precedent: false,
				Reversibility: "a file outside owns granted by a brief the ticket did not say, quiet until the orchestrator notices",
			},
		},
		{
			Ticket: "BOJI-011", Cite: "BOJI-011:291", Label: LabelWidened, Ideal: subagent.ActionAskNow,
			State: subagent.AskState{
				Path: "cmd/boji/bench.go", InOwns: false, Answered: false, Precedent: false,
				Reversibility: "a file outside owns granted by a brief the ticket did not say, quiet until the orchestrator notices",
			},
		},
		{
			Ticket: "BOJI-015", Cite: "BOJI-015:142", Label: LabelWidened, Ideal: subagent.ActionAskNow,
			State: subagent.AskState{
				Path: "internal/judge/jev/client_test.go", InOwns: false, Answered: false, Precedent: false,
				Reversibility: "a file outside owns broken by deleting a konst field, quiet until the orchestrator notices",
			},
		},
	}
}
