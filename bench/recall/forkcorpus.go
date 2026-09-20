package recall

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"tofu/bench/corpus"
)

const forkCorpusFile = "testdata/forks.jsonl"

type ForkCase struct {
	From          string `json:"from"`
	Into          string `json:"into"`
	Step          int    `json:"step"`
	Task          string `json:"task"`
	TokensBefore  int    `json:"tokens_before"`
	TokensAfter   int    `json:"tokens_after"`
	BlockedMicros int64  `json:"blocked_micros"`
	CarryText     string `json:"carry_text"`
	LastWord      string `json:"last_word"`
}

func ReadForkCorpus() ([]ForkCase, error) {
	return readForkCorpusFile(forkCorpusFile)
}

func readForkCorpusFile(path string) ([]ForkCase, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	var cases []ForkCase
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}
		if leaks := corpus.LeaksIn(line); len(leaks) > 0 {
			return nil, fmt.Errorf("%s: row %d carries %q, which came off the recording machine: re-extract it through corpus.Scrub", path, len(cases)+1, leaks)
		}
		var one ForkCase
		if err := json.Unmarshal([]byte(line), &one); err != nil {
			return nil, fmt.Errorf("%s: row %d: %w", path, len(cases)+1, err)
		}
		cases = append(cases, one)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return cases, nil
}
