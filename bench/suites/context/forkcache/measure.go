package main

import (
	"encoding/json"
	"strings"
)

const parentLookupsWeighed = 20

type forkRow struct {
	step                                 int
	parentBefore, parentLast, childFirst usage
	asides                               usage
	lived                                int
	held, named, gone                    int
	shared                               int
	look                                 reading
}

type flip struct {
	exchange
	request int
	from    string
	shared  int
	prior   int
}

func flipsOf(sent []exchange, ratio bytesPerThousandTokens) []flip {
	var flips []flip
	for at := 1; at < len(sent); at++ {
		if sent[at].choice != sent[at-1].choice {
			flips = append(flips, flip{exchange: sent[at], request: at + 1, from: sent[at-1].choice, shared: ratio.sharedTokens(sent[at-1].blocks, sent[at].blocks),
				prior: sent[at-1].billed.read + sent[at-1].billed.written()})
		}
	}
	return flips
}

func pastLookback(sent []exchange) int {
	past := 0
	for _, e := range sent {
		if e.look.pastLookback() {
			past++
		}
	}
	return past
}

func (f forkRow) grown() int { return max(0, f.parentLast.total()-f.parentBefore.total()) }

func (f forkRow) continued() float64 {
	weight := write5mWeight
	if f.parentLast.write1h > 0 {
		weight = write1hWeight
	}
	return readWeight*float64(f.parentLast.total()) + weight*float64(f.grown())
}

func (f forkRow) extra() float64 { return f.childFirst.cost() + f.asides.cost() - f.continued() }

func (f forkRow) saved() float64 {
	return readWeight * float64(f.parentLast.total()+f.grown()-f.childFirst.total())
}

type measured struct {
	arm, session       string
	steps, requests    int
	forks              []forkRow
	listedForks        int
	inside, insideGone int
	flips              []flip
	past               int
}

func measure(armName string, played script, sent []exchange, lookups []lookup, listed string, ratio bytesPerThousandTokens) measured {
	m := measured{arm: armName, session: played.name, requests: len(sent), flips: flipsOf(sent, ratio), past: pastLookback(sent)}
	var sessions struct {
		Data struct {
			Sessions []struct {
				Generations int `json:"generations"`
			} `json:"sessions"`
		} `json:"data"`
	}
	_ = json.Unmarshal([]byte(listed), &sessions)
	for _, s := range sessions.Data.Sessions {
		m.listedForks += max(0, s.Generations-1)
	}
	var steps []int
	for at, e := range sent {
		if e.step {
			steps = append(steps, at)
		}
	}
	m.steps = len(steps)
	generation := 0
	for k, at := range steps {
		if k > 0 && sent[at].messages < sent[steps[k-1]].messages {
			m.forks = append(m.forks, forked(sent, lookups, steps, generation, k, ratio))
			generation = at
			continue
		}
		for _, l := range lastLookups(lookups, generation, at-1) {
			m.inside++
			if !strings.Contains(sent[at].body, l.marker) {
				m.insideGone++
			}
		}
	}
	for at := range m.forks {
		next := len(steps)
		if at+1 < len(m.forks) {
			next = m.forks[at+1].step
		}
		m.forks[at].lived = next - m.forks[at].step
	}
	return m
}

func forked(sent []exchange, lookups []lookup, steps []int, generation, k int, ratio bytesPerThousandTokens) forkRow {
	child, parent := steps[k], steps[k-1]
	row := forkRow{step: k, parentLast: sent[parent].billed, childFirst: sent[child].billed, shared: ratio.sharedTokens(sent[parent].blocks, sent[child].blocks), look: sent[child].look}
	if k > 1 && steps[k-2] >= generation {
		row.parentBefore = sent[steps[k-2]].billed
	}
	for _, aside := range sent[parent+1 : child] {
		row.asides = row.asides.plus(aside.billed)
	}
	for _, l := range lastLookups(lookups, generation, parent) {
		switch {
		case strings.Contains(sent[child].body, l.marker):
			row.held++
		case strings.Contains(sent[child].body, l.source):
			row.named++
		default:
			row.gone++
		}
	}
	return row
}

func lastLookups(lookups []lookup, from, through int) []lookup {
	var kept []lookup
	for _, l := range lookups {
		if l.reply >= from && l.reply <= through && l.marker != "" {
			kept = append(kept, l)
		}
	}
	return kept[max(0, len(kept)-parentLookupsWeighed):]
}
