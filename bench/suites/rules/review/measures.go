package main

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
)

type window struct{ from, to time.Time }

func (w window) holds(at time.Time) bool { return !at.Before(w.from) && at.Before(w.to) }

type reading struct {
	value   float64
	n       int
	text    string
	missing string
}

const needsBoard = "needs a board export (tofu boardy list --all --json)"

type detectors struct {
	limit, limitSaid, restate, reviewer, ownerDefect, regression, openPart, qaCite, beforeCapture *regexp.Regexp
	docsOnly, decisionKind                                                                        string
	similarity                                                                                    float64
	minWords, minLetters                                                                          int
}

type bench struct {
	in inputs
	detectors
}

func share(k, n int) float64 {
	if n == 0 {
		return 0
	}
	return float64(k) / float64(n)
}

func (b bench) measure(key string, w window) (reading, error) {
	switch key {
	case "cannot_said_same_turn":
		return b.cannotSaid(w), nil
	case "epic_tickets_before_run":
		return b.epicTickets(w), nil
	case "five_lines_share":
		return b.fiveLines(w), nil
	case "regressions_per_100":
		perHundred, _ := b.regressions(w)
		return perHundred, nil
	case "regressions_caught_share":
		_, caught := b.regressions(w)
		return caught, nil
	case "rules_dropped_unlisted":
		return b.rulesDropped(), nil
	case "restatement_rate":
		return b.restatements(w), nil
	case "repeats_per_fork":
		perFork, _ := b.repeats(w)
		return perFork, nil
	case "chains_stopped_share":
		_, stopped := b.repeats(w)
		return stopped, nil
	case "wrong_memory_matches":
		return reading{missing: "needs a session trace export (tofu session trace --kind memory --json) graded by a person; not read yet"}, nil
	case "reviewer_share":
		return b.reviewerShare(w), nil
	case "before_share":
		return b.beforeShare(w), nil
	case "real_home_changes":
		return reading{missing: "needs a session trace export (tofu session trace --kind shell --json); not read yet"}, nil
	case "open_done_share":
		return b.openDone(w), nil
	}
	return reading{}, fmt.Errorf("measure %q has no reading in bench/suites/rules/review", key)
}

func (b bench) newTickets(w window) []ticket {
	var fresh []ticket
	for _, t := range b.in.tickets {
		if t.Type != "epic" && w.holds(t.Created) {
			fresh = append(fresh, t)
		}
	}
	return fresh
}

func (b bench) withoutThreads(tickets []ticket) string {
	if len(b.in.tickets) == 0 {
		return needsBoard
	}
	absent := 0
	for _, t := range tickets {
		if _, ok := b.in.threads[t.ID]; !ok {
			absent++
		}
	}
	if absent > 0 {
		return fmt.Sprintf("needs a thread export (tofu boardy show ID --thread --json) for %d of %d tickets", absent, len(tickets))
	}
	return ""
}

func (b bench) epicTickets(w window) reading {
	byEpic := map[string][]ticket{}
	for _, t := range b.in.tickets {
		if t.Type != "epic" && t.Epic != "" {
			byEpic[t.Epic] = append(byEpic[t.Epic], t)
		}
	}
	r := reading{text: "no epic started in the window"}
	var started []ticket
	for epic, tickets := range byEpic {
		first := slices.MinFunc(tickets, func(a, c ticket) int { return a.Created.Compare(c.Created) })
		if !w.holds(first.Created) {
			continue
		}
		started = append(started, tickets...)
		var firstDone time.Time
		for _, t := range tickets {
			for _, e := range b.in.threads[t.ID] {
				if e.Kind == "moved" && e.To == "done" && (firstDone.IsZero() || e.At.Before(firstDone)) {
					firstDone = e.At
				}
			}
		}
		before := 0
		for _, t := range tickets {
			if firstDone.IsZero() || !t.Created.After(firstDone) {
				before++
			}
		}
		r.n++
		if float64(before) >= r.value {
			r.value, r.text = float64(before), fmt.Sprintf("worst %s: %d tickets before its first done", epic, before)
		}
	}
	r.missing = b.withoutThreads(started)
	return r
}

