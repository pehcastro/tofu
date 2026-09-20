package turn

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"tofu/internal/sys"
)

type jsonlLine struct {
	Kind string          `json:"kind"`
	Body json.RawMessage `json:"body"`
}

func ReadRecordedTurnDir(dir string) (RecordedTurn, error) {
	body, err := sys.ReadFile(filepath.Join(dir, "body.jsonl"))
	if err != nil {
		return RecordedTurn{}, err
	}
	var recorded RecordedTurn
	outcomeFound := false
	for _, line := range strings.Split(strings.TrimSpace(string(body)), "\n") {
		if line == "" {
			continue
		}
		var entry jsonlLine
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			return RecordedTurn{}, fmt.Errorf("bench: %s/body.jsonl is not a recorded line: %w", dir, err)
		}
		switch entry.Kind {
		case "step":
			var step RecordedStep
			if err := json.Unmarshal(entry.Body, &step); err != nil {
				return RecordedTurn{}, fmt.Errorf("bench: %s/body.jsonl step is not the expected shape: %w", dir, err)
			}
			recorded.Steps = append(recorded.Steps, step)
		case "outcome":
			if err := json.Unmarshal(entry.Body, &recorded); err != nil {
				return RecordedTurn{}, fmt.Errorf("bench: %s/body.jsonl outcome is not the expected shape: %w", dir, err)
			}
			outcomeFound = true
		}
	}
	if !outcomeFound {
		return RecordedTurn{}, fmt.Errorf("bench: %s/body.jsonl carries no outcome line, so it has no wall clock", dir)
	}
	if len(recorded.Steps) == 0 || recorded.WallClockMS == 0 {
		return RecordedTurn{}, fmt.Errorf("bench: %s carries no step and no wall clock", dir)
	}
	return recorded, nil
}
