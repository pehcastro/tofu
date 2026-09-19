package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"boji/internal/rule"
	"boji/internal/sys"
)

const rulesFireSuffix = ".rules.jsonl"

var rulesTextExtensions = map[string]bool{".go": true, ".md": true}

type rulesListOpts struct {
	catalog string
	json    bool
}

type rulesCheckOpts struct {
	path    string
	catalog string
	json    bool
}

type ruleListing struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
	Mode string `json:"mode"`
}

type ruleFireListing struct {
	RuleID   string `json:"rule_id"`
	Target   string `json:"target"`
	Mode     string `json:"mode"`
	Blocked  bool   `json:"blocked"`
	Findings int    `json:"findings"`
}

type ruleFireRecord struct {
	RuleID     string    `json:"rule_id"`
	Mode       string    `json:"mode"`
	Target     string    `json:"target"`
	Blocked    bool      `json:"blocked"`
	Overridden bool      `json:"overridden"`
	Findings   int       `json:"findings"`
	At         time.Time `json:"at"`
}

type ruleCheckReport struct {
	Fires      []ruleFireListing `json:"fires"`
	Overridden int               `json:"overridden"`
	Blocked    int               `json:"blocked"`
}

func rulesVerb(args []string, out, errOut io.Writer) int {
	if len(args) == 0 {
		return rulesFail(errOut, errors.New("usage: boji rules list|check [path] [--catalog dir] [--json]"))
	}
	switch args[0] {
	case "list":
		return rulesListVerb(args[1:], out, errOut)
	case "check":
		return rulesCheckVerb(args[1:], out, errOut)
	default:
		return rulesFail(errOut, fmt.Errorf("unknown subcommand %q", args[0]))
	}
}

func rulesFail(errOut io.Writer, err error) int {
	_, _ = fmt.Fprintf(errOut, "boji rules: %v\n", err)
	return exitUsage
}

func rulesCatalogDir(override string) (string, error) {
	if override != "" {
		return override, nil
	}
	catalogDir, err := sys.CatalogDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(catalogDir, "rules"), nil
}

func rulesListVerb(args []string, out, errOut io.Writer) int {
	opts, err := parseRulesListArgs(args)
	if err != nil {
		return rulesFail(errOut, err)
	}
	dir, err := rulesCatalogDir(opts.catalog)
	if err != nil {
		return rulesFail(errOut, err)
	}
	rules, err := rule.LoadDir(dir)
	if err != nil {
		return rulesFail(errOut, err)
	}
	if opts.json {
		listing := make([]ruleListing, len(rules))
		for i, r := range rules {
			listing[i] = ruleListing{ID: r.ID, Kind: string(r.Kind), Mode: r.Mode.String()}
		}
		body, err := json.Marshal(listing)
		if err != nil {
			return rulesFail(errOut, err)
		}
		_, _ = fmt.Fprintln(out, string(body))
		return exitOK
	}
	for _, r := range rules {
		_, _ = fmt.Fprintf(out, "%-12s %-10s %s\n", r.ID, r.Kind, r.Mode)
	}
	return exitOK
}

func rulesCheckVerb(args []string, out, errOut io.Writer) int {
	opts, err := parseRulesCheckArgs(args)
	if err != nil {
		return rulesFail(errOut, err)
	}
	dir, err := rulesCatalogDir(opts.catalog)
	if err != nil {
		return rulesFail(errOut, err)
	}
	rules, err := rule.LoadDir(dir)
	if err != nil {
		return rulesFail(errOut, err)
	}
	roots := lintRoots
	if opts.path != "" {
		roots = []string{opts.path}
	}

	var ledger rule.Ledger
	fires, err := walkRuleArtifacts(rules, roots, time.Now, &ledger)
	if err != nil {
		return rulesFail(errOut, err)
	}

	logDir, err := rulesLogDir()
	if err != nil {
		return rulesFail(errOut, err)
	}
	for _, f := range fires {
		if err := appendRuleFire(logDir, f); err != nil {
			return rulesFail(errOut, err)
		}
	}

	overridden, blocked := ledger.OverrideRate()
	if opts.json {
		if err := printRulesCheckJSON(out, fires, overridden, blocked); err != nil {
			return rulesFail(errOut, err)
		}
	} else {
		printRulesCheck(out, fires, overridden, blocked)
	}

	for _, f := range fires {
		if f.Blocked {
			return exitVerdict
		}
	}
	return exitOK
}

