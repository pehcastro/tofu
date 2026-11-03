package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"tofu/internal/judge/ledger"
	"tofu/internal/project/boardy"
	"tofu/internal/sys"
)

type labelled struct {
	Title      string `json:"title"`
	Acceptance string `json:"acceptance"`
	Reason     string `json:"reason"`
	Open       bool   `json:"open"`
	Kind       string `json:"kind"`
}

type judgedReason struct {
	labelled
	lexical bool
	rounds  []ledger.Row
}

func (j judgedReason) held(round int) bool { return j.rounds[round].Reason.ModeReason != nil }

func (j judgedReason) refused(round int, at float64) bool {
	return !j.held(round) && j.rounds[round].Reason.Value >= at
}

var questionWords = regexp.MustCompile(`(?i)\b(open|later|follow-?ups?|skip\w*|not verified|unverified|partly|partial\w*|except\w*|remain\w*|still|not yet|todo)\b`)

func calibrate(tofu, samplePath, scratch, thresholdList string, workers, rounds int) error {
	var sample []labelled
	raw, err := os.ReadFile(samplePath)
	if err == nil {
		err = json.Unmarshal(raw, &sample)
	}
	if err != nil {
		return fmt.Errorf("reading the sample: %w", err)
	}
	var thresholds []float64
	for field := range strings.SplitSeq(thresholdList, ",") {
		at, err := strconv.ParseFloat(strings.TrimSpace(field), 64)
		if err != nil {
			return fmt.Errorf("-at: %w", err)
		}
		thresholds = append(thresholds, at)
	}
	if err := os.MkdirAll(scratch, 0o755); err != nil {
		return err
	}
	judged := make([]judgedReason, len(sample))
	for at, s := range sample {
		judged[at] = judgedReason{labelled: s, lexical: questionWords.MatchString(s.Reason)}
	}
	started := time.Now()
	for range rounds {
		if err := judgeRound(tofu, scratch, judged, workers); err != nil {
			return err
		}
	}
	took := time.Since(started)
	version, err := exec.Command(tofu, "version").Output()
	if err != nil {
		return fmt.Errorf("%s version: %w", tofu, err)
	}
	host, _ := os.Hostname()
	open := count(judged, func(j judgedReason) bool { return j.Open })
	finished := len(judged) - open
	fmt.Printf("bench boardy replay -sample %s, %s\n\n", samplePath, time.Now().Format(time.DateTime))
	fmt.Printf("  machine     %s, %s/%s, %d cpus\n", host, runtime.GOOS, runtime.GOARCH, runtime.NumCPU())
	fmt.Printf("  tofu        %s\n", strings.Join(strings.Fields(string(version)), " "))
	fmt.Printf("  classifier  %s\n", conditions(judged))
	fmt.Printf("  calls       %d rounds, each in a home made fresh for it, one done move per reason, %d at once, %s wall clock; no call discarded and no warm-up\n", rounds, workers, took.Round(time.Second))
	fmt.Printf("  sample      %d reasons: %d name open work, %d do not, labelled by hand against the question's own criteria; stratified, so the open share is not the board's\n\n", len(judged), open, finished)
	armsTable(judged, thresholds, rounds)
	kindsTable(judged, thresholds)
	reasonsTable(judged, rounds)
	return nil
}

func armsTable(judged []judgedReason, thresholds []float64, rounds int) {
	open := count(judged, func(j judgedReason) bool { return j.Open })
	finished := len(judged) - open
	fmt.Println("| arm | refused | false refusals | missed open parts | right | verdict differs between rounds |")
	fmt.Println("|---|---|---|---|---|---|")
	for _, at := range thresholds {
		var refusedN, falseN, missedN, rightN []int
		for round := range rounds {
			refusedN = append(refusedN, count(judged, func(j judgedReason) bool { return j.refused(round, at) }))
			falseN = append(falseN, count(judged, func(j judgedReason) bool { return !j.Open && j.refused(round, at) }))
			missedN = append(missedN, count(judged, func(j judgedReason) bool { return j.Open && !j.refused(round, at) }))
			rightN = append(rightN, len(judged)-falseN[round]-missedN[round])
		}
		flips := count(judged, func(j judgedReason) bool {
			for round := range rounds {
				if j.refused(round, at) != j.refused(0, at) {
					return true
				}
			}
			return false
		})
		fmt.Printf("| Jev done_reason@1, open_at %.2f | %s of %d | %s of %d | %s of %d | %s of %d | %d of %d |\n", at, spreadOf(refusedN), len(judged), spreadOf(falseN), finished, spreadOf(missedN), open, spreadOf(rightN), len(judged), flips, len(judged))
	}
	lexicalFalse := count(judged, func(j judgedReason) bool { return !j.Open && j.lexical })
	lexicalMissed := count(judged, func(j judgedReason) bool { return j.Open && !j.lexical })
	fmt.Printf("| no model: the question's own words as a regular expression | %d of %d | %d of %d | %d of %d | %d of %d | 0 of %d |\n", count(judged, func(j judgedReason) bool { return j.lexical }), len(judged), lexicalFalse, finished, lexicalMissed, open, len(judged)-lexicalFalse-lexicalMissed, len(judged), len(judged))
	heldN := 0
	for round := range rounds {
		heldN += count(judged, func(j judgedReason) bool { return j.held(round) })
	}
	fmt.Printf("\ncells are the median over rounds, then the lowest and highest round; held for the person because Jev did not answer: %d of %d calls\n\n", heldN, rounds*len(judged))
}

