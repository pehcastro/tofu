package main

import (
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"tofu/internal/judge/gate"
	"tofu/internal/judge/ledger"
	"tofu/internal/judge/state"
	"tofu/internal/sys"
)

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
}

type replayTransition struct {
	before, after ledger.Verdict
}

type replayChange struct {
	row   ledger.Row
	after ledger.Verdict
}

type replayRuleSource struct {
	ref    string
	origin gate.Origin
	pol    gate.Rule
	err    error
}

type replayNoDecider struct {
	ref    string
	schema string
}

func (n replayNoDecider) Error() string {
	return fmt.Sprintf("the rule %s declares the schema %q and replay has no decider for it", n.ref, n.schema)
}

type replayResult struct {
	read        int
	rescored    int
	skipped     int
	unavailable int
	noDecider   map[replayNoDecider]int
	changes     []replayChange
	rules       replayRules
}

func replayVerb(args []string, out, errOut io.Writer, now func() time.Time) int {
	opts, err := parseReplayArgs(args)
	if err != nil {
		return replayFail(errOut, err)
	}
	dir, err := sys.LogDir()
	if err != nil {
		return replayFail(errOut, err)
	}
	filter := ledger.Filter{Point: opts.point}
	if opts.since > 0 {
		filter.Since = now().Add(-opts.since)
	}

	start := time.Now()
	result, err := runReplay(ledger.NewReader(dir), filter, opts.sets)
	elapsed := time.Since(start)
	if err != nil {
		return replayFail(errOut, err)
	}
	printReplay(out, result, elapsed, opts.verbose)
	return exitOK
}

func replayFail(errOut io.Writer, err error) int {
	_, _ = fmt.Fprintf(errOut, "tofu replay: %v\n", err)
	return exitUsage
}

func parseReplayArgs(args []string) (replayOpts, error) {
	opts := replayOpts{sets: map[string]float64{}}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--point":
			i++
			if i >= len(args) {
				return replayOpts{}, errors.New("--point needs a name")
			}
			opts.point = args[i]
		case "--set":
			i++
			if i >= len(args) {
				return replayOpts{}, errors.New("--set needs key=value")
			}
			key, value, err := parseSetArg(args[i])
			if err != nil {
				return replayOpts{}, err
			}
			opts.sets[key] = value
		case "--since":
			i++
			if i >= len(args) {
				return replayOpts{}, errors.New("--since needs a duration")
			}
			d, err := parseSince(args[i])
			if err != nil {
				return replayOpts{}, err
			}
			opts.since = d
		case "--verbose":
			opts.verbose = true
		default:
			return replayOpts{}, fmt.Errorf("unknown argument %q", args[i])
		}
	}
	if opts.point == "" {
		return replayOpts{}, errors.New("tofu replay needs --point")
	}
	for key := range opts.sets {
		if !knownThreshold(key) {
			return replayOpts{}, fmt.Errorf("%q is not a threshold this rule declares, want one of %s", key, strings.Join(replayThresholdFields, ", "))
		}
	}
	return opts, nil
}

