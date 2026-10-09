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

type capture struct {
	Say   int    `json:"say"`
	Label string `json:"label"`
}

type question struct {
	Ask     string   `json:"ask"`
	Key     []string `json:"key"`
	Capture *capture `json:"capture"`
}

func (c capture) from(reply string) string {
	marker := c.Label + "="
	at := strings.LastIndex(reply, marker)
	if at < 0 {
		return ""
	}
	fields := strings.Fields(reply[at+len(marker):])
	if len(fields) == 0 {
		return ""
	}
	return strings.Trim(fields[0], "`*_.,;:'\"()")
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

type sources struct {
	asks, typed, later []string
}

func (ch chain) sources() sources {
	s := sources{typed: []string{ch.Lead}}
	for _, q := range ch.Questions {
		s.asks = append(s.asks, q.Ask)
	}
	for i, said := range ch.Sessions {
		s.typed = append(s.typed, said.Says...)
		if i > 0 {
			s.later = append(s.later, slices.Collect(maps.Values(said.Files))...)
		}
	}
	return s
}

func (s sources) leaksOf(id, value string) []leak {
	var found []leak
	for _, ask := range s.asks {
		if holds(ask, value) {
			found = append(found, leak{id, value, "a question names it"})
		}
		for _, part := range components(value) {
			if len(part) >= componentRunes && holds(ask, part) {
				found = append(found, leak{id, value, fmt.Sprintf("a question names %q, a component of it", part)})
			}
		}
	}
	for _, said := range s.typed {
		if holds(said, value) {
			found = append(found, leak{id, value, "the person types it, and a fork carries every typed message word for word"})
		}
	}
	for _, body := range s.later {
		if holds(body, value) {
			found = append(found, leak{id, value, "a later session holds it"})
		}
	}
	return found
}

func (c corpus) leaks() []leak {
	var found []leak
	for _, ch := range c.Chains {
		if len(ch.Sessions) < sessionsPerChain || len(ch.Questions) != questionsPerChain {
			found = append(found, leak{ch.ID, "", fmt.Sprintf("has %d sessions and %d questions, and a chain needs %d sessions, so two fork boundaries, and %d questions",
				len(ch.Sessions), len(ch.Questions), sessionsPerChain, questionsPerChain)})
			continue
		}
		s := ch.sources()
		for _, q := range ch.Questions {
			if q.Capture != nil {
				found = append(found, q.Capture.leaks(ch, s)...)
				continue
			}
			answerable := false
			for _, value := range q.Key {
				for _, body := range ch.Sessions[0].Files {
					answerable = answerable || holds(body, value)
				}
				found = append(found, s.leaksOf(ch.ID, value)...)
			}
			if !answerable {
				found = append(found, leak{ch.ID, strings.Join(q.Key, " | "), "no first session file holds any form of it, so the question cannot be answered"})
			}
		}
	}
	return found
}

func (c capture) leaks(ch chain, s sources) []leak {
	marker := c.Label + "="
	first := ch.Sessions[0].Says
	if c.Say >= len(first) || !strings.Contains(first[c.Say], marker) {
		return []leak{{ch.ID, marker, "the first session message it names does not ask for it, so the lead never says it"}}
	}
	uses := 0
	for _, text := range append(slices.Clone(s.typed), s.asks...) {
		uses += strings.Count(text, marker)
	}
	if uses > 1 {
		return []leak{{ch.ID, marker, "more than one message the person types asks for it, so the value is not settled once in the first session"}}
	}
	return nil
}

func printLeaks(say printer, c corpus, found []leak) {
	questions, values, captured := 0, 0, 0
	for _, ch := range c.Chains {
		questions += len(ch.Questions)
		for _, q := range ch.Questions {
			values += len(q.Key)
			if q.Capture != nil {
				captured++
			}
		}
	}
	say("leakage check, corpus %s: %d chains, %d questions, %d written answer forms, %d answers the lead settles in the first session\n", c.Name, len(c.Chains), questions, values, captured)
	say("  each form, matched whole and case blind, must be absent from every question, every message the person types and every later session,\n")
	say("  no question may name a part of it of %d characters or more, and some first session file must hold it.\n", componentRunes)
	say("  a settled answer is asked for in one first session message only, and the same rules run on the value the lead said before it is graded\n")
	for _, l := range found {
		say("  LEAK %s %q: %s\n", l.chain, l.value, l.where)
	}
	say("  %d leaks\n\n", len(found))
}