func (b bench) fiveLines(w window) reading {
	if len(b.in.tickets) == 0 {
		return reading{missing: needsBoard}
	}
	tickets := b.newTickets(w)
	four, five := 0, 0
	for _, t := range tickets {
		if slices.ContainsFunc([]string{t.Before, t.Driven, t.StillWorks, t.ErrorCase}, func(line string) bool { return strings.TrimSpace(line) == "" }) {
			continue
		}
		four++
		if b.qaCite.MatchString(strings.Join(append([]string{t.Problem, t.Scope, t.Driven, t.StillWorks, t.ErrorCase}, t.Acceptance...), "\n")) {
			five++
		}
	}
	return reading{value: share(five, len(tickets)), n: len(tickets), text: fmt.Sprintf("%d of %d; the four gated lines %d", five, len(tickets), four)}
}

func (b bench) regressions(w window) (perHundred, caughtShare reading) {
	tickets := b.newTickets(w)
	found, caught := 0, 0
	for _, t := range tickets {
		if b.regression.MatchString(t.Title + "\n" + t.Problem) {
			found++
		}
	}
	for _, thread := range b.in.threads {
		for _, e := range thread {
			if e.Kind == "moved" && e.From == "review" && (e.To == "doing" || e.To == "todo") && w.holds(e.At) && b.regression.MatchString(e.Text) {
				caught++
			}
		}
	}
	all := found + caught
	missing, text := b.withoutThreads(tickets), fmt.Sprintf("%d found after done, %d sent back from review, %d tickets", found, caught, len(tickets))
	return reading{value: 100 * share(all, len(tickets)), n: len(tickets), text: text, missing: missing},
		reading{value: share(caught, all), n: all, text: text, missing: missing}
}

func (b bench) openDone(w window) reading {
	r := reading{missing: b.withoutThreads(b.newTickets(w))}
	open := 0
	for _, thread := range b.in.threads {
		for _, e := range thread {
			if e.Kind == "moved" && e.To == "done" && w.holds(e.At) {
				r.n++
				if b.openPart.MatchString(e.Text) {
					open++
				}
			}
		}
	}
	r.value, r.text = share(open, r.n), fmt.Sprintf("%d of %d done moves", open, r.n)
	return r
}

func (b bench) beforeShare(w window) reading {
	if len(b.in.tickets) == 0 {
		return reading{missing: needsBoard}
	}
	r, with := reading{}, 0
	for _, t := range b.newTickets(w) {
		if t.Status != "done" || slices.Contains(t.Labels, b.docsOnly) {
			continue
		}
		r.n++
		if b.beforeCapture.MatchString(t.Log) {
			with++
		}
	}
	r.value, r.text = share(with, r.n), fmt.Sprintf("%d of %d done, %s excluded", with, r.n, b.docsOnly)
	return r
}

func (b bench) rulesDropped() reading {
	if len(b.in.whyRows) == 0 || len(b.in.library) == 0 {
		return reading{missing: "needs a session export (tofu rules why SESSION --json and tofu rules list --json from the same home)"}
	}
	listed := map[[2]string]map[string]bool{}
	unreasoned := 0
	for _, row := range b.in.whyRows {
		pair := [2]string{row.Turn, row.Agent}
		if listed[pair] == nil {
			listed[pair] = map[string]bool{}
		}
		listed[pair][row.Rule] = true
		if row.ReasonKind == "" {
			unreasoned++
		}
	}
	unlisted, composed := 0, 0
	for _, rule := range b.in.library {
		if rule.Kind == b.decisionKind {
			continue
		}
		composed++
		for _, rules := range listed {
			if !rules[rule.ID] {
				unlisted++
			}
		}
	}
	return reading{value: float64(unlisted + unreasoned), n: len(listed), text: fmt.Sprintf("%d unlisted and %d with no reason, %d prompt rules, %d agent turns", unlisted, unreasoned, composed, len(listed))}
}

