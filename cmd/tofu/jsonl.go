package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

const jsonLinesSuffix = ".jsonl"

func jsonlRecords[T any](dir string, wanted func(name string) bool) ([]T, int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, 0, nil
		}
		return nil, 0, err
	}
	var records []T
	unreadable := 0
	for _, entry := range entries {
		if entry.IsDir() || !wanted(entry.Name()) {
			continue
		}
		body, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, 0, err
		}
		for _, line := range strings.Split(string(body), "\n") {
			if strings.TrimSpace(line) == "" {
				continue
			}
			var record T
			if json.Unmarshal([]byte(line), &record) != nil {
				unreadable++
				continue
			}
			records = append(records, record)
		}
	}
	return records, unreadable, nil
}
