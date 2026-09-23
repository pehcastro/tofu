package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"tofu/internal/rule"
	"tofu/internal/sys"
	shipped "tofu/library"
)

const (
	libraryRoot         = "library"
	rulesFireSuffix     = ".rules.jsonl"
	rulesFromTheBinary  = "the binary"
	rulesFromTheProject = "the project"
)

type rulesListOpts struct {
	library string
	json    bool
}

type rulesCheckOpts struct {
	path    string
	library string
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
	ruleFireListing
	At time.Time `json:"at"`
}

func fireListing(f rule.Fire) ruleFireListing {
	return ruleFireListing{RuleID: f.RuleID, Target: f.Target, Mode: f.Mode.String(), Blocked: f.Blocked, Findings: len(f.Findings)}
}

type ruleCheckReport struct {
	Origin  string            `json:"origin"`
	Fires   []ruleFireListing `json:"fires"`
	Blocked int               `json:"blocked"`
}

type ruleListReport struct {
	Origin string        `json:"origin"`
	Rules  []ruleListing `json:"rules"`
}

func rulesVerb(args []string, out, errOut io.Writer) int {
	if len(args) == 0 {
		return rulesFail(errOut, errors.New("usage: tofu rules list|check|fired|index [path] [--task kind] [--library dir] [--json]"))
	}
	switch args[0] {
	case "list":
		return rulesListVerb(args[1:], out, errOut)
	case "check":
		return rulesCheckVerb(args[1:], out, errOut)
	case "fired":
		return rulesFiredVerb(args[1:], out, errOut)
	case "index":
		return rulesIndexVerb(args[1:], out, errOut)
	default:
		return rulesFail(errOut, fmt.Errorf("unknown subcommand %q", args[0]))
	}
}

func rulesFail(errOut io.Writer, err error) int {
	_, _ = fmt.Fprintf(errOut, "tofu rules: %v\n", err)
	return exitUsage
}

func loadRules(override string) ([]rule.Rule, string, error) {
	if override != "" {
		rules, err := rule.LoadDir(override)
		return rules, override, err
	}
	libraryDir, err := sys.LibraryDir()
	if err != nil {
		return nil, "", err
	}
	isDir, err := sys.IsDir(libraryDir)
	if err != nil {
		return nil, "", err
	}
	if isDir {
		rules, err := rule.LoadDir(libraryDir)
		return rules, rulesFromTheProject, err
	}
	rules, err := rule.LoadFS(shipped.Files(), libraryRoot)
	return rules, rulesFromTheBinary, err
}

func rulesListVerb(args []string, out, errOut io.Writer) int {
	opts, err := parseRulesListArgs(args)
	if err != nil {
		return rulesFail(errOut, err)
	}
	rules, origin, err := loadRules(opts.library)
	if err != nil {
		return rulesFail(errOut, err)
	}
	listing := make([]ruleListing, len(rules))
	for i, r := range rules {
		listing[i] = ruleListing{ID: r.ID, Kind: string(r.Kind), Mode: r.Mode.String()}
	}
	if opts.json {
		body, err := json.Marshal(ruleListReport{Origin: origin, Rules: listing})
		if err != nil {
			return rulesFail(errOut, err)
		}
		_, _ = fmt.Fprintln(out, string(body))
		return exitOK
	}
	widest := 0
	for _, r := range listing {
		widest = max(widest, len(r.ID))
	}
	_, _ = fmt.Fprintf(out, "%d rules from %s\n", len(listing), origin)
	for _, r := range listing {
		_, _ = fmt.Fprintf(out, "%-*s %-10s %s\n", widest, r.ID, r.Kind, r.Mode)
	}
	return exitOK
}

func rulesCheckVerb(args []string, out, errOut io.Writer) int {
	opts, err := parseRulesCheckArgs(args)
	if err != nil {
		return rulesFail(errOut, err)
	}
	rules, origin, err := loadRules(opts.library)
	if err != nil {
		return rulesFail(errOut, err)
	}
	roots := lintRoots
	if opts.path != "" {
		roots = []string{opts.path}
	}

	var fires []rule.Fire
	for _, root := range roots {
		present, err := sys.Exists(root)
		if err != nil {
			return rulesFail(errOut, err)
		}
		if !present {
			continue
		}
		found, err := rule.CheckTree(rules, rule.Builtins(), root, time.Now)
		if err != nil {
			return rulesFail(errOut, err)
		}
		fires = append(fires, found...)
	}

	logDir, err := sys.LogDir()
	if err != nil {
		return rulesFail(errOut, err)
	}
	blocked := 0
	for _, f := range fires {
		if err := appendRuleFire(logDir, f); err != nil {
			return rulesFail(errOut, err)
		}
		if f.Blocked {
			blocked++
		}
	}

	if opts.json {
		if err := printRulesCheckJSON(out, origin, fires, blocked); err != nil {
			return rulesFail(errOut, err)
		}
	} else {
		printRulesCheck(out, origin, fires, blocked)
	}

	if blocked > 0 {
		return exitVerdict
	}
	return exitOK
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
	line, err := json.Marshal(ruleFireRecord{ruleFireListing: fireListing(f), At: f.At})
	if err != nil {
		return err
	}
	_, err = file.Write(append(line, '\n'))
	return err
}

func printRulesCheck(out io.Writer, origin string, fires []rule.Fire, blocked int) {
	_, _ = fmt.Fprintf(out, "%d blocked of %d fires, rules from %s\n", blocked, len(fires), origin)
	for _, f := range fires {
		_, _ = fmt.Fprintf(out, "%s  %s  %s  blocked=%t\n", f.RuleID, f.Target, f.Mode, f.Blocked)
	}
}

func printRulesCheckJSON(out io.Writer, origin string, fires []rule.Fire, blocked int) error {
	listing := make([]ruleFireListing, len(fires))
	for i, f := range fires {
		listing[i] = fireListing(f)
	}
	body, err := json.Marshal(ruleCheckReport{Origin: origin, Fires: listing, Blocked: blocked})
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(out, string(body))
	return err
}

func parseRulesFlags(args []string) (library string, asJSON bool, rest []string, err error) {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--json":
			asJSON = true
		case arg == "--library":
			i++
			if i >= len(args) {
				return "", false, nil, errors.New("--library needs a directory")
			}
			library = args[i]
		case strings.HasPrefix(arg, "-"):
			return "", false, nil, fmt.Errorf("unknown argument %q", arg)
		default:
			rest = append(rest, arg)
		}
	}
	return library, asJSON, rest, nil
}

func parseRulesListArgs(args []string) (rulesListOpts, error) {
	library, asJSON, rest, err := parseRulesFlags(args)
	if err != nil {
		return rulesListOpts{}, err
	}
	if len(rest) > 0 {
		return rulesListOpts{}, fmt.Errorf("unknown argument %q", rest[0])
	}
	return rulesListOpts{library: library, json: asJSON}, nil
}

func parseRulesCheckArgs(args []string) (rulesCheckOpts, error) {
	library, asJSON, rest, err := parseRulesFlags(args)
	if err != nil {
		return rulesCheckOpts{}, err
	}
	if len(rest) > 1 {
		return rulesCheckOpts{}, fmt.Errorf("tofu rules check takes one path, got %q and %q", rest[0], rest[1])
	}
	opts := rulesCheckOpts{library: library, json: asJSON}
	if len(rest) == 1 {
		opts.path = rest[0]
	}
	return opts, nil
}
