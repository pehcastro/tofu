package judge

import (
	"cmp"
	"errors"
	"fmt"
	"io"
	"maps"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"tofu/apps/cli/output"
	"tofu/apps/cli/page"
	"tofu/apps/cli/wiring"
	"tofu/internal/cli/render"
	"tofu/internal/judge/gate"
	"tofu/internal/judge/ledger"
	"tofu/internal/judge/questions"
	"tofu/internal/judge/state"
	"tofu/internal/sys"
	"tofu/library/questions"
)

const ReplayUsage = "tofu replay --point name [--set threshold=value]... [--since 7d] [--verbose] [--json]"

func gateThresholdSlots(t *gate.Thresholds) map[string]*float64 {
	return map[string]*float64{
		"risk_ask_at":             &t.RiskAskAt,
		"risk_deny_at":            &t.RiskDenyAt,
		"user_requested_relax_at": &t.UserRequestedRelaxAt,
		"approval_relax_at":       &t.ApprovalRelaxAt,
		"from_untrusted_block_at": &t.FromUntrustedBlockAt,
	}
}

type replayOpts struct {
	point   string
	sets    map[string]float64
	since   time.Duration
	verbose bool
}

type replayRule struct {
	Ref    string      `json:"rule"`
	Origin gate.Origin `json:"origin,omitempty"`
	File   string      `json:"file,omitempty"`
	Schema string      `json:"schema,omitempty"`
	Rows   int         `json:"rows"`
	pol    gate.Rule
	slots  func(*gate.Thresholds) map[string]*float64
	err    error
}

type replayRow struct {
	ID       string         `json:"id"`
	Question string         `json:"question"`
	Value    float64        `json:"value"`
	Before   ledger.Verdict `json:"before"`
	BeforeAt float64        `json:"before_at"`
	After    ledger.Verdict `json:"after"`
	AfterAt  float64        `json:"after_at"`
}

type unknownThreshold struct {
	key, ref string
	want     []string
}

func (e unknownThreshold) Error() string {
	return fmt.Sprintf("%q is not a threshold of %s, want one of %s", e.key, e.ref, strings.Join(e.want, ", "))
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
	Rows        []replayRow        `json:"rows,omitempty"`
	verbose     bool
}

func ReplayVerb(args []string, out, errOut io.Writer, now func() time.Time) int {
	o := output.Verb{Name: "replay", UsageLine: ReplayUsage, AsJSON: output.JSONAsked(args), Out: out, ErrOut: errOut}
	opts, err := parseReplayArgs(args)
	if err != nil {
		return o.Usage(err)
	}
	dir, err := sys.LogDir()
	if err != nil {
		return o.Fail(err)
	}
	filter := ledger.Filter{Point: opts.point}
	if opts.since > 0 {
		filter.Since = now().Add(-opts.since)
	}
	report, err := runReplay(ledger.NewReader(dir), filter, opts.sets)
	if errors.As(err, new(unknownThreshold)) {
		return o.Usage(err)
	}
	if err != nil {
		return o.Fail(err)
	}
	report.verbose = opts.verbose
	return o.Done(true, report, report.lines)
}

