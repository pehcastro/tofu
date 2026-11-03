package judge

import (
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"time"

	"tofu/apps/cli/output"
	"tofu/apps/cli/page"
	"tofu/apps/cli/wiring"
	"tofu/internal/judge/ledger"
	"tofu/internal/sys"
)

const LabelUsage = "tofu label <id> allow|ask|deny | tofu label --last allow|ask|deny [--json]"

const OutcomeKindHandLabeled = "hand-labeled"

type labelReceipt struct {
	ID      string         `json:"id"`
	Outcome ledger.Verdict `json:"outcome"`
	Kind    string         `json:"kind"`
	Verdict ledger.Verdict `json:"verdict"`
}

func LabelVerb(args []string, out, errOut io.Writer, now func() time.Time) int {
	o := output.Verb{Name: "label", UsageLine: LabelUsage, AsJSON: output.JSONAsked(args), Out: out, ErrOut: errOut}
	args = output.WithoutJSON(args)
	last := slices.Contains(args, "--last")
	args = slices.DeleteFunc(args, func(arg string) bool { return arg == "--last" })
	operands := 2
	if last {
		operands = 1
	}
	if len(args) != operands {
		return o.Usage(errors.New("an id and an outcome, or --last and an outcome"))
	}
	outcome := ledger.Verdict(args[len(args)-1])
	if outcome != ledger.VerdictAllow && outcome != ledger.VerdictAsk && outcome != ledger.VerdictDeny {
		return o.Usage(fmt.Errorf("%q is not an outcome, want allow, ask or deny", string(outcome)))
	}
	dir, err := sys.LogDir()
	if err != nil {
		return o.Fail(err)
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
			return o.Fail(err)
		}
		if id == "" {
			return o.Fail(errors.New("the ledger has no rows"))
		}
	}
	row, ok, err := reader.ByID(id)
	switch {
	case err != nil:
		return o.Fail(err)
	case !ok:
		return o.Fail(output.ProblemError{What: "no row " + strconv.Quote(id) + " in the ledger", Hint: "tofu why --last 5"})
	case row.Outcome != nil:
		return o.Fail(output.ProblemError{What: id + " already carries the outcome " + row.Outcome.Detail})
	}
	receipt := labelReceipt{ID: id, Outcome: outcome, Kind: OutcomeKindHandLabeled, Verdict: row.Verdict}
	if err := ledger.NewWriterWithClock(dir, now).Backfill(id, ledger.Outcome{Kind: receipt.Kind, Detail: string(outcome)}); err != nil {
		return o.Fail(err)
	}
	return o.Done(true, receipt, func(page cli.Page) []string {
		return []string{page.Glyph(cli.Done) + " " + id + " labeled " + wiring.VerdictLook(outcome).Text + cli.Gap + page.Label("verdict was "+wiring.VerdictLook(row.Verdict).Text)}
	})
}
