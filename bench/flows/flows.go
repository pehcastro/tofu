package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const (
	crowd         = 450
	searchedOf    = 317
	clockReadings = 3
)

type flow struct {
	number int
	name   string
	seamed bool
	run    func(session) (string, error)
}

var flows = []flow{
	{1, "lead spawns two, both report, the lead answers", false, twoReport},
	{2, "kill mid sub-agents, reopen, message the errored ones, their reports start the lead", false, killAndResume},
	{3, "a report finished before a kill reaches the lead once after reopen", false, reportOnce},
	{4, "a panic in a sub-agent ends only it and the lead goes on", true, panicEndsOne},
	{5, "a sub-agent held 70 s inside one call shows its clock moving", false, heldClock},
	{6, "a sub-agent waiting on the lead past a shortened cap keeps running", true, capSkipsWaits},
	{7, "450 settled sub-agents fold and one opens by search", true, foldAndSearch},
	{8, "10,000 kept shells: memory under 300 MB for 3 min, a report reaches the lead, ctrl+c quits within 3 s", false, keptShellsQuit},
}

var clockOnScreen = regexp.MustCompile(`(?m)(?:spawning|running) \[&sub-1\][^\n]*?  ((?:\d+h )?(?:\d+m )?\d+s)\s*[│┃]?\s*$`)

func carries(r request, marks ...string) error {
	for _, mark := range marks {
		if !strings.Contains(r.text(), mark) {
			return fmt.Errorf("%s does not carry %s", r.name(), mark)
		}
	}
	return nil
}

func lastLead(out string) (request, error) {
	sent := leadRequests(out)
	if len(sent) == 0 {
		return request{}, fmt.Errorf("the orchestrator sent no request")
	}
	return sent[len(sent)-1], nil
}

func driveToLastLead(s session, script string, timeout time.Duration) (request, error) {
	out, err := s.drive(script, "", timeout)
	if err != nil {
		return request{}, err
	}
	return lastLead(out)
}

func twoReport(s session) (string, error) {
	lead, err := driveToLastLead(s, "1-two-report", time.Minute)
	if err != nil {
		return "", err
	}
	if err := carries(lead, "FIRSTREPORT", "SECONDREPORT"); err != nil {
		return "", err
	}
	return lead.name() + " carries both reports and the lead answered", nil
}

func killAndResume(s session) (string, error) {
	if _, err := s.driveUntilKilled("2-kill", "sub-agent c1 request 2", time.Minute); err != nil {
		return "", err
	}
	out, err := s.drive("2-reopen", "", time.Minute, "--continue")
	if err != nil {
		return "", err
	}
	errored := lineWith(out, "failed · route worker")
	if errored == "" || lineWith(out, "failed · model worker") == "" {
		return "", fmt.Errorf("the reopened sub-agents screen does not show both workers failed")
	}
	sent := leadRequests(out)
	if len(sent) == 0 || !strings.Contains(sent[0].text(), "sub-1 is errored") || !strings.Contains(sent[0].text(), "sub-2 is errored") {
		return "", fmt.Errorf("the lead's first request after reopen does not say sub-1 and sub-2 are errored")
	}
	var startedOn []string
	reached := 0
	for _, lead := range sent {
		if carried := strings.Count(lead.last(), "RESUMEDREPORT"); carried > 0 {
			reached += carried
			startedOn = append(startedOn, lead.name())
		}
	}
	if reached != 2 {
		last := sent[len(sent)-1]
		return "", fmt.Errorf("%d of 2 restored reports started a lead turn; %s ends on %q", reached, last.name(), strings.SplitN(last.last(), "\n", 2)[0])
	}
	return "screen: " + errored + "; " + strings.Join(startedOn, " and ") + " start on the two restored reports", nil
}

func reportOnce(s session) (string, error) {
	before, err := s.driveUntilKilled("3-kill", "the orchestrator request 2", time.Minute)
	if err != nil {
		return "", err
	}
	if strings.Contains(before, "the orchestrator request 3") {
		return "", fmt.Errorf("setup: the lead read the second report before the kill, so the flow proves nothing")
	}
	out, err := s.drive("3-reopen", "", time.Minute, "--continue")
	if err != nil {
		return "", err
	}
	lead, err := lastLead(out)
	if err != nil {
		return "", err
	}
	for _, mark := range []string{"READONCE", "UNREADREPORT"} {
		if seen := strings.Count(lead.text(), mark); seen != 1 {
			return "", fmt.Errorf("%s carries %s %d times: %s", lead.name(), mark, seen, lineWith(lead.text(), mark))
		}
	}
	return lead.name() + " carries READONCE once and UNREADREPORT once", nil
}

