package harness

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
)

type ChecklistCheckStatus string

const (
	ChecklistCheckPassed           ChecklistCheckStatus = "pass"
	ChecklistCheckFailed           ChecklistCheckStatus = "fail"
	ChecklistCheckCouldNotEvaluate ChecklistCheckStatus = "could_not_evaluate"
	ChecklistCheckRecorded         ChecklistCheckStatus = "recorded"
)

type ChecklistCheck struct {
	Item   int
	Status ChecklistCheckStatus
	Reason string
}

func (c ChecklistCheck) ToResult() ChecklistResult {
	return ChecklistResult{
		Item:   fmt.Sprintf("item %d", c.Item),
		Passed: c.Status == ChecklistCheckPassed,
	}
}

func ChecklistResults(checks []ChecklistCheck) []ChecklistResult {
	results := make([]ChecklistResult, 0, len(checks))
	for _, c := range checks {
		results = append(results, c.ToResult())
	}
	return results
}

func RunChecklist(bunBin, checkerPath, armDir string) ([]ChecklistCheck, error) {
	cmd := exec.Command(bunBin, checkerPath, armDir)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return nil, fmt.Errorf("%s exited nonzero: %s", checkerPath, stderr.String())
		}
		return nil, fmt.Errorf("%s did not run: %w", checkerPath, err)
	}
	return parseChecklistOutput(stdout.Bytes())
}

type checklistLine struct {
	Item   json.RawMessage `json:"item"`
	Status string          `json:"status"`
	Reason string          `json:"reason"`
}

func parseChecklistOutput(out []byte) ([]ChecklistCheck, error) {
	var checks []ChecklistCheck
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var parsed checklistLine
		if err := json.Unmarshal(line, &parsed); err != nil {
			return nil, fmt.Errorf("checker line %q: %w", line, err)
		}
		var itemNumber int
		if err := json.Unmarshal(parsed.Item, &itemNumber); err != nil {
			continue
		}
		checks = append(checks, ChecklistCheck{
			Item:   itemNumber,
			Status: ChecklistCheckStatus(parsed.Status),
			Reason: parsed.Reason,
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading checker output: %w", err)
	}
	return checks, nil
}