func kindsTable(judged []judgedReason, thresholds []float64) {
	fmt.Print("| kind | n | open, min | open, median | open, max |")
	for _, at := range thresholds {
		fmt.Printf(" refused at %.2f |", at)
	}
	fmt.Println("\n|---|---|---|---|---|" + strings.Repeat("---|", len(thresholds)))
	var kinds []string
	for _, j := range judged {
		if !slices.Contains(kinds, j.Kind) {
			kinds = append(kinds, j.Kind)
		}
	}
	for _, kind := range kinds {
		var values []float64
		for _, j := range judged {
			for round, row := range j.rounds {
				if j.Kind == kind && !j.held(round) {
					values = append(values, row.Reason.Value)
				}
			}
		}
		if len(values) == 0 {
			continue
		}
		slices.Sort(values)
		fmt.Printf("| %s | %d | %.2f | %.2f | %.2f |", kind, count(judged, func(j judgedReason) bool { return j.Kind == kind }), values[0], values[len(values)/2], values[len(values)-1])
		for _, at := range thresholds {
			refusedAt := 0
			for _, value := range values {
				if value >= at {
					refusedAt++
				}
			}
			fmt.Printf(" %d of %d calls |", refusedAt, len(values))
		}
		fmt.Println()
	}
}

func reasonsTable(judged []judgedReason, rounds int) {
	fmt.Println("\n| # | labelled open | Jev open, each round | regex |")
	fmt.Println("|---|---|---|---|")
	for at, j := range judged {
		values := make([]string, rounds)
		for round, row := range j.rounds {
			values[round] = "held"
			if !j.held(round) {
				values[round] = fmt.Sprintf("%.2f", row.Reason.Value)
			}
		}
		fmt.Printf("| %d | %t | %s | %t |\n", at+1, j.Open, strings.Join(values, " "), j.lexical)
	}
}

func judgeRound(tofu, scratch string, judged []judgedReason, workers int) error {
	managed, err := freshHome(tofu, scratch)
	if err != nil {
		return err
	}
	err = spread(managed, workers, len(judged), func(key string, at int) error {
		made, err := proved(managed, key, judged[at].Title, []string{judged[at].Acceptance}, []int{1}, true)
		if err != nil {
			return err
		}
		_, err = managed.Move(made.ID, boardy.Done, judged[at].Reason, replayActor)
		var waiting boardy.WaitingError
		if err == nil || errors.As(err, &waiting) || refused(err) {
			return nil
		}
		return err
	})
	if err != nil {
		return err
	}
	places, err := sys.ResolvePlaces(managed.Store.Root)
	if err != nil {
		return err
	}
	seen := 0
	_, err = ledger.NewReader(places.Log).Each(ledger.Filter{Point: "done_reason"}, func(row ledger.Row) error {
		var state struct {
			Reason string `json:"reason"`
		}
		if err := json.Unmarshal(row.State, &state); err != nil {
			return err
		}
		at := slices.IndexFunc(judged, func(j judgedReason) bool { return j.Reason == state.Reason })
		if at < 0 || row.Reason == nil {
			return fmt.Errorf("a done_reason row the sample does not explain: %q", state.Reason)
		}
		judged[at].rounds = append(judged[at].rounds, row)
		seen++
		return nil
	})
	if err == nil && seen != len(judged) {
		err = fmt.Errorf("%d done_reason rows for %d reasons", seen, len(judged))
	}
	return err
}

func count(judged []judgedReason, keep func(judgedReason) bool) int {
	n := 0
	for _, j := range judged {
		if keep(j) {
			n++
		}
	}
	return n
}

func spreadOf(counts []int) string {
	sorted := slices.Sorted(slices.Values(counts))
	if sorted[0] == sorted[len(sorted)-1] {
		return strconv.Itoa(sorted[0])
	}
	return fmt.Sprintf("%d (%d to %d)", sorted[len(sorted)/2], sorted[0], sorted[len(sorted)-1])
}

func conditions(judged []judgedReason) string {
	var seen []string
	var latencies []int64
	spent := 0.0
	for _, j := range judged {
		for round, row := range j.rounds {
			if j.held(round) {
				continue
			}
			said := fmt.Sprintf("model %s build %s over %s with an API key, open_at in force %.2f", row.Model, row.Build, row.Provider, row.Reason.Threshold)
			if !slices.Contains(seen, said) {
				seen = append(seen, said)
			}
			latencies = append(latencies, row.LatencyMS)
			if row.Charge != nil {
				spent += row.Charge.USD
			}
		}
	}
	if len(latencies) == 0 {
		return "no call was answered"
	}
	slices.Sort(latencies)
	at := func(q float64) int64 { return latencies[int(q*float64(len(latencies)-1)+0.5)] }
	return fmt.Sprintf("%s; latency p50 %d ms, p95 %d ms, max %d ms; %.4f USD for %d calls", strings.Join(seen, "; "), at(0.5), at(0.95), at(1), spent, len(latencies))
}
