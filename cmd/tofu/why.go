package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"tofu/interface/cli"
	"tofu/internal/judge/ledger"
	"tofu/internal/sys"
)

const whyUsage = "tofu why <id> | --last [n] [--point name] [--state | --json]"

type whyOpts struct {
	id    string
	last  bool
	count int
	point string
	json  bool
	state bool
}

type whyReport struct {
	Call *recordedCall `json:"call,omitempty"`
	Rows []whyListing  `json:"rows"`
}

type whyListing struct {
	ledger.Row
	Chain      *ledger.Row    `json:"chain,omitempty"`
	BlockedBy  string         `json:"blocked_by,omitempty"`
	Precedents []whyPrecedent `json:"precedents"`
	state      json.RawMessage
	stateErr   error
	statePath  string
}

type whyPrecedent struct {
	ID              string          `json:"row_id"`
	Verdict         ledger.Verdict  `json:"verdict"`
	At              time.Time       `json:"at"`
	Distance        float64         `json:"distance"`
	SameFingerprint bool            `json:"same_fingerprint"`
	Comparable      bool            `json:"comparable"`
	Why             string          `json:"why"`
	Outcome         *ledger.Outcome `json:"outcome,omitempty"`
}

func (l whyListing) decision() ledger.Row {
	if l.Chain != nil {
		return *l.Chain
	}
	return l.Row
}

func whyVerb(args []string, out, errOut io.Writer, now func() time.Time) int {
	o := verbOutput{verb: "why", usageLine: whyUsage, out: out, errOut: errOut}
	opts, err := parseWhyArgs(args)
	if err != nil {
		return o.usage(err)
	}
	o.asJSON = opts.json
	dir, err := sys.LogDir()
	if err != nil {
		return o.fail(err)
	}
	reader := ledger.NewReader(dir)
	report := whyReport{Rows: []whyListing{}}
	rows, err := whyRows(reader, opts)
	if err != nil {
		found, lookedUp, lookupErr := recordedCallByHash(opts.id)
		if lookupErr != nil || !lookedUp {
			return o.fail(err)
		}
		report.Call, rows = &found, nil
		if opts.id = found.Call.GateDecisionID; opts.id != "" {
			if rows, err = whyRows(reader, opts); err != nil {
				return o.fail(err)
			}
		}
	}
	if opts.state {
		for _, row := range rows {
			body, err := reader.State(row)
			if err == nil && len(body) == 0 {
				err = fmt.Errorf("row %s carries no state body", row.ID)
			}
			if err != nil {
				return o.fail(err)
			}
			_, _ = fmt.Fprintln(out, string(body))
		}
		return exitOK
	}
	for _, row := range rows {
		listing, err := whyListingOf(reader, dir, row)
		if err != nil {
			return o.fail(err)
		}
		report.Rows = append(report.Rows, listing)
	}
	return o.done(true, report, func(page cli.Page) []string { return whyPage(page, report, now()) })
}

func whyListingOf(reader *ledger.Reader, dir string, row ledger.Row) (whyListing, error) {
	listing := whyListing{Row: row, Precedents: []whyPrecedent{}}
	if row.ReplayOf != "" {
		original, ok, err := reader.ByID(row.ReplayOf)
		if err != nil {
			return whyListing{}, err
		}
		if !ok {
			return whyListing{}, problemError{What: "no row " + strconv.Quote(row.ReplayOf) + " in the ledger, which " + row.ID + " replays"}
		}
		listing.Chain = &original
	}
	decision := listing.decision()
	listing.BlockedBy = blockingQuestion(decision)
	if e := decision.StateElision; e != nil {
		listing.statePath = filepath.Join(dir, e.File)
	}
	listing.state, listing.stateErr = reader.State(decision)
	found, err := reader.Precedents(decision)
	if err != nil {
		return whyListing{}, err
	}
	for _, precedent := range found {
		listing.Precedents = append(listing.Precedents, whyPrecedent{ID: precedent.Row.ID, Verdict: precedent.Row.Verdict, At: precedent.Row.At,
			Distance: precedent.Distance, SameFingerprint: precedent.SameFingerprint, Comparable: precedent.Comparable, Why: precedentWhy(precedent), Outcome: precedent.Row.Outcome})
	}
	return listing, nil
}

func parseWhyArgs(args []string) (whyOpts, error) {
	var opts whyOpts
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--last":
			opts.last, opts.count = true, max(opts.count, 1)
			if i+1 == len(args) {
				continue
			}
			if n, err := strconv.Atoi(args[i+1]); err == nil {
				if n < 1 {
					return whyOpts{}, fmt.Errorf("--last needs a positive count, got %d", n)
				}
				opts.count = n
				i++
			}
		case arg == "--point":
			i++
			if i >= len(args) {
				return whyOpts{}, errors.New("--point needs a name")
			}
			opts.point = args[i]
		case arg == jsonFlag:
			opts.json = true
		case arg == "--state":
			opts.state = true
		case strings.HasPrefix(arg, "-"):
			return whyOpts{}, fmt.Errorf("unknown argument %q", arg)
		default:
			if opts.id != "" {
				return whyOpts{}, fmt.Errorf("one id at a time, got %q and %q", opts.id, arg)
			}
			opts.id = arg
		}
	}
	switch {
	case opts.id == "" && !opts.last:
		return whyOpts{}, errors.New("an id or --last is needed")
	case opts.id != "" && opts.last:
		return whyOpts{}, errors.New("an id or --last, not both")
	case opts.point != "" && !opts.last:
		return whyOpts{}, errors.New("--point goes with --last")
	case opts.state && opts.json:
		return whyOpts{}, errors.New("--state or --json, not both")
	}
	return opts, nil
}

func blockingQuestion(row ledger.Row) string {
	if row.Reason == nil || !row.Reason.Blocked || row.Policy == "" {
		return ""
	}
	pol, _, err := loadRulePoint(fmt.Sprintf("%s@%d", row.Policy, row.PolicyVersion), "")
	if err != nil {
		return ""
	}
	return pol.FromUntrustedQuestion
}

func whyRows(reader *ledger.Reader, opts whyOpts) ([]ledger.Row, error) {
	if opts.id != "" {
		row, ok, err := reader.ByID(opts.id)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, problemError{What: "no row " + strconv.Quote(opts.id) + " in the ledger", Hint: "tofu why --last 5"}
		}
		return []ledger.Row{row}, nil
	}
	var window []ledger.Row
	if _, err := reader.Each(ledger.Filter{Point: opts.point}, func(row ledger.Row) error {
		window = append(window, row)
		if len(window) > opts.count {
			window = window[1:]
		}
		return nil
	}); err != nil {
		return nil, err
	}
	if len(window) == 0 && opts.point != "" {
		return nil, errors.New("the ledger has no rows at " + opts.point)
	}
	if len(window) == 0 {
		return nil, errors.New("the ledger has no rows")
	}
	return window, nil
}

func isTerminalWriter(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}
