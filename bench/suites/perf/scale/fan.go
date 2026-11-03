package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"
)

type fanSplit struct {
	slowest                                               string
	fan, stagger, model, busy, final, memory, gather, own float64
	reportTurns                                           int
	firstRequestMedian, firstRequestMax                   float64
}

func running(requests []requestDetail, at time.Time) bool {
	return slices.ContainsFunc(requests, func(d requestDetail) bool { return !at.Before(d.At) && at.Before(d.end()) })
}

func (s sessionTimes) fanSplit() fanSplit {
	asked := s.askEnd()
	turnEnd, final := s.turnEnds()
	reports := slices.DeleteFunc(slices.Clone(s.trace.Inserted), func(item tracedInsert) bool { return item.Origin != "sub-agent report" })
	last := slices.MaxFunc(reports, func(a, b tracedInsert) int { return a.TakenAt.Compare(b.TakenAt) })
	named := slices.DeleteFunc(slices.Clone(s.trace.Agents), func(a tracedAgent) bool { return !strings.Contains(last.Text, "sub-agent "+a.Agent+" is ") })
	slowest := slices.MaxFunc(named, func(a, b tracedAgent) int { return a.EndedAt.Compare(b.EndedAt) })
	split := fanSplit{slowest: slowest.Agent, fan: ms(final.Sub(asked)), final: ms(final.Sub(last.TakenAt)), reportTurns: len(turnEnd) - 1}
	var lead, memory []requestDetail
	first := map[string]requestDetail{}
	for _, request := range s.trace.Requests {
		detail := s.details[request.Request]
		if request.Agent == "" {
			if !detail.At.Before(asked) {
				lead = append(lead, detail)
			}
			continue
		}
		if earlier, seen := first[request.Agent]; !seen || detail.At.Before(earlier.At) {
			first[request.Agent] = detail
		}
		if request.Agent == slowest.Agent {
			split.model += request.DurationMS
		}
	}
	for _, request := range s.trace.Memory {
		memory = append(memory, s.details[request.Request])
	}
	notice := s.noticeSteps()[slowest.Agent]["total"]
	split.stagger = ms(slowest.StartedAt.Sub(asked)) - notice
	split.own = notice + ms(slowest.EndedAt.Sub(slowest.StartedAt)) - split.model
	for at := slowest.EndedAt; at.Before(last.TakenAt); at = at.Add(time.Millisecond) {
		switch {
		case running(lead, at):
			split.busy++
		case running(memory, at):
			split.memory++
		default:
			split.gather++
		}
	}
	var firsts []float64
	for _, detail := range first {
		firsts = append(firsts, detail.DurationMS)
	}
	slices.Sort(firsts)
	split.firstRequestMedian, split.firstRequestMax = firsts[len(firsts)/2], firsts[len(firsts)-1]
	return split
}

func traceIn(binary string, projects []string, session string) (sessionTimes, error) {
	for _, project := range projects {
		if times, err := (tofuCLI{binary: binary, project: project, env: os.Environ()}).times(session); err == nil {
			return times, nil
		}
	}
	return sessionTimes{}, fmt.Errorf("session %s is in none of %s", session, strings.Join(projects, ", "))
}

func fan(args []string) error {
	flags := flag.NewFlagSet("fan", flag.ExitOnError)
	binary := flags.String("tofu", "", "the tofu binary that reads the sessions")
	from := flags.String("rows", "", "the wall rows whose live runs are split")
	agents := flags.Int("agents", 10, "the rung whose runs are split")
	_ = flags.Parse(args)
	if *binary == "" || *from == "" || flags.NArg() == 0 {
		return errors.New("fan needs -tofu, -rows and the projects the sessions ran in")
	}
	groups, err := readWallRows(*from)
	if err != nil {
		return err
	}
	fmt.Println("| commit | run | fan | slowest report | model wait: stagger | model wait: its requests | lead: busy on an earlier report turn | lead: final report turn | memory model | gather window | tofu own | report turns | first request median | first request max |")
	fmt.Println("|" + strings.Repeat("---|", 14))
	for _, rows := range groups {
		g := grouped(rows)
		if g.first.Arm != "live" || g.first.Agents != *agents {
			continue
		}
		for _, run := range g.runs {
			times, err := traceIn(*binary, flags.Args(), run.Session)
			if err != nil {
				return err
			}
			s := times.fanSplit()
			fmt.Printf("| %.8s | %d | %.2f | %s | %.2f | %.2f | %.2f | %.2f | %.2f | %.2f | %.2f | %d | %.2f | %.2f |\n", run.Commit, run.Run, s.fan/1000, s.slowest,
				s.stagger/1000, s.model/1000, s.busy/1000, s.final/1000, s.memory/1000, s.gather/1000, s.own/1000, s.reportTurns, s.firstRequestMedian/1000, s.firstRequestMax/1000)
		}
	}
	return nil
}
