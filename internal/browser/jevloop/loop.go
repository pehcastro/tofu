package jevloop

import (
	"context"
	"fmt"

	"tofu/internal/browser"
	"tofu/internal/konst"
)

type Browser struct {
	Snapshot func(ctx context.Context) (browser.Page, error)
	Fresh    func(ctx context.Context, page browser.Page) (bool, error)
	Act      func(ctx context.Context, page browser.Page, action browser.Action) (fresh bool, err error)
}

type Writer func(ctx context.Context, goal string, page browser.Page, field browser.Element) (string, error)

type Loop struct {
	Browser Browser
	Choose  Chooser
	Write   Writer
	Actions int
}

type Status int

const (
	StatusDone Status = iota
	StatusBlocked
)

type Step struct {
	Choice
	Label   string
	Changed bool
	Stale   bool
}

type Result struct {
	Status    Status
	Reason    string
	Steps     []Step
	Decisions int
}

func (l Loop) Run(ctx context.Context, goal string) Result {
	result := Result{Status: StatusBlocked}
	stop := func(format string, args ...any) Result {
		result.Reason = fmt.Sprintf(format, args...)
		return result
	}
	budget := min(l.Actions, konst.BrowserActionCeiling)
	decisions := budget * konst.BrowserDecisionsPerAction
	page, err := l.Browser.Snapshot(ctx)
	if err != nil {
		return stop("no snapshot: %v", err)
	}
	acted, unchanged, retried := 0, 0, false
	for {
		if result.Decisions >= decisions {
			return stop("the decision budget of %d ran out", decisions)
		}
		choice, err := l.Choose(ctx, goal, page, result.Steps)
		result.Decisions++
		if err != nil {
			return stop("no action ran, the chooser failed: %v", err)
		}
		op := choice.Action.Op
		label := op.String()
		element, targeted := page.Element(choice.Action.Element)
		if targeted {
			label = element.Label
		}
		fresh, err := l.Browser.Fresh(ctx, page)
		if err != nil {
			return stop("no freshness check: %v", err)
		}
		if fresh {
			switch {
			case op == browser.OpDone:
				result.Status = StatusDone
				return stop("the chooser chose DONE")
			case op == browser.OpBlocked:
				return stop("the chooser chose BLOCKED")
			case acted >= budget:
				return stop("the action budget of %d ran out", budget)
			case op == browser.OpTypeText:
				text, err := l.Write(ctx, goal, page, element)
				if err != nil {
					return stop("nothing typed into %q: %v", label, err)
				}
				choice.Action.Value = text
			}
			fresh, err = l.Browser.Act(ctx, page, choice.Action)
			if err != nil {
				return stop("%s on %q failed: %v", op, label, err)
			}
		}
		next, err := l.Browser.Snapshot(ctx)
		if err != nil {
			return stop("no snapshot after %s on %q: %v", op, label, err)
		}
		step := Step{Choice: choice, Label: label, Changed: next.Fingerprint != page.Fingerprint, Stale: !fresh}
		page = next
		if step.Stale && !retried {
			retried = true
			continue
		}
		retried = false
		result.Steps = append(result.Steps, step)
		if !step.Stale {
			acted++
		}
		unchanged++
		if step.Stale || step.Changed || op == browser.OpWait {
			unchanged = 0
		}
		if unchanged == konst.BrowserNoChangeStop {
			return stop("%d actions in a row changed nothing", unchanged)
		}
	}
}
