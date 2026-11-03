package dispatch

import (
	"fmt"

	"example.com/atlas/intake"
)

type Picker interface {
	Pick(o intake.Order) string
}

type Sink interface {
	Write(queue string, o intake.Order) error
}

type Stage struct {
	picker Picker
	sink   Sink
}

func NewStage(p Picker, s Sink) Stage {
	return Stage{picker: p, sink: s}
}

func (s Stage) Run(o *intake.Order) error {
	queue := s.picker.Pick(*o)
	if queue == "" {
		queue = deadLetter
	}
	if err := s.sink.Write(queue, *o); err != nil {
		return fmt.Errorf("dispatch %s to %s: %w", o.ID, queue, err)
	}
	return nil
}
