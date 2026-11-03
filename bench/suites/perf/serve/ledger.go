package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"tofu/internal/host"
	"tofu/internal/judge/ledger"
)

const (
	besideAsked   = "query.ledger"
	besideSmall   = "query.drain"
	besideLast    = 10000
	statePadBytes = 3000
)

func besideDelays() []time.Duration {
	return []time.Duration{50 * time.Millisecond, 300 * time.Millisecond}
}

func besideNames(delay time.Duration) (asked, small string) {
	return fmt.Sprintf("%s, drain %s in", besideAsked, delay), fmt.Sprintf("%s %s into it", besideSmall, delay)
}

func writeLedger(root string, rows int) error {
	projects, err := filepath.Glob(filepath.Join(root, "home", ".tofu", "projects", "*"))
	if err != nil {
		return err
	}
	if len(projects) != 1 {
		return fmt.Errorf("the bench home holds %d project state folders, not one: %v", len(projects), projects)
	}
	writer := ledger.NewWriter(filepath.Join(projects[0], "log"))
	started := time.Now().Add(-time.Duration(rows) * time.Millisecond)
	pad := strings.Repeat("synthetic bench context ", statePadBytes/len("synthetic bench context "))
	noul := func(question string, p float64) ledger.Answer {
		return ledger.Answer{Question: question, Wording: 1, Kind: ledger.AnswerNoul, Noul: p}
	}
	for i := range rows {
		state, err := json.Marshal(map[string]any{"tool": "bash", "input": map[string]any{"command": "go test ./pkg" + strconv.Itoa(i%40) + "/..."}, "context": pad})
		if err != nil {
			return err
		}
		row := ledger.Row{At: started.Add(time.Duration(i) * time.Millisecond), Point: "tool_gate", Questions: "tool_gate@1", Version: 1, Build: "bench-build", Model: "bench/model",
			StateHash: strconv.Itoa(i), Fingerprint: "fp" + strconv.Itoa(i%50), StateBuilder: "bench@1", State: state, Verdict: ledger.VerdictAllow,
			Answers: []ledger.Answer{noul("destructive", float64(i%7)/10), noul("user_requested", float64(i%5)/10), noul("flagged_untrusted_content", float64(i%3)/10)},
			Reason:  &ledger.Reason{Question: "destructive", Comparison: "<", Threshold: 0.5, Value: float64(i%7) / 10, Mode: ledger.ModeShadow}, RequestID: "bench-" + strconv.Itoa(i)}
		if _, err := writer.Append(row); err != nil {
			return err
		}
	}
	return nil
}

func (a *arm) beside(s *served, session string, delay time.Duration) error {
	asked, askedAt, err := s.send(besideAsked, host.LedgerParams{Last: besideLast})
	if err != nil {
		return err
	}
	time.Sleep(delay)
	small, smallAt, err := s.send(besideSmall, host.SessionParams{Session: session})
	if err != nil {
		return err
	}
	pending := map[string]int64{asked: askedAt, small: smallAt}
	names := map[string]string{}
	names[asked], names[small] = besideNames(delay)
	deadline := time.After(callLimit)
	for len(pending) > 0 {
		line, err := s.await(deadline, func(line wireLine) bool { _, waited := pending[string(line.ID)]; return line.Method == "" && waited })
		if err != nil {
			return fmt.Errorf("%s beside %s: %w", besideSmall, besideAsked, err)
		}
		id := string(line.ID)
		took := s.clock.since(pending[id])
		delete(pending, id)
		if line.Error != nil {
			return errors.New(names[id] + ": " + string(line.Error))
		}
		a.samples[names[id]] = append(a.samples[names[id]], took)
		a.sizes[names[id]] = line.size
	}
	return nil
}
