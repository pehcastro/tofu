package main

import (
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"time"

	"tofu/interface/cli"
	"tofu/internal/judge/ledger"
	"tofu/internal/sys"
)

const labelUsage = "tofu label <id> allow|ask|deny | tofu label --last allow|ask|deny [--json]"

const outcomeKindHandLabeled = "hand-labeled"

type labelReceipt struct {
	ID      string         `json:"id"`
	Outcome ledger.Verdict `json:"outcome"`
	Kind    string         `json:"kind"`
	Verdict ledger.Verdict `json:"verdict"`
}

func labelVerb(args []string, out, errOut io.Writer, now func() time.Time) int {
	o := verbOutput{verb: "label", usageLine: labelUsage, asJSON: slices.Contains(args, jsonFlag), out: out, errOut: errOut}
	args = slices.DeleteFunc(slices.Clone(args), func(arg string) bool { return arg == jsonFlag })
	last := slices.Contains(args, "--last")
	args = slices.DeleteFunc(args, func(arg string) bool { return arg == "--last" })
	operands := 2
	if last {
		operands = 1
	}
	if len(args) != operands {
		return o.usage(errors.New("an id and an outcome, or --last and an outcome"))
	}
	outcome := ledger.Verdict(args[len(args)-1])
	if outcome != ledger.VerdictAllow && outcome != ledger.VerdictAsk && outcome != ledger.VerdictDeny {
		return o.usage(fmt.Errorf("%q is not an outcome, want allow, ask or deny", string(outcome)))
	}
	dir, err := sys.LogDir()
	if err != nil {
		return o.fail(err)
	}
	reader := ledger.NewReader(dir)
	id := args[0]
	if last {
		id = ""
		var newest time.Time
		if _, err := reader.Each(ledger.Filter{}, func(row ledger.Row) error {
			if id == "" || row.At.After(newest) {
				id, newest = row.ID, row.At
			}
			return nil
		}); err != nil {
			return o.fail(err)
		}
		if id == "" {
			return o.fail(errors.New("the ledger has no rows"))
		}
	}
	row, ok, err := reader.ByID(id)
	switch {
	case err != nil:
		return o.fail(err)
	case !ok:
		return o.fail(problemError{What: "no row " + strconv.Quote(id) + " in the ledger", Hint: "tofu why --last 5"})
	case row.Outcome != nil:
		return o.fail(problemError{What: id + " already carries the outcome " + row.Outcome.Detail})
	}
	receipt := labelReceipt{ID: id, Outcome: outcome, Kind: outcomeKindHandLabeled, Verdict: row.Verdict}
	if err := ledger.NewWriterWithClock(dir, now).Backfill(id, ledger.Outcome{Kind: receipt.Kind, Detail: string(outcome)}); err != nil {
		return o.fail(err)
	}
	return o.done(true, receipt, func(page cli.Page) []string {
		return []string{page.Glyph(cli.Done) + " " + id + " labeled " + verdictLook(outcome).Text + cli.Gap + page.Label("verdict was "+verdictLook(row.Verdict).Text)}
	})
}
