package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"tofu/internal/judge/gate"
	"tofu/internal/judge/ledger"
	"tofu/internal/sys"
)

type whyOpts struct {
	id    string
	last  bool
	count int
	point string
	json  bool
	state bool
}

type notFoundError struct {
	id  string
	dir string
}

func (e notFoundError) Error() string {
	return fmt.Sprintf("no row %q in the ledger at %s", e.id, e.dir)
}

type whyRow struct {
	queried   ledger.Row
	chain     ledger.Row
	isReplay  bool
	blockedBy string
	statePath string
}

func whyVerb(args []string, out, errOut io.Writer, now func() time.Time) int {
	opts, err := parseWhyArgs(args)
	if err != nil {
		return whyFail(errOut, err)
	}
	dir, err := sys.LogDir()
	if err != nil {
		return whyFail(errOut, err)
	}
	reader := ledger.NewReader(dir)

	rows, err := whyRows(reader, dir, opts)
	if err != nil {
		found, lookedUp, lookupErr := recordedCallByHash(opts.id)
		if lookupErr != nil || !lookedUp {
			return whyFail(errOut, err)
		}
		printRecordedCall(out, found)
		if found.call.GateDecisionID == "" {
			return exitOK
		}
		_, _ = fmt.Fprintln(out)
		opts.id = found.call.GateDecisionID
		if rows, err = whyRows(reader, dir, opts); err != nil {
			return whyFail(errOut, err)
		}
	}

	if opts.state {
		for _, row := range rows {
			body, err := reader.State(row)
			if err != nil {
				return whyFail(errOut, err)
			}
			if len(body) == 0 {
				return whyFail(errOut, fmt.Errorf("row %s carries no state body", row.ID))
			}
			_, _ = fmt.Fprintln(out, string(body))
		}
		return exitOK
	}

	color := isTerminalWriter(out)
	moment := now()
	for i, row := range rows {
		wr, err := loadChain(reader, row, dir)
		if err != nil {
			return whyFail(errOut, err)
		}
		wr.blockedBy = blockingQuestion(wr.chain)
		if e := wr.chain.StateElision; e != nil {
			wr.statePath = filepath.Join(dir, e.File)
		}
		precedents, err := reader.Precedents(wr.chain)
		if err != nil {
			return whyFail(errOut, err)
		}
		if i > 0 {
			_, _ = fmt.Fprintln(out)
		}
		if opts.json {
			if err := printWhyJSON(out, wr, moment, precedents); err != nil {
				return whyFail(errOut, err)
			}
			continue
		}
		printWhy(out, wr, moment, color, precedents)
	}
	return exitOK
}

func whyFail(errOut io.Writer, err error) int {
	_, _ = fmt.Fprintf(errOut, "tofu why: %v\n", err)
	return exitUsage
}

func peekCount(args []string, i int) (n int, consumed bool, err error) {
	if i+1 >= len(args) {
		return 0, false, nil
	}
	n, convErr := strconv.Atoi(args[i+1])
	if convErr != nil {
		return 0, false, nil
	}
	if n < 1 {
		return 0, false, fmt.Errorf("--last needs a positive count, got %d", n)
	}
	return n, true, nil
}

func parseWhyArgs(args []string) (whyOpts, error) {
	var opts whyOpts
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--last":
			opts.last = true
			n, consumed, countErr := peekCount(args, i)
			if countErr != nil {
				return whyOpts{}, countErr
			}
			if consumed {
				opts.count = n
				i++
				continue
			}
			if opts.count == 0 {
				opts.count = 1
			}
		case arg == "--point":
			i++
			if i >= len(args) {
				return whyOpts{}, errors.New("--point needs a name")
			}
			opts.point = args[i]
		case arg == "--json":
			opts.json = true
		case arg == "--state":
			opts.state = true
		case strings.HasPrefix(arg, "-"):
			return whyOpts{}, fmt.Errorf("unknown argument %q", arg)
		default:
			if opts.id != "" {
				return whyOpts{}, fmt.Errorf("tofu why takes one id, got %q and %q", opts.id, arg)
			}
			opts.id = arg
		}
	}
	if opts.id == "" && !opts.last {
		return whyOpts{}, errors.New("tofu why needs an id or --last")
	}
	if opts.id != "" && opts.last {
		return whyOpts{}, errors.New("tofu why takes an id or --last, not both")
	}
	if opts.point != "" && !opts.last {
		return whyOpts{}, errors.New("--point only makes sense with --last")
	}
	if opts.state && opts.json {
		return whyOpts{}, errors.New("--state prints the state body alone, --json prints the row, not both")
	}
	return opts, nil
}

func blockingQuestion(row ledger.Row) string {
	if row.Reason == nil || !row.Reason.Blocked || row.Policy == "" {
		return ""
	}
	catalog, err := sys.CatalogDir()
	if err != nil {
		return ""
	}
	found, err := gate.FindRule(os.DirFS(catalog), fmt.Sprintf("%s@%d", row.Policy, row.PolicyVersion))
	if err != nil || found == "" {
		return ""
	}
	pol, err := gate.Load(filepath.Join(catalog, filepath.FromSlash(found)))
	if err != nil {
		return ""
	}
	return pol.FromUntrustedQuestion
}

func whyRows(reader *ledger.Reader, dir string, opts whyOpts) ([]ledger.Row, error) {
	if opts.id != "" {
		row, ok, err := reader.ByID(opts.id)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, notFoundError{id: opts.id, dir: dir}
		}
		return []ledger.Row{row}, nil
	}
	filter := ledger.Filter{Point: opts.point}
	var window []ledger.Row
	if _, err := reader.Each(filter, func(row ledger.Row) error {
		window = append(window, row)
		if len(window) > opts.count {
			window = window[1:]
		}
		return nil
	}); err != nil {
		return nil, err
	}
	if len(window) == 0 {
		what := "--last"
		if opts.point != "" {
			what = fmt.Sprintf("--point %s --last", opts.point)
		}
		return nil, notFoundError{id: what, dir: dir}
	}
	return window, nil
}

func loadChain(reader *ledger.Reader, row ledger.Row, dir string) (whyRow, error) {
	if row.ReplayOf == "" {
		return whyRow{queried: row, chain: row}, nil
	}
	original, ok, err := reader.ByID(row.ReplayOf)
	if err != nil {
		return whyRow{}, err
	}
	if !ok {
		return whyRow{}, notFoundError{id: row.ReplayOf, dir: dir}
	}
	return whyRow{queried: row, chain: original, isReplay: true}, nil
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
