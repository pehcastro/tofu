package main

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"tofu/internal/rule"
)

type rulesIndexOpts struct {
	text    string
	paths   []string
	verb    rule.Verb
	library string
	json    bool
}

type ruleIndexListing struct {
	RuleID  string `json:"rule_id"`
	Fires   bool   `json:"fires"`
	Why     string `json:"why"`
	Concern string `json:"concern"`
	Kind    string `json:"kind"`
	Mode    string `json:"mode"`
}

type ruleIndexReport struct {
	Origin   string             `json:"origin"`
	Task     string             `json:"task"`
	TaskKind string             `json:"task_kind"`
	Paths    []string           `json:"paths"`
	Firing   int                `json:"firing"`
	Rules    []ruleIndexListing `json:"rules"`
}

func rulesIndexVerb(args []string, out, errOut io.Writer) int {
	opts, err := parseRulesIndexArgs(args)
	if err != nil {
		return rulesFail(errOut, err)
	}
	rules, origin, err := loadRules(opts.library)
	if err != nil {
		return rulesFail(errOut, err)
	}
	index := rule.Index(rules, rule.Task{Text: opts.text, Paths: opts.paths, Verb: opts.verb})

	listing := make([]ruleIndexListing, len(index))
	firing := 0
	var concerns []string
	for i, m := range index {
		listing[i] = ruleIndexListing{
			RuleID:  m.RuleID,
			Fires:   m.Fires,
			Why:     m.Why,
			Concern: string(rules[i].Concern),
			Kind:    string(rules[i].Kind),
			Mode:    rules[i].Mode.String(),
		}
		if m.Fires {
			firing++
			if !slices.Contains(concerns, listing[i].Concern) {
				concerns = append(concerns, listing[i].Concern)
			}
		}
	}

	if opts.json {
		body, err := json.Marshal(ruleIndexReport{
			Origin:   origin,
			Task:     opts.text,
			TaskKind: string(opts.verb),
			Paths:    opts.paths,
			Firing:   firing,
			Rules:    listing,
		})
		if err != nil {
			return rulesFail(errOut, err)
		}
		_, _ = fmt.Fprintln(out, string(body))
		return exitOK
	}

	paths := "none, so a scope and a language reach nothing and hold their rule back"
	if len(opts.paths) > 0 {
		paths = strings.Join(opts.paths, ", ")
	}
	_, _ = fmt.Fprintf(out, "task: %s\nkind: %s\npaths: %s\nrules from %s\n",
		opts.text,
		cmp.Or(string(opts.verb), "unnamed, so a rule that names a task holds back"),
		paths,
		origin)
	rule.WriteIndex(out, index)
	_, _ = fmt.Fprintf(out, "concerns firing: %s\n", cmp.Or(strings.Join(concerns, ", "), "none"))
	return exitOK
}

func parseRulesIndexArgs(args []string) (rulesIndexOpts, error) {
	kept := make([]string, 0, len(args))
	verb := rule.VerbNone
	for i := 0; i < len(args); i++ {
		if args[i] != "--task" {
			kept = append(kept, args[i])
			continue
		}
		i++
		if i >= len(args) {
			return rulesIndexOpts{}, fmt.Errorf("--task is %s, %s, %s or %s, and it needs a value", rule.VerbDebug, rule.VerbExplore, rule.VerbReview, rule.VerbWrite)
		}
		verb = rule.Verb(args[i])
	}
	switch verb {
	case rule.VerbNone, rule.VerbDebug, rule.VerbExplore, rule.VerbReview, rule.VerbWrite:
	default:
		return rulesIndexOpts{}, fmt.Errorf("--task is %s, %s, %s or %s, found %q", rule.VerbDebug, rule.VerbExplore, rule.VerbReview, rule.VerbWrite, verb)
	}
	library, asJSON, rest, err := parseRulesFlags(kept)
	if err != nil {
		return rulesIndexOpts{}, err
	}
	if len(rest) == 0 {
		return rulesIndexOpts{}, errors.New("tofu rules index needs the task in words, then the paths the task names")
	}
	return rulesIndexOpts{text: rest[0], paths: rest[1:], verb: verb, library: library, json: asJSON}, nil
}
