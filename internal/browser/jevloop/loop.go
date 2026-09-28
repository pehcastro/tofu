package jevloop

import (
	"context"
	"fmt"
	"reflect"

	"tofu/internal/browser"
	"tofu/internal/konst"
)

type Browser struct {
	Snapshot func(ctx context.Context) (browser.Page, error)
	Act      func(ctx context.Context, page browser.Page, action browser.Action) (browser.Stale, error)
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

func (s Status) String() string {
	switch s {
	case StatusDone:
		return "done"
	case StatusBlocked:
		return "blocked"
	}
	panic(fmt.Sprintf("jevloop: unknown status %d", int(s)))
}

type Step struct {
	Choice
	Label   string
	Changed bool
	Stale   browser.Stale
}

type Result struct {
	Status    Status
	Reason    string
	Steps     []Step
	Decisions int
	Page      browser.Page
}

func (l Loop) Run(ctx context.Context, goal string) Result {
	result := Result{Status: StatusBlocked}
	stop := func(format string, args ...any) Result {
		result.Reason = fmt.Sprintf(format, args...)
		return result
	}
	budget := min(l.Actions, konst.BrowserActionCeiling)
	decisions := budget * konst.BrowserDecisionsPerAction
	var err error
	if result.Page, err = l.Browser.Snapshot(ctx); err != nil {
		return stop("no snapshot: %v", err)
	}
	acted, unchanged, refused, refusedOn := 0, 0, 0, 0
	for {
		if result.Decisions >= decisions {
			return stop("the decision budget of %d ran out", decisions)
		}
		page := result.Page
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
		switch {
		case op == browser.OpBlocked:
			return stop("the chooser chose BLOCKED")
		case op == browser.OpDone:
			next, err := l.Browser.Snapshot(ctx)
			if err != nil {
				return stop("no snapshot to confirm DONE: %v", err)
			}
			result.Page = next
			if page.URL == next.URL && reflect.DeepEqual(page.Elements, next.Elements) {
				result.Status = StatusDone
				return stop("the chooser chose DONE")
			}
			continue
		case acted >= budget:
			return stop("the action budget of %d ran out", budget)
		case op == browser.OpTypeText:
			text, err := l.Write(ctx, goal, page, element)
			if err != nil {
				return stop("nothing typed into %q: %v", label, err)
			}
			choice.Action.Value = text
		}
		stale, err := l.Browser.Act(ctx, page, choice.Action)
		if err != nil {
			return stop("%s on %q failed: %v", op, label, err)
		}
		next, err := l.Browser.Snapshot(ctx)
		if err != nil {
			return stop("no snapshot after %s on %q: %v", op, label, err)
		}
		result.Page = next
		step := Step{Choice: choice, Label: label, Changed: next.Fingerprint != page.Fingerprint, Stale: stale}
		result.Steps = append(result.Steps, step)
		if stale != browser.StaleNone {
			if refusedOn != choice.Action.Element {
				refused, refusedOn = 0, choice.Action.Element
			}
			if refused++; refused == konst.BrowserStaleStop {
				return stop("%s on %q did not run %d times in a row: %s", op, label, refused, stale)
			}
			continue
		}
		refused = 0
		acted++
		unchanged++
		if step.Changed || op == browser.OpWait {
			unchanged = 0
		}
		if unchanged == konst.BrowserNoChangeStop {
			return stop("%d actions in a row changed nothing", unchanged)
		}
	}
}
