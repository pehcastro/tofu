package main

import (
	_ "embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"tofu/internal/agent/context/recall"
	"tofu/internal/agent/loop"
	"tofu/internal/judge/ledger"
	"tofu/internal/konst"
	"tofu/internal/models/llm/core"
	platformproject "tofu/internal/platform/project"
)

//go:embed testdata/corpus.json
var corpusJSON []byte

type mockedStep struct {
	Say    string          `json:"say"`
	Tool   string          `json:"tool"`
	Args   json.RawMessage `json:"args"`
	Result string          `json:"result"`
	Bytes  int             `json:"bytes"`
	Needed bool            `json:"needed"`
}

type mockedSession struct {
	Name   string       `json:"name"`
	Task   string       `json:"task"`
	Latest string       `json:"latest"`
	Steps  []mockedStep `json:"steps"`
}

type tally struct {
	results, needed, dropped, refetches, spentKept, asked int
	before, after                                         int
	cost                                                  float64
	latencies                                             []int64
	decidedBy                                             string
	lines                                                 []string
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() (err error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	bound := flag.String("home", home, "the home whose bound classifier the jev arm asks")
	flag.Parse()
	var corpus []mockedSession
	if err := json.Unmarshal(corpusJSON, &corpus); err != nil {
		return err
	}
	scratch, err := os.MkdirTemp("", "tofu-keep-bench-")
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, os.RemoveAll(scratch)) }()
	fresh := filepath.Join(scratch, "fresh-home")
	if err := os.MkdirAll(fresh, 0o755); err != nil {
		return err
	}
	arms := []struct{ name, home string }{{"age", fresh}, {"jev", *bound}}
	tallies := make([]tally, len(arms))
	for i, arm := range arms {
		if tallies[i], err = runArm(filepath.Join(scratch, arm.name), arm.home, corpus); err != nil {
			return fmt.Errorf("arm %s: %w", arm.name, err)
		}
	}
	fmt.Printf("keep_result against the age rule, %s, %d mocked sessions through the /compact engine, turn.CompactCarried at target 0; each session is under the %d token warm suffix, so every result before the newest step is a candidate\n",
		time.Now().Format("2006-01-02 15:04"), len(corpus), konst.TrimWarmSuffixTokens)
	fmt.Printf("a re-fetch is a result the session needs later that compaction dropped to a handle\n\n")
	fmt.Printf("%-5s %8s %7s %8s %10s %11s %14s %13s %6s %10s %8s\n", "arm", "results", "needed", "dropped", "re-fetches", "spent kept", "tokens before", "tokens after", "asked", "cost USD", "p50 ms")
	for i, arm := range arms {
		t := tallies[i]
		fmt.Printf("%-5s %8d %7d %8d %10d %11d %14d %13d %6d %10.6f %8d\n", arm.name, t.results, t.needed, t.dropped, t.refetches, t.spentKept, t.before, t.after, t.asked, t.cost, median(t.latencies))
	}
	for i, arm := range arms {
		fmt.Printf("\n%s arm: %s\n%s\n", arm.name, tallies[i].decidedBy, strings.Join(tallies[i].lines, "\n"))
	}
	return nil
}

func runArm(dir, home string, corpus []mockedSession) (tally, error) {
	for _, name := range []string{"HOME", "USERPROFILE"} {
		if err := os.Setenv(name, home); err != nil {
			return tally{}, err
		}
	}
	places := platformproject.Places{State: filepath.Join(dir, "state"), Log: filepath.Join(dir, "log")}
	project := filepath.Join(dir, "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		return tally{}, err
	}
	var t tally
	for _, session := range corpus {
		compacted, err := loop.CompactCarried(project, places, "", session.Task, "", conversationOf(session))
		if err != nil {
			return tally{}, fmt.Errorf("%s: %w", session.Name, err)
		}
		if compacted.Kept != 0 {
			return tally{}, fmt.Errorf("%s keeps a warm head of %d messages, so some results were never candidates", session.Name, compacted.Kept)
		}
		t.before += compacted.TokensBefore
		t.after += compacted.TokensAfter
		for i, step := range session.Steps {
			dropped := recall.AlreadyDropped(compacted.Messages[2+2*i].Content)
			t.results++
			switch {
			case step.Needed:
				t.needed++
			case !dropped:
				t.spentKept++
			}
			if dropped {
				t.dropped++
			}
			if dropped && step.Needed {
				t.refetches++
			}
			t.lines = append(t.lines, fmt.Sprintf("  %-15s %-6s %-40.40s needed %-5v dropped %v", session.Name, step.Tool, string(step.Args), step.Needed, dropped))
		}
	}
	_, err := ledger.NewReader(places.Log).Each(ledger.Filter{Point: "keep_result"}, func(row ledger.Row) error {
		if len(row.Answers) > 0 {
			t.asked++
			t.cost += row.Cost
			t.latencies = append(t.latencies, row.LatencyMS)
		}
		if row.Reason != nil && row.Reason.ModeReason != nil && t.decidedBy == "" {
			t.decidedBy = *row.Reason.ModeReason
		}
		return nil
	})
	if t.decidedBy == "" {
		t.decidedBy = "jev answered every row"
	}
	return t, err
}

func conversationOf(session mockedSession) []core.Message {
	messages := []core.Message{{Role: core.RoleUser, Content: session.Task}}
	for i, step := range session.Steps {
		id := fmt.Sprintf("%s-%d", session.Name, i)
		result := step.Result
		for len(result) < step.Bytes {
			result += step.Result
		}
		messages = append(messages,
			core.Message{Role: core.RoleAssistant, Content: step.Say, ToolCalls: []core.ToolCall{{ID: id, Name: step.Tool, Arguments: step.Args}}},
			core.Message{Role: core.RoleTool, ToolCallID: id, Content: result[:step.Bytes]})
	}
	return append(messages, core.Message{Role: core.RoleAssistant, Content: session.Latest})
}

func median(values []int64) int64 {
	if len(values) == 0 {
		return 0
	}
	sorted := slices.Sorted(slices.Values(values))
	return sorted[len(sorted)/2]
}
