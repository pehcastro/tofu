package rules

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"tofu/apps/cli/output"
	cli "tofu/apps/cli/page"
	"tofu/apps/cli/wiring"
	rule "tofu/internal/librarian/rules"
	"tofu/internal/platform/fs"
	"tofu/internal/platform/project"
)

const FireSuffix = ".rules.jsonl"

type fireListing struct {
	RuleID   string `json:"rule_id"`
	Target   string `json:"target"`
	Mode     string `json:"mode"`
	Blocked  bool   `json:"blocked"`
	Findings int    `json:"findings"`
}

type fireRecord struct {
	fireListing
	At time.Time `json:"at"`
}

func listFire(f rule.Fire) fireListing {
	return fireListing{RuleID: f.RuleID, Target: filepath.ToSlash(f.Target), Mode: f.Mode.String(), Blocked: f.Blocked, Findings: len(f.Findings)}
}

type checkReport struct {
	Origin  string        `json:"origin"`
	Fires   []fireListing `json:"fires"`
	Blocked int           `json:"blocked"`
}

func checkVerb(args []string, out, errOut io.Writer) int {
	o := output.Verb{Name: "rules check", UsageLine: checkUsage, AsJSON: output.JSONAsked(args), Out: out, ErrOut: errOut}
	opts, err := parseFlags(args)
	if err == nil && len(opts.rest) > 1 {
		err = fmt.Errorf("one path, got %q and %q", opts.rest[0], opts.rest[1])
	}
	if err != nil {
		return o.Usage(err)
	}
	rules, origin, err := wiring.LoadRules(opts.library, opts.dir)
	if err != nil {
		return o.Fail(err)
	}
	roots := fs.GoRoots()
	if len(opts.rest) == 1 {
		roots = opts.rest
	}

	var fires []rule.Fire
	for _, root := range roots {
		present, err := fs.Exists(root)
		if err != nil {
			return o.Fail(err)
		}
		if !present {
			continue
		}
		found, err := rule.CheckTree(rules, rule.Builtins(), root, time.Now)
		if err != nil {
			return o.Fail(err)
		}
		listed, err := patternFiles(rules, root)
		if err != nil {
			return o.Fail(err)
		}
		patterned, err := rule.CheckFiles(rules, root, listed, time.Now())
		if err != nil {
			return o.Fail(err)
		}
		fires = append(append(fires, found...), patterned...)
	}

	logDir, err := project.LogDir()
	if err != nil {
		return o.Fail(err)
	}
	report := checkReport{Origin: origin, Fires: make([]fireListing, len(fires))}
	for i, f := range fires {
		if err := appendFire(logDir, f); err != nil {
			return o.Fail(err)
		}
		report.Fires[i] = listFire(f)
		if f.Blocked {
			report.Blocked++
		}
	}
	return o.Done(report.Blocked == 0, report, report.lines)
}

func patternFiles(rules []rule.Rule, root string) ([]string, error) {
	if !slices.ContainsFunc(rules, func(r rule.Rule) bool { return r.Pattern != nil }) {
		return nil, nil
	}
	listed, err := exec.Command("git", "ls-files", "-z", "--cached", "--others", "--exclude-standard", "--", root).Output()
	if err != nil {
		return nil, fmt.Errorf("a pattern rule reads the files git lists under %s, and git ls-files failed: %w", root, err)
	}
	return strings.FieldsFunc(string(listed), func(r rune) bool { return r == 0 }), nil
}

func (report checkReport) lines(page cli.Page) []string {
	lines := page.Title("Rules check", []string{"from " + page.Path(report.Origin)}, wiring.FiresVerdict(len(report.Fires), report.Blocked))
	rows := make([]cli.Row, len(report.Fires))
	for i, f := range report.Fires {
		rows[i] = f.row(page, output.Plural(f.Findings, "finding"))
	}
	if len(rows) > 0 {
		lines = append(append(lines, ""), page.Rows(rows)...)
	}
	return lines
}

func (f fireListing) row(page cli.Page, detail string) cli.Row {
	mark := cli.Warn
	if f.Blocked {
		mark = cli.Fail
	}
	return cli.Row{Mark: mark, Cells: []string{f.RuleID, page.Path(f.Target), f.Mode}, Detail: detail}
}

func appendFire(dir string, f rule.Fire) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(dir, f.At.UTC().Format("2006-01-02")+FireSuffix)
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	line, err := json.Marshal(fireRecord{fireListing: listFire(f), At: f.At})
	if err != nil {
		return err
	}
	_, err = file.Write(append(line, '\n'))
	return err
}
