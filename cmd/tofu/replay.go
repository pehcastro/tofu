package main

import (
	"cmp"
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"tofu/interface/cli"
	"tofu/internal/judge/gate"
	"tofu/internal/judge/ledger"
	"tofu/internal/judge/state"
	"tofu/internal/sys"
	"tofu/internal/widget"
)

const replayUsage = "tofu replay --point name [--set threshold=value]... [--since 7d] [--verbose] [--json]"

var replayThresholdFields = []string{
	"risk_ask_at",
	"risk_deny_at",
	"user_requested_relax_at",
	"approval_relax_at",
	"from_untrusted_block_at",
}

type replayOpts struct {
	point   string
	sets    map[string]float64
	since   time.Duration
	verbose bool
	json    bool
}

type replayRule struct {
	Ref    string      `json:"rule"`
	Origin gate.Origin `json:"origin,omitempty"`
	File   string      `json:"file,omitempty"`
	Schema string      `json:"schema,omitempty"`
	Rows   int         `json:"rows"`
	pol    gate.Rule
	err    error
}

type replayChange struct {
	ID      string          `json:"id"`
	Before  ledger.Verdict  `json:"before"`
	After   ledger.Verdict  `json:"after"`
	Outcome *ledger.Outcome `json:"outcome,omitempty"`
}

type replayMove struct {
	Before   ledger.Verdict `json:"before"`
	After    ledger.Verdict `json:"after"`
	Rows     int            `json:"rows"`
	Labeled  int            `json:"labeled"`
	Agree    int            `json:"agree"`
	Disagree int            `json:"disagree"`
}

type replayReport struct {
	Point       string             `json:"point"`
	Sets        map[string]float64 `json:"sets"`
	Read        int                `json:"read"`
	Rescored    int                `json:"rescored"`
	NoRule      int                `json:"no_rule"`
	Unavailable int                `json:"unavailable"`
	Rules       []replayRule       `json:"rules"`
	Moves       []replayMove       `json:"moves"`
	Changes     []replayChange     `json:"changes"`
	verbose     bool
}

func replayVerb(args []string, out, errOut io.Writer, now func() time.Time) int {
	o := verbOutput{verb: "replay", usageLine: replayUsage, out: out, errOut: errOut}
	opts, err := parseReplayArgs(args)
	if err != nil {
		return o.usage(err)
	}
	o.asJSON = opts.json
	dir, err := sys.LogDir()
	if err != nil {
		return o.fail(err)
	}
	filter := ledger.Filter{Point: opts.point}
	if opts.since > 0 {
		filter.Since = now().Add(-opts.since)
	}
	report, err := runReplay(ledger.NewReader(dir), filter, opts.sets)
	if err != nil {
		return o.fail(err)
	}
	report.verbose = opts.verbose
	return o.done(true, report, report.lines)
}

func parseReplayArgs(args []string) (replayOpts, error) {
	opts := replayOpts{sets: map[string]float64{}}
	for i := 0; i < len(args); i++ {
		flag := args[i]
		switch flag {
		case "--verbose":
			opts.verbose = true
			continue
		case jsonFlag:
			opts.json = true
			continue
		case "--point", "--set", "--since":
		default:
			return replayOpts{}, fmt.Errorf("unknown argument %q", flag)
		}
		i++
		if i >= len(args) {
			return replayOpts{}, errors.New(flag + " needs a value")
		}
		var err error
		switch flag {
		case "--point":
			opts.point = args[i]
		case "--set":
			err = parseSetArg(args[i], opts.sets)
		case "--since":
			opts.since, err = parseSince(args[i])
		}
		if err != nil {
			return replayOpts{}, err
		}
	}
	if opts.point == "" {
		return replayOpts{}, errors.New("--point is needed")
	}
	return opts, nil
}

func parseSetArg(arg string, sets map[string]float64) error {
	key, raw, ok := strings.Cut(arg, "=")
	if !ok {
		return fmt.Errorf("--set wants key=value, got %q", arg)
	}
	if !slices.Contains(replayThresholdFields, key) {
		return fmt.Errorf("%q is not a threshold, want one of %s", key, strings.Join(replayThresholdFields, ", "))
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return fmt.Errorf("--set %s: %q is not a number", key, raw)
	}
	sets[key] = value
	return nil
}

func parseSince(s string) (time.Duration, error) {
	if days, ok := strings.CutSuffix(s, "d"); ok {
		n, err := strconv.Atoi(days)
		if err != nil {
			return 0, fmt.Errorf("--since %q is not a number of days", s)
		}
		return time.Duration(n) * 24 * time.Hour, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("--since %q: %w", s, err)
	}
	return d, nil
}