func parseSetArg(arg string) (string, float64, error) {
	key, raw, ok := strings.Cut(arg, "=")
	if !ok {
		return "", 0, fmt.Errorf("--set wants key=value, got %q", arg)
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return "", 0, fmt.Errorf("--set %s: %q is not a number", key, raw)
	}
	return key, value, nil
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

func knownThreshold(key string) bool {
	for _, name := range replayThresholdFields {
		if name == key {
			return true
		}
	}
	return false
}

func runReplay(reader *ledger.Reader, filter ledger.Filter, sets map[string]float64) (replayResult, error) {
	result := replayResult{noDecider: map[replayNoDecider]int{}}
	var rules replayRules
	_, err := reader.Each(filter, func(row ledger.Row) error {
		result.read++
		if row.Reason != nil && gate.IsUnavailable(row.Reason.Comparison) {
			result.unavailable++
			return nil
		}
		if row.Policy == "" {
			result.skipped++
			return nil
		}
		pol, err := rules.load(row.Policy, row.PolicyVersion)
		var missing replayNoDecider
		if errors.As(err, &missing) {
			result.noDecider[missing]++
			return nil
		}
		if err != nil {
			return err
		}
		pol.Thresholds = withOverrides(pol.Thresholds, sets)
		verdict, _, err := gate.Decide(ledgerAnswersToJev(row.Answers), pol)
		if err != nil {
			return err
		}
		result.rescored++
		after := toLedgerVerdict(verdict)
		if after != row.Verdict {
			result.changes = append(result.changes, replayChange{row: row, after: after})
		}
		return nil
	})
	result.rules = rules
	return result, err
}

type replayRules []replayRuleSource

func (p *replayRules) load(name string, version int) (gate.Rule, error) {
	ref := fmt.Sprintf("%s@%d", name, version)
	for _, source := range *p {
		if source.ref == ref {
			return source.pol, source.err
		}
	}
	pol, origin, err := replayDecide(ref)
	*p = append(*p, replayRuleSource{ref: ref, origin: origin, pol: pol, err: err})
	return pol, err
}

func replayDecide(ref string) (gate.Rule, gate.Origin, error) {
	if ref == state.StopCheckRuleRef {
		return state.StopCheckRule()
	}
	pol, origin, err := loadRulePoint(ref)
	if err != nil || pol.Schema == gate.SchemaGate {
		return pol, origin, err
	}
	return gate.Rule{}, origin, replayNoDecider{ref: ref, schema: pol.Schema}
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

func printReplay(out io.Writer, result replayResult, elapsed time.Duration, verbose bool) {
	_, _ = fmt.Fprintf(out, "  %d rows read, %d rescored, %d skipped for naming no rule, %d unavailable: the typed decision was never made    0 API calls, %s\n",
		result.read, result.rescored, result.skipped, result.unavailable, elapsed.Round(time.Millisecond))
	for _, source := range result.rules {
		if source.err != nil {
			continue
		}
		_, _ = fmt.Fprintf(out, "  rule %s from %s: %s\n", source.ref, source.origin, source.pol.File)
	}
	for _, missing := range slices.SortedFunc(maps.Keys(result.noDecider), func(a, b replayNoDecider) int { return strings.Compare(a.ref, b.ref) }) {
		_, _ = fmt.Fprintf(out, "  %d skipped: %v\n", result.noDecider[missing], missing)
	}
	_, _ = fmt.Fprintf(out, "  verdict changes: %d\n\n", len(result.changes))

	for _, t := range replayTransitions(result.changes) {
		count, agree, disagree, labeled := replayTransitionCounts(result.changes, t)
		line := fmt.Sprintf("  would now %s (was %s)   %d", strings.ToUpper(string(t.after)), t.before, count)
		if labeled > 0 {
			line += fmt.Sprintf("   of which %d carry an outcome: %d now agree, %d now disagree", labeled, agree, disagree)
		}
		_, _ = fmt.Fprintln(out, line)
	}

	if verbose {
		_, _ = fmt.Fprintln(out)
		for _, c := range result.changes {
			outcome := "no outcome"
			if c.row.Outcome != nil {
				outcome = "outcome " + c.row.Outcome.Detail
			}
			_, _ = fmt.Fprintf(out, "  %s  %s -> %s  %s\n", c.row.ID, c.row.Verdict, c.after, outcome)
		}
	}
}

func replayTransitions(changes []replayChange) []replayTransition {
	seen := map[replayTransition]bool{}
	var out []replayTransition
	for _, c := range changes {
		t := replayTransition{before: c.row.Verdict, after: c.after}
		if !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].after != out[j].after {
			return out[i].after < out[j].after
		}
		return out[i].before < out[j].before
	})
	return out
}

func replayTransitionCounts(changes []replayChange, t replayTransition) (count, agree, disagree, labeled int) {
	for _, c := range changes {
		if c.row.Verdict != t.before || c.after != t.after {
			continue
		}
		count++
		if c.row.Outcome == nil {
			continue
		}
		labeled++
		if string(c.after) == c.row.Outcome.Detail {
			agree++
		} else {
			disagree++
		}
	}
	return count, agree, disagree, labeled
}
