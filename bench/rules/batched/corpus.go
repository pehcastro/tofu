package main

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"
	"unicode"

	"tofu/internal/rule"
	"tofu/library"
)

const (
	emDash         = string(rune(0x2014))
	emDashMarker   = "<U+2014>"
	emDashRule     = "em_dash"
	leakRunOfWords = 8
)

type reply struct {
	ID      string          `json:"id"`
	Request string          `json:"request"`
	Reply   string          `json:"reply"`
	Labels  map[string]bool `json:"labels"`
}

func loadCorpus(path string) ([]reply, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var replies []reply
	if err := json.Unmarshal(data, &replies); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	for i := range replies {
		replies[i].Reply = strings.ReplaceAll(replies[i].Reply, emDashMarker, emDash)
	}
	return replies, nil
}

func loadRules() ([]rule.Rule, error) {
	loaded, err := rule.LoadFS(library.Files(), "library")
	if err != nil {
		return nil, err
	}
	return slices.DeleteFunc(loaded, func(r rule.Rule) bool {
		return r.Domain != rule.DomainGeneral && r.Domain != rule.DomainDev
	}), nil
}

func applicable(rules []rule.Rule, r reply) []rule.Rule {
	var asked []rule.Rule
	for i, match := range rule.Index(rules, rule.Task{Text: r.Request, Role: rule.RoleOrchestrator}) {
		if rules[i].ID == emDashRule || (match.Fires && rules[i].Kind == rule.KindHuman) {
			asked = append(asked, rules[i])
		}
	}
	slices.SortFunc(asked, func(a, b rule.Rule) int { return strings.Compare(a.ID, b.ID) })
	return asked
}

func leaks(replies []reply, rules []rule.Rule) []string {
	text := map[string][]string{}
	for _, r := range rules {
		text[r.ID] = words(r.Text)
	}
	var found []string
	for _, r := range replies {
		said := " " + strings.Join(words(r.Reply), " ") + " "
		for _, id := range slices.Sorted(maps.Keys(r.Labels)) {
			ruleWords := text[id]
			for start := 0; start+leakRunOfWords <= len(ruleWords); start++ {
				run := strings.Join(ruleWords[start:start+leakRunOfWords], " ")
				if strings.Contains(said, " "+run+" ") {
					found = append(found, fmt.Sprintf("%s quotes %s: %q", r.ID, id, run))
					break
				}
			}
		}
	}
	return found
}

func words(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
}