func runReplay(reader *ledger.Reader, filter ledger.Filter, sets map[string]float64) (replayReport, error) {
	report := replayReport{Point: filter.Point, Sets: sets, Rules: []replayRule{}, Moves: []replayMove{}, Changes: []replayChange{}}
	_, err := reader.Each(filter, func(row ledger.Row) error {
		report.Read++
		if row.Reason != nil && gate.IsUnavailable(row.Reason.Comparison) {
			report.Unavailable++
			return nil
		}
		if row.Policy == "" {
			report.NoRule++
			return nil
		}
		rule := report.rule(fmt.Sprintf("%s@%d", row.Policy, row.PolicyVersion))
		rule.Rows++
		if rule.err != nil || rule.Schema != "" {
			return rule.err
		}
		pol := rule.pol
		pol.Thresholds = withOverrides(pol.Thresholds, sets)
		verdict, _, err := gate.Decide(ledgerAnswersToJev(row.Answers), pol)
		if err != nil {
			return err
		}
		report.Rescored++
		if after := verdict.Ledger(); after != row.Verdict {
			report.Changes = append(report.Changes, replayChange{ID: row.ID, Before: row.Verdict, After: after, Outcome: row.Outcome})
		}
		return nil
	})
	for _, change := range report.Changes {
		at := slices.IndexFunc(report.Moves, func(m replayMove) bool { return m.Before == change.Before && m.After == change.After })
		if at < 0 {
			at = len(report.Moves)
			report.Moves = append(report.Moves, replayMove{Before: change.Before, After: change.After})
		}
		move := &report.Moves[at]
		move.Rows++
		if change.Outcome == nil {
			continue
		}
		move.Labeled++
		if string(change.After) == change.Outcome.Detail {
			move.Agree++
		} else {
			move.Disagree++
		}
	}
	slices.SortFunc(report.Moves, func(a, b replayMove) int {
		return cmp.Or(cmp.Compare(a.After, b.After), cmp.Compare(a.Before, b.Before))
	})
	return report, err
}

func (r *replayReport) rule(ref string) *replayRule {
	at := slices.IndexFunc(r.Rules, func(rule replayRule) bool { return rule.Ref == ref })
	if at >= 0 {
		return &r.Rules[at]
	}
	rule := replayRule{Ref: ref}
	if ref == state.StopCheckRuleRef {
		rule.pol, rule.Origin, rule.err = state.StopCheckRule()
	} else {
		rule.pol, rule.Origin, rule.err = loadRulePoint(ref, "")
		if rule.err == nil && rule.pol.Schema != gate.SchemaGate {
			rule.Schema = rule.pol.Schema
		}
	}
	rule.File = rule.pol.File
	r.Rules = append(r.Rules, rule)
	return &r.Rules[len(r.Rules)-1]
}

func withOverrides(t gate.Thresholds, sets map[string]float64) gate.Thresholds {
	for key, value := range sets {
		switch key {
		case "risk_ask_at":
			t.RiskAskAt = value
		case "risk_deny_at":
			t.RiskDenyAt = value
		case "user_requested_relax_at":
			t.UserRequestedRelaxAt = value
		case "approval_relax_at":
			t.ApprovalRelaxAt = value
		case "from_untrusted_block_at":
			t.FromUntrustedBlockAt = value
		}
	}
	return t
}

func (r replayReport) lines(page cli.Page) []string {
	count := func(n int, one, many string) string {
		if n == 1 {
			return "1 " + one
		}
		return strconv.Itoa(n) + " " + many
	}
	verdict := cli.Verdict{Mark: cli.Done, Text: "no verdict changes"}
	if len(r.Changes) > 0 {
		verdict = cli.Verdict{Mark: cli.Changed, Text: count(len(r.Changes), "verdict changes", "verdicts change")}
	}
	nonzero := func(n int) string {
		if n == 0 {
			return ""
		}
		return strconv.Itoa(n)
	}
	var sets []string
	for _, key := range slices.Sorted(maps.Keys(r.Sets)) {
		sets = append(sets, key+"="+strconv.FormatFloat(r.Sets[key], 'g', -1, 64))
	}
	facts := []cli.Fact{
		{Label: "set", Text: strings.Join(sets, " · ")},
		{Label: "rescored", Text: nonzero(r.Rescored)},
		{Label: "no rule", Text: nonzero(r.NoRule)},
		{Label: "unavailable", Text: nonzero(r.Unavailable)},
	}
	for _, rule := range r.Rules {
		text := rule.Ref + " · " + string(rule.Origin) + " · " + page.Path(rule.File)
		if rule.Schema != "" {
			text = page.Glyph(cli.Warn) + " " + rule.Ref + " · schema " + rule.Schema + " has no decider · " + count(rule.Rows, "row", "rows") + " skipped"
		}
		facts = append(facts, cli.Fact{Label: "rule", Text: text})
	}
	lines := append(page.Title("Replay", []string{r.Point, count(r.Read, "row", "rows")}, verdict), "")
	lines = append(lines, cli.Indent(page.Facts(facts)...)...)
	if len(r.Moves) == 0 {
		return lines
	}
	header := []string{"change", "rows", "labeled", "agree", "disagree"}
	rows := []cli.Row{{Cells: make([]string, len(header))}}
	for i, name := range header {
		rows[0].Cells[i] = page.Label(name)
	}
	for _, move := range r.Moves {
		cells := []string{verdictLook(move.Before).Text + " → " + verdictLook(move.After).Text}
		for _, n := range []int{move.Rows, move.Labeled, move.Agree, move.Disagree} {
			cells = append(cells, widget.Lead(strconv.Itoa(n), len(header[len(cells)])))
		}
		rows = append(rows, cli.Row{Mark: cli.Changed, Cells: cells})
	}
	lines = append(append(lines, ""), cli.Indent(page.Rows(rows)...)...)
	if !r.verbose {
		return lines
	}
	var changes []cli.Row
	for _, change := range r.Changes {
		outcome := "no outcome"
		if change.Outcome != nil {
			outcome = "outcome " + change.Outcome.Detail
		}
		changes = append(changes, cli.Row{Mark: cli.Changed, Cells: []string{change.ID, verdictLook(change.Before).Text + " → " + verdictLook(change.After).Text}, Detail: outcome})
	}
	return append(append(lines, ""), cli.Indent(page.Rows(changes)...)...)
}
