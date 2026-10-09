package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"tofu/interface/cli"
	"tofu/internal/host"
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
	turns map[string]bool
	since time.Time
}

type whyReport struct {
	Call *recordedCall `json:"call,omitempty"`
	Rows []whyListing  `json:"rows"`
}

type whyListing struct {
	ledger.Row
	Chain      *ledger.Row            `json:"chain,omitempty"`
	BlockedBy  string                 `json:"blocked_by,omitempty"`
	Precedents []host.LedgerPrecedent `json:"precedents"`
	state      json.RawMessage
	stateErr   error
	statePath  string
}

func (l whyListing) decision() ledger.Row {
	if l.Chain != nil {
		return *l.Chain
	}
	return l.Row
}

func whyVerb(args []string, out, errOut io.Writer, now func() time.Time) int {
	o := verbOutput{verb: "why", usageLine: whyUsage, asJSON: jsonAsked(args), out: out, errOut: errOut}
	opts, err := parseWhyArgs(args)
	if err != nil {
		return o.usage(err)
	}
	found, err := whyFind(opts)
	switch {
	case err != nil, len(found.rows) > 0, found.call != nil:
	case opts.point != "":
		err = errors.New("the ledger has no rows at " + opts.point)
	default:
		err = errors.New("the ledger has no rows")
	}
	if err != nil {
		return o.fail(err)
	}
	if opts.state {
		for _, row := range found.rows {
			body, err := found.reader.State(row)
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
	report, err := found.report()
	if err != nil {
		return o.fail(err)
	}
	return o.done(true, report, func(page cli.Page) []string { return whyPage(page, report, now()) })
}

type whyFound struct {
	dir    string
	reader *ledger.Reader
	call   *recordedCall
	rows   []ledger.Row
}

func whyFind(opts whyOpts) (whyFound, error) {
	dir, err := sys.LogDir()
	if err != nil {
		return whyFound{}, err
	}
	found := whyFound{dir: dir, reader: ledger.NewReader(dir)}
	found.rows, err = whyRows(found.reader, opts)
	if err == nil {
		return found, nil
	}
	called, lookedUp, lookupErr := recordedCallByHash(opts.id)
	if lookupErr != nil || !lookedUp {
		return found, err
	}
	found.call, found.rows = &called, nil
	if opts.id = called.Call.GateDecisionID; opts.id == "" {
		return found, nil
	}
	found.rows, err = whyRows(found.reader, opts)
	return found, err
}

func (f whyFound) report() (whyReport, error) {
	report := whyReport{Call: f.call, Rows: []whyListing{}}
	decisions := make([]ledger.Row, len(f.rows))
	for i, row := range f.rows {
		listing, err := whyListingOf(f.reader, f.dir, row)
		if err != nil {
			return report, err
		}
		report.Rows, decisions[i] = append(report.Rows, listing), listing.decision()
	}
	shortlists, err := f.reader.Precedents(decisions)
	if err != nil {
		return report, err
	}
	for i, found := range shortlists {
		for _, precedent := range found {
			report.Rows[i].Precedents = append(report.Rows[i].Precedents, host.LedgerPrecedent{ID: precedent.Row.ID, Verdict: precedent.Row.Verdict, At: precedent.Row.At,
				Distance: precedent.Distance, SameFingerprint: precedent.SameFingerprint, Comparable: precedent.Comparable, Why: precedentWhy(precedent), Outcome: precedent.Row.Outcome})
		}
	}
	return report, nil
}

func whyListingOf(reader *ledger.Reader, dir string, row ledger.Row) (whyListing, error) {
	listing := whyListing{Row: row, Precedents: []host.LedgerPrecedent{}}
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
	if _, err := reader.Each(ledger.Filter{Point: opts.point, Turns: opts.turns, Since: opts.since}, func(row ledger.Row) error {
		window = append(window, row)
		if len(window) > opts.count {
			window = window[1:]
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return window, nil
}
