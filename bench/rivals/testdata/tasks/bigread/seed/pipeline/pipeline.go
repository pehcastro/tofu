package pipeline

import (
	"sync"

	"example.com/atlas/dispatch"
	"example.com/atlas/enrich"
	"example.com/atlas/intake"
	"example.com/atlas/route"
	"example.com/atlas/sink"
	"example.com/atlas/validate"
)

type Stage interface {
	Run(o *intake.Order) error
}

type Pipeline struct {
	stages []Stage
}

func New(stages ...Stage) *Pipeline {
	return &Pipeline{stages: stages}
}

func (p *Pipeline) Submit(o *intake.Order) error {
	for _, stage := range p.stages {
		if err := stage.Run(o); err != nil {
			return err
		}
	}
	return nil
}

var (
	defaultOnce sync.Once
	defaultLine *Pipeline
	quotesOnce  sync.Once
	quotesLine  *Pipeline
)

func Default() *Pipeline {
	defaultOnce.Do(func() {
		defaultLine = New(validate.Stage{}, enrich.New(), dispatch.NewStage(route.ByClass(route.Defaults()), sink.Shared()))
	})
	return defaultLine
}

func Quotes() *Pipeline {
	quotesOnce.Do(func() {
		quotesLine = New(validate.Stage{}, dispatch.NewStage(route.Legacy(), sink.Shared()))
	})
	return quotesLine
}
