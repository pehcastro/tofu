package sift

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"tofu/internal/judge/ledger"
)

type DayBuilds map[string]map[string]int

func ReadLedgerBuilds(dir string) (DayBuilds, error) {
	names, err := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	if err != nil {
		return nil, err
	}
	days := DayBuilds{}
	for _, name := range names {
		raw, err := os.ReadFile(name)
		if err != nil {
			return nil, err
		}
		for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
			if strings.TrimSpace(line) == "" {
				continue
			}
			var row ledger.Row
			if err := json.Unmarshal([]byte(line), &row); err != nil {
				return nil, fmt.Errorf("%s: %w", name, err)
			}
			if row.Build == "" {
				continue
			}
			if days[row.Day()] == nil {
				days[row.Day()] = map[string]int{}
			}
			days[row.Day()][row.Build]++
		}
	}
	return days, nil
}
