package jevloop

import (
	"context"
	"fmt"
	"reflect"
	"slices"

	"tofu/internal/browser"
	"tofu/internal/konst"
)

type Browser struct {
	Snapshot func(ctx context.Context) (browser.Page, error)
	Act      func(ctx context.Context, page browser.Page, action browser.Action) (browser.Stale, error)
	Tab      func() int
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
	Opened  int
}

type Result struct {
	Status    Status
	Reason    string
	Steps     []Step
	Decisions int
	Page      browser.Page
	Pages     []browser.Page
}

func empty(page browser.Page) bool {
	return page.URL == "about:blank" || len(page.Elements) == 0 && page.Text == ""
}

func (l Loop) tab() int {
	if l.Browser.Tab == nil {
		return 0
	}
	return l.Browser.Tab()
}

func (l Loop) Run(ctx context.Context, goal string) Result {
	result := Result{Status: StatusBlocked}
	stop := func(format string, args ...any) Result {
		result.Reason = fmt.Sprintf(format, args...)
		return result
	}
	see := func() error {
		page, err := l.Browser.Snapshot(ctx)
		if err != nil {
			return err
		}
		result.Page = page
		if !empty(page) && !slices.ContainsFunc(result.Pages, func(seen browser.Page) bool { return seen.URL == page.URL && seen.Text == page.Text }) {
			result.Pages = append(result.Pages, page)
			result.Pages = result.Pages[max(0, len(result.Pages)-konst.BrowserRecentSteps):]
		}
		return nil
	}
	budget := min(l.Actions, konst.BrowserActionCeiling)
	decisions := budget * konst.BrowserDecisionsPerAction
	if err := see(); err != nil {
		return stop("no snapshot: %v", err)
	}
	acted, unchanged, refused, refusedOn, waited := 0, 0, 0, 0, 0
	for {
		if empty(result.Page) {
			if waited == konst.BrowserNoChangeStop {
				return stop("the page stayed empty after %d waits", waited)
			}
			waited++
			if _, err := l.Browser.Act(ctx, result.Page, browser.Action{Op: browser.OpWait}); err != nil {
				return stop("no wait on the empty page: %v", err)
			}
			if err := see(); err != nil {
				return stop("no snapshot after a wait on the empty page: %v", err)
			}
			continue
		}
		waited = 0
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
			if err := see(); err != nil {
				return stop("no snapshot to confirm DONE: %v", err)
			}
			if page.URL == result.Page.URL && reflect.DeepEqual(page.Elements, result.Page.Elements) {
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
		from := l.tab()
		var stale browser.Stale
		for retried := false; ; retried = true {
			if stale, err = l.Browser.Act(ctx, page, choice.Action); err != nil {
				return stop("%s on %q failed: %v", op, label, err)
			}
			if err := see(); err != nil {
				return stop("no snapshot after %s on %q: %v", op, label, err)
			}
			same := slices.DeleteFunc(slices.Clone(result.Page.Elements), func(e browser.Element) bool { return e.Role != element.Role || e.Label != element.Label })
			if retried || stale != browser.StaleChanged || len(same) != 1 {
				break
			}
			page, choice.Action.Element = result.Page, same[0].Index
		}
		step := Step{Choice: choice, Label: label, Changed: result.Page.Fingerprint != page.Fingerprint, Stale: stale}
		if to := l.tab(); to != from {
			step.Opened, step.Changed = to, true
		}
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
