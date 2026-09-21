package readworth

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"tofu/bench/corpus"
)

const corpusFile = "testdata/readworth-corpus.jsonl"

type Row struct {
	Turn        string `json:"turn"`
	Task        string `json:"task"`
	Position    string `json:"position"`
	AlreadySaid string `json:"already_said"`
	Paragraph   string `json:"paragraph"`
	Keep        bool   `json:"keep"`
}

func ReadCorpus() ([]Row, error) {
	return readCorpusFile(corpusFile)
}

func readCorpusFile(path string) ([]Row, error) {
	file, err := os.Open(path)
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
			return nil, fmt.Errorf("%s: row %d: %w", path, len(rows)+1, err)
		}
		if leaks := corpus.LeaksIn(line); len(leaks) > 0 {
			return nil, fmt.Errorf("%s: row %d carries %q, which came off the recording machine: re-extract it through corpus.Scrub", path, len(rows)+1, leaks)
		}
		rows = append(rows, row)
	}
	return rows, scanner.Err()
}
