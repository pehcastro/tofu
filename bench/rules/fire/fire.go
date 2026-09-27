package fire

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"time"
)

type Fire struct {
	RuleID   string    `json:"rule_id"`
	Target   string    `json:"target"`
	Mode     string    `json:"mode"`
	Blocked  bool      `json:"blocked"`
	Findings int       `json:"findings"`
	At       time.Time `json:"at"`
}

func ReadDir(dir string) ([]Fire, int, error) {
	names, err := filepath.Glob(filepath.Join(dir, "*.rules.jsonl"))
	if err != nil {
		return nil, 0, err
	}
	sort.Strings(names)
	var fires []Fire
	unreadable := 0
	for _, name := range names {
		read, bad, err := readFile(name)
		if err != nil {
			return nil, 0, err
		}
		fires = append(fires, read...)
		unreadable += bad
	}
	return fires, unreadable, nil
}

func readFile(name string) ([]Fire, int, error) {
	f, err := os.Open(name)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = f.Close() }()
	var fires []Fire
	unreadable := 0
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var one Fire
		if err := json.Unmarshal(line, &one); err != nil {
			unreadable++
			continue
		}
		fires = append(fires, one)
	}
	if err := scanner.Err(); err != nil {
		return nil, 0, err
	}
	return fires, unreadable, nil
}