func panicEndsOne(s session) (string, error) {
	lead, err := driveToLastLead(s, "4-panic", time.Minute)
	if err != nil {
		return "", err
	}
	if err := carries(lead, "SIBLINGREPORT", "is errored: panic: "+panicMark); err != nil {
		return "", err
	}
	return lead.name() + ": " + lineWith(lead.text(), "is errored: panic"), nil
}

func heldClock(s session) (string, error) {
	out, err := s.drive("5-held", "", 2*time.Minute)
	if err != nil {
		return "", err
	}
	var read []string
	previous := time.Duration(-1)
	for _, found := range clockOnScreen.FindAllStringSubmatch(out, -1) {
		clock, err := time.ParseDuration(strings.ReplaceAll(found[1], " ", ""))
		if err != nil {
			return "", err
		}
		read = append(read, found[1])
		if clock <= previous {
			return "", fmt.Errorf("sub-1's clock did not move between screens: %s", strings.Join(read, ", "))
		}
		previous = clock
	}
	if len(read) != clockReadings {
		return "", fmt.Errorf("sub-1's clock was read on %d of %d screens: %v", len(read), clockReadings, read)
	}
	return "sub-1 read " + strings.Join(read, ", ") + " at session 20s, 45s, 1m 10s", nil
}

func capSkipsWaits(s session) (string, error) {
	lead, err := driveToLastLead(s, "6-cap", 90*time.Second)
	if err != nil {
		return "", err
	}
	if retired := lineWith(lead.text(), "wall clock cap"); retired != "" {
		return "", fmt.Errorf("%s: %s", lead.name(), retired)
	}
	if err := carries(lead, "CAPREPORT"); err != nil {
		return "", err
	}
	return lead.name() + " carries CAPREPORT, the sub-agent ran past its 5 s cap after 8 s waiting on the lead", nil
}

func foldAndSearch(s session) (string, error) {
	if err := s.setting("subAgentsPerTurn", fmt.Sprint(crowd)); err != nil {
		return "", err
	}
	cassette, err := crowdCassette(s.dir)
	if err != nil {
		return "", err
	}
	out, err := s.drive("7-fold", cassette, 5*time.Minute)
	if err != nil {
		return "", err
	}
	folded := lineWith(out, fmt.Sprintf("%d folded", crowd))
	if folded == "" {
		return "", fmt.Errorf("no screen says %d folded", crowd)
	}
	opened := lineWith(out, fmt.Sprintf("sub-agent sub-%d is finished", searchedOf))
	if opened == "" {
		return "", fmt.Errorf("the search for mission%d opened no report of sub-%d", searchedOf, searchedOf)
	}
	return folded + "; search opened: " + opened, nil
}

func crowdCassette(dir string) (string, error) {
	var spawns []map[string]any
	for n := 1; n <= crowd; n++ {
		spawns = append(spawns, map[string]any{"name": "spawn", "args": map[string]any{"task": fmt.Sprintf("mission%03d: read the note", n), "owns": []string{fmt.Sprintf("m%03d/**", n)}}})
	}
	replies := []any{
		map[string]any{"text": "spawning the crowd", "tools": spawns},
		map[string]any{"agent": fmt.Sprintf("c%d", crowd+1), "text": "the forked lead waits on the crowd"},
	}
	for n := 1; n <= crowd; n++ {
		replies = append(replies, map[string]any{"agent": fmt.Sprintf("c%d", n), "text": fmt.Sprintf("crowd report %d", n)})
	}
	var body bytes.Buffer
	encoder := json.NewEncoder(&body)
	for _, reply := range replies {
		if err := encoder.Encode(reply); err != nil {
			return "", err
		}
	}
	path := filepath.Join(dir, "7-fold.jsonl")
	return path, os.WriteFile(path, body.Bytes(), 0o644)
}
