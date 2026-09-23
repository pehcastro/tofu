package sift

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"tofu/bench/corpus"
	"tofu/internal/sift"
)

const corpusFile = "testdata/shell-corpus.jsonl"

type Row struct {
	Session string `json:"session"`
	Task    string `json:"task"`
	Command string `json:"command"`
	Outcome string `json:"outcome"`
	Output  string `json:"output"`
}

func (r Row) Shell() sift.Shell {
	exit := 0
	if r.Outcome != "ran" {
		exit = 1
	}
	return sift.Shell{Command: r.Command, Stdout: r.Output, ExitCode: exit}
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
		if strings.TrimSpace(scanner.Text()) == "" {
			continue
		}
		var row Row
		if err := json.Unmarshal(scanner.Bytes(), &row); err != nil {
			return nil, fmt.Errorf("%s: row %d: %w", path, len(rows)+1, err)
		}
		if leaks := corpus.LeaksIn(scanner.Text()); len(leaks) > 0 {
			return nil, fmt.Errorf("%s: row %d carries %q, which came off the recording machine: re-extract it through corpus.Scrub", path, len(rows)+1, leaks)
		}
		rows = append(rows, row)
	}
	return rows, scanner.Err()
}

type Planted struct {
	Shell  sift.Shell
	Units  []sift.Unit
	At     int
	Needle string
}

func Plant(row Row, index int) (Planted, error) {
	needle := fmt.Sprintf("needle-%04d: the answer to %q is build 2026-09-20-%04d\n", index, row.Task, index)
	units := sift.SplitShell(row.Shell())
	if len(units) == 0 {
		return Planted{}, fmt.Errorf("%s: %q returned nothing to sieve", row.Session, row.Command)
	}
	chosen := index % len(units)

	var stdout strings.Builder
	for i, unit := range units {
		if i != chosen {
			stdout.WriteString(unit.Text)
			continue
		}
		lines := strings.SplitAfter(unit.Text, "\n")
		at := len(lines) / 2
		stdout.WriteString(strings.Join(lines[:at], ""))
		stdout.WriteString(needle)
		stdout.WriteString(strings.Join(lines[at:], ""))
	}

	result := row.Shell()
	result.Stdout = stdout.String()
	planted := sift.SplitShell(result)
	for _, unit := range planted {
		if strings.Contains(unit.Text, needle) {
			return Planted{Shell: result, Units: planted, At: unit.Index, Needle: strings.TrimRight(needle, "\n")}, nil
		}
	}
	return Planted{}, fmt.Errorf("%s: the needle was planted and no unit carries it", row.Session)
}
