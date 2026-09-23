package sift

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	benchcorpus "tofu/bench/corpus"
	"tofu/internal/judge/ledger"
	"tofu/internal/sift"
)

type LedgerRead struct {
	Files            int
	Rows             int
	Millis           []float64
	Costs            []float64
	PointsWithCalls  map[string]int
	SkippedNoPoint   int
	SkippedNoWire    int
	ShellSiftDecided int
	ShellSiftMillis  []float64
	ShellSiftCosts   []float64
	ShellSiftNeeded  []float64
}

func ReadLedger(dir string) (LedgerRead, error) {
	names, err := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	if err != nil {
		return LedgerRead{}, err
	}
	read := LedgerRead{Files: len(names), PointsWithCalls: map[string]int{}}
	for _, name := range names {
		raw, err := os.ReadFile(name)
		if err != nil {
			return LedgerRead{}, err
		}
		for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
			if strings.TrimSpace(line) == "" {
				continue
			}
			read.Rows++
			var row ledger.Row
			if err := json.Unmarshal([]byte(line), &row); err != nil {
				return LedgerRead{}, fmt.Errorf("%s: row %d: %w", name, read.Rows, err)
			}
			if row.Point == "" {
				read.SkippedNoPoint++
				continue
			}
			shellSift := strings.HasPrefix(row.Point, "shell_sift")
			if shellSift {
				read.ShellSiftDecided++
			}
			if row.LatencyMS <= 0 || row.Cost <= 0 {
				read.SkippedNoWire++
				continue
			}
			read.Millis = append(read.Millis, float64(row.LatencyMS))
			read.Costs = append(read.Costs, row.Cost)
			read.PointsWithCalls[row.Point]++
			if shellSift {
				read.ShellSiftMillis = append(read.ShellSiftMillis, float64(row.LatencyMS))
				read.ShellSiftCosts = append(read.ShellSiftCosts, row.Cost)
				for _, answer := range row.Answers {
					if answer.Question == sift.NeededQuestion {
						read.ShellSiftNeeded = append(read.ShellSiftNeeded, answer.Noul)
					}
				}
			}
		}
	}
	return read, nil
}

type SessionShells struct {
	Shells     int
	ShellBytes float64
	ToolBytes  float64
	ShellReads float64
	ToolReads  float64
}

type SessionRead struct {
	Sessions []SessionShells
	Skipped  []benchcorpus.SkippedTurn
	Totals   SessionShells
}

func ReadSessionShells(dir string) (SessionRead, error) {
	walked, err := benchcorpus.WalkSessions(dir)
	if err != nil {
		return SessionRead{}, err
	}
	read := SessionRead{Skipped: walked.Skipped}
	for _, turn := range walked.Turns {
		var session SessionShells
		for index, step := range turn.Steps {
			resends := float64(len(turn.Steps) - index)
			for _, call := range step.ToolCalls {
				rendered := float64(call.RenderedBytes)
				session.ToolBytes += rendered
				session.ToolReads += rendered * resends
				if call.Tool != "bash" {
					continue
				}
				session.Shells++
				session.ShellBytes += rendered
				session.ShellReads += rendered * resends
			}
		}
		read.Sessions = append(read.Sessions, session)
		read.Totals.Shells += session.Shells
		read.Totals.ShellBytes += session.ShellBytes
		read.Totals.ToolBytes += session.ToolBytes
		read.Totals.ShellReads += session.ShellReads
		read.Totals.ToolReads += session.ToolReads
	}
	return read, nil
}
