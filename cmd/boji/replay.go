package main

import (
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

	"boji/internal/judge/ledger"
	"boji/internal/judge/policy"
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

type replayPolicySource struct {
	ref    string
	origin policy.Origin
	pol    policy.Policy
}

type replayResult struct {
	read        int
	rescored    int
	skipped     int
	unavailable int
	changes     []replayChange
	policies    replayPolicies
}

func replayVerb(args []string, out, errOut io.Writer, now func() time.Time) int {
	opts, err := parseReplayArgs(args)
	if err != nil {
		return replayFail(errOut, err)
	}
	dir, err := ledger.Dir()
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
	_, _ = fmt.Fprintf(errOut, "boji replay: %v\n", err)
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
		return replayOpts{}, errors.New("boji replay needs --point")
	}
	for key := range opts.sets {
		if !knownThreshold(key) {
			return replayOpts{}, fmt.Errorf("%q is not a threshold this policy declares, want one of %s", key, strings.Join(replayThresholdFields, ", "))
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
	var result replayResult
	var policies replayPolicies
	_, err := reader.Each(filter, func(row ledger.Row) error {
		result.read++
		if row.Reason != nil && policy.IsUnavailable(row.Reason.Comparison) {
			result.unavailable++
			return nil
		}
		if row.Policy == "" {
			result.skipped++
			return nil
		}
		pol, err := policies.load(row.Policy, row.PolicyVersion)
		if err != nil {
			return err
		}
		pol.Thresholds = withOverrides(pol.Thresholds, sets)
		verdict, _, err := policy.Decide(ledgerAnswersToJev(row.Answers), pol)
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
	result.policies = policies
	return result, err
}

type replayPolicies []replayPolicySource

func (p *replayPolicies) load(name string, version int) (policy.Policy, error) {
	ref := fmt.Sprintf("%s@%d", name, version)
	for _, source := range *p {
		if source.ref == ref {
			return source.pol, nil
		}
	}
	pol, origin, err := loadPolicyPoint(ref)
	if err != nil {
		return policy.Policy{}, err
	}
	*p = append(*p, replayPolicySource{ref: ref, origin: origin, pol: pol})
	return pol, nil
}

func withOverrides(t policy.Thresholds, sets map[string]float64) policy.Thresholds {
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
	_, _ = fmt.Fprintf(out, "  %d rows read, %d rescored, %d skipped for having no policy, %d unavailable: the typed decision was never made    0 API calls, %s\n",
		result.read, result.rescored, result.skipped, result.unavailable, elapsed.Round(time.Millisecond))
	for _, source := range result.policies {
		_, _ = fmt.Fprintf(out, "  policy %s from %s: %s\n", source.ref, source.origin, source.pol.File)
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