func parseReplayArgs(args []string) (replayOpts, error) {
	opts := replayOpts{sets: map[string]float64{}}
	for i := 0; i < len(args); i++ {
		flag := args[i]
		switch flag {
		case "--verbose":
			opts.verbose = true
			continue
		case output.JSONFlag:
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
			return report.rescoreSet(row, sets)
		}
		ref := fmt.Sprintf("%s@%d", row.Policy, row.PolicyVersion)
		rule := report.rule(ref, func() replayRule { return gateRule(ref, sets) })
		rule.Rows++
		if rule.err != nil || rule.Schema != "" {
			return rule.err
		}
		pol := rule.pol
		named := rule.slots(&pol.Thresholds)
		for key, value := range sets {
			*named[key] = value
		}
		verdict, _, err := gate.Decide(LedgerAnswersToJev(row.Answers), pol)
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

func (r *replayReport) rescoreSet(row ledger.Row, sets map[string]float64) error {
	if row.Reason == nil || !deniesAtOrAbove(row.Questions) {
		r.NoRule++
		return nil
	}
	if len(row.Answers) == 0 {
		r.Unavailable++
		return nil
	}
	ref := row.Questions + "@" + strconv.Itoa(row.Version)
	rule := r.rule(ref, func() replayRule { return setRule(ref, sets) })
	rule.Rows++
	if rule.err != nil {
		return rule.err
	}
	at, set := sets[row.Reason.Question+"_at"]
	if !set {
		at = row.Reason.Threshold
	}
	after := ledger.VerdictAllow
	if row.Reason.Value >= at {
		after = ledger.VerdictDeny
	}
	r.Rescored++
	r.Rows = append(r.Rows, replayRow{ID: row.ID, Question: row.Reason.Question, Value: row.Reason.Value, Before: row.Verdict, BeforeAt: row.Reason.Threshold, After: after, AfterAt: at})
	if after != row.Verdict {
		r.Changes = append(r.Changes, replayChange{ID: row.ID, Before: row.Verdict, After: after, Outcome: row.Outcome})
	}
	return nil
}

func deniesAtOrAbove(questions string) bool {
	return questions == "done_reason" || questions == "ticket_feasibility"
}

func (r *replayReport) rule(ref string, load func() replayRule) *replayRule {
	at := slices.IndexFunc(r.Rules, func(rule replayRule) bool { return rule.Ref == ref })
	if at < 0 {
		at = len(r.Rules)
		r.Rules = append(r.Rules, load())
	}
	return &r.Rules[at]
}

func gateRule(ref string, sets map[string]float64) replayRule {
	rule := replayRule{Ref: ref, slots: gateThresholdSlots}
	if rule.pol, rule.Origin, rule.err = LoadRulePoint(ref, ""); rule.err != nil {
		return rule
	}
	rule.File = rule.pol.File
	switch {
	case ref == state.StopCheckRuleRef:
		rule.slots = state.StopCheckThresholdSlots
	case rule.pol.Schema != gate.SchemaGate:
		rule.Schema = rule.pol.Schema
		return rule
	}
	rule.err = unknownIn(sets, slices.Sorted(maps.Keys(rule.slots(&gate.Thresholds{}))), ref)
	return rule
}

func setRule(ref string, sets map[string]float64) replayRule {
	layers, err := question.Layers(questions.Files(), "")
	if err != nil {
		return replayRule{Ref: ref, err: err}
	}
	set, fields, err := question.Resolve(ref, layers)
	if err != nil {
		return replayRule{Ref: ref, err: err}
	}
	var thresholds []string
	for _, field := range fields {
		if name, ok := strings.CutPrefix(field.Path, "thresholds."); ok {
			thresholds = append(thresholds, name)
		}
	}
	return replayRule{Ref: ref, File: filepath.ToSlash(set.File), err: unknownIn(sets, thresholds, ref)}
}

func unknownIn(sets map[string]float64, want []string, ref string) error {
	for _, key := range slices.Sorted(maps.Keys(sets)) {
		if !slices.Contains(want, key) {
			return unknownThreshold{key: key, ref: ref, want: want}
		}
	}
	return nil
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
		switch {
		case rule.Schema != "":
			text = page.Glyph(cli.Warn) + " " + rule.Ref + " · schema " + rule.Schema + " has no decider · " + count(rule.Rows, "row", "rows") + " skipped"
		case rule.Origin == "":
			text = rule.Ref + " · " + page.Path(rule.File)
		}
		facts = append(facts, cli.Fact{Label: "rule", Text: text})
	}
	lines := append(page.Title("Replay", []string{r.Point, count(r.Read, "row", "rows")}, verdict), "")
	lines = append(lines, cli.Indent(page.Facts(facts)...)...)
	if len(r.Moves) > 0 {
		header := []string{"change", "rows", "labeled", "agree", "disagree"}
		rows := []cli.Row{{Cells: make([]string, len(header))}}
		for i, name := range header {
			rows[0].Cells[i] = page.Label(name)
		}
		for _, move := range r.Moves {
			cells := []string{wiring.VerdictLook(move.Before).Text + " → " + wiring.VerdictLook(move.After).Text}
			for _, n := range []int{move.Rows, move.Labeled, move.Agree, move.Disagree} {
				cells = append(cells, widget.Lead(strconv.Itoa(n), len(header[len(cells)])))
			}
			rows = append(rows, cli.Row{Mark: cli.Changed, Cells: cells})
		}
		lines = append(append(lines, ""), cli.Indent(page.Rows(rows)...)...)
	}
	if len(r.Rows) > 0 {
		at := func(v ledger.Verdict, threshold float64) string {
			return wiring.VerdictLook(v).Text + " at " + strconv.FormatFloat(threshold, 'g', -1, 64)
		}
		var rows []cli.Row
		for _, row := range r.Rows {
			mark := cli.None
			if row.After != row.Before {
				mark = cli.Changed
			}
			rows = append(rows, cli.Row{Mark: mark, Cells: []string{row.ID, row.Question + " " + strconv.FormatFloat(row.Value, 'f', 2, 64), at(row.Before, row.BeforeAt) + " → " + at(row.After, row.AfterAt)}})
		}
		lines = append(append(lines, ""), cli.Indent(page.Rows(rows)...)...)
	}
	if !r.verbose || len(r.Changes) == 0 {
		return lines
	}
	var changes []cli.Row
	for _, change := range r.Changes {
		outcome := "no outcome"
		if change.Outcome != nil {
			outcome = "outcome " + change.Outcome.Detail
		}
		changes = append(changes, cli.Row{Mark: cli.Changed, Cells: []string{change.ID, wiring.VerdictLook(change.Before).Text + " → " + wiring.VerdictLook(change.After).Text}, Detail: outcome})
	}
	return append(append(lines, ""), cli.Indent(page.Rows(changes)...)...)
}
