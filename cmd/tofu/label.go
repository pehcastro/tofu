package main

import (
	"errors"
	"fmt"
	"io"
	"time"

	"tofu/internal/judge/ledger"
)

type outcome string

const (
	outcomeAllow outcome = "allow"
	outcomeAsk   outcome = "ask"
	outcomeDeny  outcome = "deny"
)

const outcomeKindHandLabeled = "hand-labeled"

func parseOutcome(s string) (outcome, error) {
	switch outcome(s) {
	case outcomeAllow, outcomeAsk, outcomeDeny:
		return outcome(s), nil
	}
	return "", fmt.Errorf("%q is not an outcome, want allow, ask or deny", s)
}

type labelOpts struct {
	id      string
	last    bool
	outcome outcome
}

func labelVerb(args []string, out, errOut io.Writer, now func() time.Time) int {
	opts, err := parseLabelArgs(args)
	if err != nil {
		return labelFail(errOut, err)
	}
	dir, err := ledger.Dir()
	if err != nil {
		return labelFail(errOut, err)
	}
	reader := ledger.NewReader(dir)

	id := opts.id
	if opts.last {
		var found ledger.Row
		var ok bool
		if _, err := reader.Each(ledger.Filter{}, func(row ledger.Row) error {
			if !ok || row.At.After(found.At) {
				found, ok = row, true
			}
			return nil
		}); err != nil {
			return labelFail(errOut, err)
		}
		if !ok {
			return labelFail(errOut, errors.New("the ledger has no rows"))
		}
		id = found.ID
	}

	row, ok, err := reader.ByID(id)
	if err != nil {
		return labelFail(errOut, err)
	}
	if !ok {
		return labelFail(errOut, fmt.Errorf("no row %q in the ledger at %s", id, dir))
	}
	if row.Outcome != nil {
		return labelFail(errOut, fmt.Errorf("row %q already carries the outcome %q", id, row.Outcome.Detail))
	}

	writer := ledger.NewWriterWithClock(dir, now)
	if err := writer.Backfill(id, ledger.Outcome{Kind: outcomeKindHandLabeled, Detail: string(opts.outcome)}); err != nil {
		return labelFail(errOut, err)
	}
	_, _ = fmt.Fprintf(out, "%s  %s\n", id, opts.outcome)
	return exitOK
}

func labelFail(errOut io.Writer, err error) int {
	_, _ = fmt.Fprintf(errOut, "tofu label: %v\n", err)
	return exitUsage
}

func parseLabelArgs(args []string) (labelOpts, error) {
	var positional []string
	last := false
	for _, arg := range args {
		if arg == "--last" {
			last = true
			continue
		}
		positional = append(positional, arg)
	}
	if last {
		if len(positional) != 1 {
			return labelOpts{}, errors.New("tofu label --last needs one outcome")
		}
		out, err := parseOutcome(positional[0])
		if err != nil {
			return labelOpts{}, err
		}
		return labelOpts{last: true, outcome: out}, nil
	}
	if len(positional) != 2 {
		return labelOpts{}, errors.New("tofu label needs an id and an outcome")
	}
	out, err := parseOutcome(positional[1])
	if err != nil {
		return labelOpts{}, err
	}
	return labelOpts{id: positional[0], outcome: out}, nil
}