func (b bench) entries(w window) ([][]said, string) {
	if len(b.in.transcripts) == 0 {
		return nil, "needs a session export (tofu session transcript NAME --json)"
	}
	var sessions [][]said
	for _, t := range b.in.transcripts {
		var kept []said
		for _, e := range t.Entries {
			if w.holds(e.At) {
				kept = append(kept, e)
			}
		}
		sessions = append(sessions, kept)
	}
	return sessions, ""
}

func (b bench) cannotSaid(w window) reading {
	sessions, missing := b.entries(w)
	r, said, worst := reading{missing: missing}, 0, 0.0
	for _, entries := range sessions {
		for at, e := range entries {
			if e.Speaker != "report" || !b.limit.MatchString(e.Text) {
				continue
			}
			r.n++
			end := entries[len(entries)-1].At
			for _, next := range entries[at+1:] {
				if next.Speaker == "person" {
					end = next.At
					break
				}
				if next.Speaker == "lead" && b.limitSaid.MatchString(next.Text) {
					said++
					end = next.At
					break
				}
			}
			worst = max(worst, end.Sub(e.At).Hours())
		}
	}
	r.value, r.text = share(said, r.n), fmt.Sprintf("%d of %d; worst %.2f h", said, r.n, worst)
	return r
}

func (b bench) restatements(w window) reading {
	sessions, missing := b.entries(w)
	r, restated := reading{missing: missing}, 0
	for _, entries := range sessions {
		for _, e := range entries {
			if e.Speaker == "lead" {
				r.n++
				if b.restate.MatchString(e.Text) {
					restated++
				}
			}
		}
	}
	r.value, r.text = share(restated, r.n), fmt.Sprintf("%d of %d replies", restated, r.n)
	return r
}

func (b bench) words(text string) map[string]bool {
	set := map[string]bool{}
	for _, word := range strings.FieldsFunc(strings.ToLower(text), func(c rune) bool { return (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '\'' }) {
		if len(word) >= b.minLetters {
			set[word] = true
		}
	}
	return set
}

func (b bench) similar(a, c map[string]bool) bool {
	if len(a) < b.minWords || len(c) < b.minWords {
		return false
	}
	both := 0
	for word := range a {
		if c[word] {
			both++
		}
	}
	return share(both, len(a)+len(c)-both) >= b.similarity
}

func (b bench) repeats(w window) (perFork, chainsStopped reading) {
	sessions, missing := b.entries(w)
	forks := map[string]bool{}
	repeated, chains, stopped := 0, 0, 0
	for at, entries := range sessions {
		var asked []map[string]bool
		var chainOf []int
		size := map[int]int{}
		for _, e := range entries {
			forks[fmt.Sprint(at, ".", e.Generation)] = true
			if e.Speaker != "person" {
				continue
			}
			words, chain := b.words(e.Text), len(asked)
			for earlier, prior := range asked {
				if b.similar(words, prior) {
					chain = chainOf[earlier]
					break
				}
			}
			if chain != len(asked) {
				repeated++
			}
			asked, chainOf = append(asked, words), append(chainOf, chain)
			size[chain]++
		}
		for _, members := range size {
			if members >= 2 {
				chains++
				if members == 2 {
					stopped++
				}
			}
		}
	}
	return reading{value: share(repeated, len(forks)), n: len(forks), text: fmt.Sprintf("%d repeats in %d forks", repeated, len(forks)), missing: missing},
		reading{value: share(stopped, chains), n: chains, text: fmt.Sprintf("%d of %d chains", stopped, chains), missing: missing}
}

func (b bench) reviewerShare(w window) reading {
	sessions, missing := b.entries(w)
	reviewer, owner := 0, 0
	for _, entries := range sessions {
		for _, e := range entries {
			switch {
			case e.Speaker == "report" && b.reviewer.MatchString(e.Text):
				reviewer++
			case e.Speaker == "person" && b.ownerDefect.MatchString(e.Text):
				owner++
			}
		}
	}
	return reading{missing: missing, value: share(reviewer, reviewer+owner), n: reviewer + owner, text: fmt.Sprintf("reviewer %d, his %d", reviewer, owner)}
}