func walkRuleArtifacts(rules []rule.Rule, roots []string, now func() time.Time, ledger *rule.Ledger) ([]rule.Fire, error) {
	checkers := rule.Builtins()
	byChecker := make(map[string][]rule.Rule)
	for _, r := range rules {
		byChecker[r.Checker] = append(byChecker[r.Checker], r)
	}

	var fires []rule.Fire
	for _, root := range roots {
		present, err := sys.Exists(root)
		if err != nil {
			return nil, err
		}
		if !present {
			continue
		}
		err = sys.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if skipRuleDir(d.Name()) {
					return filepath.SkipDir
				}
				return nil
			}
			collected, err := runFileRules(byChecker, checkers, path, now, ledger)
			if err != nil {
				return err
			}
			fires = append(fires, collected...)
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return fires, nil
}

func runFileRules(byChecker map[string][]rule.Rule, checkers map[string]rule.Checker, path string, now func() time.Time, ledger *rule.Ledger) ([]rule.Fire, error) {
	var fires []rule.Fire
	ext := filepath.Ext(path)
	if ext == ".go" {
		for _, r := range byChecker["comments"] {
			fire, err := rule.Run(r, checkers, rule.GoFile{Path: path}, path, now())
			if err != nil {
				return nil, err
			}
			if len(fire.Findings) > 0 {
				ledger.Append(fire)
				fires = append(fires, fire)
			}
		}
	}
	if rulesTextExtensions[ext] {
		for _, r := range byChecker["em_dash"] {
			fire, err := rule.Run(r, checkers, rule.TextFile{Path: path}, path, now())
			if err != nil {
				return nil, err
			}
			if len(fire.Findings) > 0 {
				ledger.Append(fire)
				fires = append(fires, fire)
			}
		}
	}
	return fires, nil
}

func skipRuleDir(name string) bool {
	switch name {
	case ".git", ".boji", "node_modules", "vendor":
		return true
	default:
		return false
	}
}

func rulesLogDir() (string, error) {
	state, err := sys.ProjectStateDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(state, "log"), nil
}

func appendRuleFire(dir string, f rule.Fire) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(dir, f.At.UTC().Format("2006-01-02")+rulesFireSuffix)
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	line, err := json.Marshal(ruleFireRecord{
		RuleID:     f.RuleID,
		Mode:       f.Mode.String(),
		Target:     f.Target,
		Blocked:    f.Blocked,
		Overridden: f.Overridden,
		Findings:   len(f.Findings),
		At:         f.At,
	})
	if err != nil {
		return err
	}
	_, err = file.Write(append(line, '\n'))
	return err
}

func printRulesCheck(out io.Writer, fires []rule.Fire, overridden, blocked int) {
	for _, f := range fires {
		_, _ = fmt.Fprintf(out, "%s  %s  %s  blocked=%t\n", f.RuleID, f.Target, f.Mode, f.Blocked)
	}
	_, _ = fmt.Fprintf(out, "override rate: %d/%d blocked fires overridden\n", overridden, blocked)
}

func printRulesCheckJSON(out io.Writer, fires []rule.Fire, overridden, blocked int) error {
	listing := make([]ruleFireListing, len(fires))
	for i, f := range fires {
		listing[i] = ruleFireListing{RuleID: f.RuleID, Target: f.Target, Mode: f.Mode.String(), Blocked: f.Blocked, Findings: len(f.Findings)}
	}
	body, err := json.Marshal(ruleCheckReport{Fires: listing, Overridden: overridden, Blocked: blocked})
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(out, string(body))
	return err
}

func parseRulesListArgs(args []string) (rulesListOpts, error) {
	var opts rulesListOpts
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "--json":
			opts.json = true
		case "--catalog":
			i++
			if i >= len(args) {
				return rulesListOpts{}, errors.New("--catalog needs a directory")
			}
			opts.catalog = args[i]
		default:
			return rulesListOpts{}, fmt.Errorf("unknown argument %q", arg)
		}
	}
	return opts, nil
}

func parseRulesCheckArgs(args []string) (rulesCheckOpts, error) {
	var opts rulesCheckOpts
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--json":
			opts.json = true
		case arg == "--catalog":
			i++
			if i >= len(args) {
				return rulesCheckOpts{}, errors.New("--catalog needs a directory")
			}
			opts.catalog = args[i]
		case opts.path == "":
			opts.path = arg
		default:
			return rulesCheckOpts{}, fmt.Errorf("unknown argument %q", arg)
		}
	}
	return opts, nil
}
