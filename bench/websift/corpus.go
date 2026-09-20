package websift

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"

	"tofu/bench/corpus"
	"tofu/internal/sift"
	"tofu/internal/web"
)

const corpusFile = "testdata/page-corpus.jsonl"

const (
	GroupFocused   = "focused"
	GroupReference = "reference"
)

type Row struct {
	Source      string `json:"source"`
	Group       string `json:"group"`
	URL         string `json:"url"`
	Task        string `json:"task"`
	ContentType string `json:"content_type"`
	Body        string `json:"body"`
}

func ReadCorpus() ([]Row, error) {
	file, err := os.Open(corpusFile)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	var rows []Row
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for scanner.Scan() {
		if strings.TrimSpace(scanner.Text()) == "" {
			continue
		}
		var row Row
		if err := json.Unmarshal(scanner.Bytes(), &row); err != nil {
			return nil, fmt.Errorf("%s: row %d: %w", corpusFile, len(rows)+1, err)
		}
		if leaks := corpus.LeaksIn(scanner.Text()); len(leaks) > 0 {
			return nil, fmt.Errorf("%s: row %d carries %q, which came off the recording machine: re-extract it through corpus.Scrub", corpusFile, len(rows)+1, leaks)
		}
		rows = append(rows, row)
	}
	return rows, scanner.Err()
}

type Planted struct {
	URL   string
	Units []sift.PageUnit
	At    int
}

func Plant(row Row, index int) (Planted, error) {
	base, err := url.Parse(row.URL)
	if err != nil {
		return Planted{}, fmt.Errorf("%s: %q is not a URL: %w", row.Source, row.URL, err)
	}
	units := web.Units(row.ContentType, []byte(row.Body), base)
	if len(units) == 0 {
		return Planted{}, fmt.Errorf("%s: %q returned nothing to sieve", row.Source, row.URL)
	}

	var candidates []int
	for i, unit := range units {
		if unit.Kind == web.UnitParagraph {
			candidates = append(candidates, i)
		}
	}
	if len(candidates) == 0 {
		for i := range units {
			candidates = append(candidates, i)
		}
	}
	chosen := candidates[index%len(candidates)]

	needle := fmt.Sprintf("Needle-%04d: the answer to %q is build 2026-09-20-%04d.", index, row.Task, index)
	units[chosen].Text = units[chosen].Text + " " + needle

	planted := sift.SplitPage(units)
	return Planted{URL: row.URL, Units: planted, At: chosen}, nil
}
