package shortlist

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"tofu/bench/corpus"
)

const corpusFile = "testdata/shortlist-corpus.jsonl"

type File struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

type Row struct {
	TurnID string   `json:"turn_id"`
	Task   string   `json:"task"`
	Files  []File   `json:"files"`
	Label  []string `json:"label"`
}

func (r Row) Hit(path string) bool {
	for _, want := range r.Label {
		if want == path {
			return true
		}
	}
	return false
}

func ReadCorpus() ([]Row, error) {
	file, err := os.Open(corpusFile)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()

	var rows []Row
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}
		var row Row
		if err := json.Unmarshal(scanner.Bytes(), &row); err != nil {
			return nil, fmt.Errorf("%s: row %d: %w", corpusFile, len(rows)+1, err)
		}
		if leaks := corpus.LeaksIn(line); len(leaks) > 0 {
			return nil, fmt.Errorf("%s: row %d carries %q, which came off the recording machine: re-extract it through corpus.Scrub", corpusFile, len(rows)+1, leaks)
		}
		rows = append(rows, row)
	}
	return rows, scanner.Err()
}
