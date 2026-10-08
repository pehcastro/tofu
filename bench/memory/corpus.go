package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
	"unicode"
)

//go:embed testdata/*.json
var corpora embed.FS

const (
	questionsPerChain = 3
	sessionsPerChain  = 3
	componentRunes    = 3
)

type question struct {
	Ask string   `json:"ask"`
	Key []string `json:"key"`
}

type session struct {
	Files map[string]string `json:"files"`
	Says  []string          `json:"says"`
}

type chain struct {
	ID        string     `json:"id"`
	Sessions  []session  `json:"sessions"`
	Lead      string     `json:"lead"`
	Questions []question `json:"questions"`
}

type corpus struct {
	Name    string    `json:"name"`
	Closers [2]string `json:"closers"`
	Chains  []chain   `json:"chains"`
}

func loadCorpus(name string) (corpus, error) {
	body, err := corpora.ReadFile("testdata/" + name + ".json")
	if err != nil {
		return corpus{}, fmt.Errorf("no corpus %q under bench/memory/testdata: %w", name, err)
	}
	var loaded corpus
	if err := json.Unmarshal(body, &loaded); err != nil {
		return corpus{}, fmt.Errorf("corpus %q does not parse: %w", name, err)
	}
	for _, ch := range loaded.Chains {
		for i := range ch.Sessions {
			ch.Sessions[i].Says = append(ch.Sessions[i].Says, loaded.Closers[min(i, 1)])
		}
	}
	return loaded, nil
}

func (c chain) ask() string {
	var text strings.Builder
	text.WriteString(c.Lead + "\n")
	for i, q := range c.Questions {
		fmt.Fprintf(&text, "%d. %s\n", i+1, q.Ask)
	}
	fmt.Fprintf(&text, "Answer in exactly %d numbered lines, one per question, the value alone. Write unknown on a line you cannot answer from this conversation. Do not guess.", len(c.Questions))
	return text.String()
}

func holds(text, value string) bool {
	text, value = strings.ToLower(text), strings.ToLower(value)
	for from := 0; ; {
		at := strings.Index(text[from:], value)
		if at < 0 {
			return false
		}
		at += from
		end := at + len(value)
		if !continues(text, at-1, value[0]) && !continues(text, end, value[len(value)-1]) {
			return true
		}
		from = at + 1
	}
}

func continues(text string, at int, edge byte) bool {
	if at < 0 || at >= len(text) {
		return false
	}
	return unicode.IsLetter(rune(text[at])) && unicode.IsLetter(rune(edge)) || unicode.IsDigit(rune(text[at])) && unicode.IsDigit(rune(edge))
}

func components(value string) []string {
	return strings.FieldsFunc(value, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
}

type leak struct {
	chain, value, where string
}

func (c corpus) leaks() []leak {
	var found []leak
	for _, ch := range c.Chains {
		if len(ch.Sessions) < sessionsPerChain || len(ch.Questions) != questionsPerChain {
			found = append(found, leak{ch.ID, "", fmt.Sprintf("has %d sessions and %d questions, and a chain needs %d sessions, so two fork boundaries, and %d questions",
				len(ch.Sessions), len(ch.Questions), sessionsPerChain, questionsPerChain)})
			continue
		}
		typed, later := []string{ch.Lead}, []string{}
		for i, s := range ch.Sessions {
			typed = append(typed, s.Says...)
			if i > 0 {
				later = append(later, slices.Collect(maps.Values(s.Files))...)
			}
		}
		for _, q := range ch.Questions {
			answerable := false
			for _, value := range q.Key {
				for _, body := range ch.Sessions[0].Files {
					answerable = answerable || holds(body, value)
				}
				for _, other := range ch.Questions {
					if holds(other.Ask, value) {
						found = append(found, leak{ch.ID, value, "a question names it"})
					}
					for _, part := range components(value) {
						if len(part) >= componentRunes && holds(other.Ask, part) {
							found = append(found, leak{ch.ID, value, fmt.Sprintf("a question names %q, a component of it", part)})
						}
					}
				}
				for _, said := range typed {
					if holds(said, value) {
						found = append(found, leak{ch.ID, value, "the person types it, and a fork carries every typed message word for word"})
					}
				}
				for _, body := range later {
					if holds(body, value) {
						found = append(found, leak{ch.ID, value, "a file of a later session holds it"})
					}
				}
			}
			if !answerable {
				found = append(found, leak{ch.ID, strings.Join(q.Key, " | "), "no first session file holds any form of it, so the question cannot be answered"})
			}
		}
	}
	return found
}

func printLeaks(say printer, c corpus, found []leak) {
	questions, values := 0, 0
	for _, ch := range c.Chains {
		questions += len(ch.Questions)
		for _, q := range ch.Questions {
			values += len(q.Key)
		}
	}
	say("leakage check, corpus %s: %d chains, %d questions, %d accepted answer forms\n", c.Name, len(c.Chains), questions, values)
	say("  each form, matched whole and case blind, must be absent from every question, every message the person types and every file of a later session,\n")
	say("  no question may name a part of it of %d characters or more, and some first session file must hold it\n", componentRunes)
	for _, l := range found {
		say("  LEAK %s %q: %s\n", l.chain, l.value, l.where)
	}
	say("  %d leaks\n\n", len(found))
}
