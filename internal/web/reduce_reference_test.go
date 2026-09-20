package web_test

import (
	"bufio"
	"encoding/json"
	"net/url"
	"os"
	"strings"
	"testing"

	"tofu/internal/web"
)

const referenceCorpus = "../../bench/websift/testdata/page-corpus.jsonl"

type referenceRow struct {
	Source      string `json:"source"`
	Group       string `json:"group"`
	URL         string `json:"url"`
	ContentType string `json:"content_type"`
	Body        string `json:"body"`
}

func referenceRows(t *testing.T) []referenceRow {
	t.Helper()
	file, err := os.Open(referenceCorpus)
	if err != nil {
		t.Fatalf("opening %s: %v", referenceCorpus, err)
	}
	defer func() { _ = file.Close() }()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	var rows []referenceRow
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var row referenceRow
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			t.Fatalf("%s: %v", referenceCorpus, err)
		}
		if row.Group == "reference" {
			rows = append(rows, row)
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("reading %s: %v", referenceCorpus, err)
	}
	if len(rows) != 3 {
		t.Fatalf("expected 3 reference pages, found %d", len(rows))
	}
	return rows
}

const needle = "Needle-0001: the answer is build 2026-09-20-0001."

func plantNeedle(units []web.Unit) int {
	for i, unit := range units {
		if unit.Kind == web.UnitParagraph {
			units[i].Text += " " + needle
			return i
		}
	}
	units[0].Text += " " + needle
	return 0
}

func TestTheReadabilityRuleOverTheThreeReferencePages(t *testing.T) {
	totalBefore, totalAfter := 0, 0
	for _, row := range referenceRows(t) {
		base, err := url.Parse(row.URL)
		if err != nil {
			t.Fatalf("%s: %v", row.URL, err)
		}
		units := web.Units(row.ContentType, []byte(row.Body), base)
		if len(units) == 0 {
			t.Fatalf("%s: no units", row.Source)
		}
		at := plantNeedle(units)

		before := web.Render(units)
		kept, cut := web.Reduce(units)
		after := web.Render(kept)

		t.Logf("%-16s %8d -> %8d bytes, %5.1f%% saved, dropped %d units %d bytes",
			row.Source, len(before), len(after), 100*float64(len(before)-len(after))/float64(len(before)), cut.Units, cut.Bytes)

		if !strings.Contains(after, needle) {
			t.Fatalf("%s: the needle planted at unit %d did not survive", row.Source, at)
		}
		if row.Source == "nodejs-fs" {
			logDroppedSample(t, units)
		}

		totalBefore += len(before)
		totalAfter += len(after)
	}

	saved := 100 * float64(totalBefore-totalAfter) / float64(totalBefore)
	t.Logf("all three: %d -> %d bytes, %.1f%% saved, TOFU-236 measured 9.4%%", totalBefore, totalAfter, saved)
	if totalAfter >= totalBefore {
		t.Fatalf("the rule removed nothing across the three reference pages: %d -> %d", totalBefore, totalAfter)
	}
}

func logDroppedSample(t *testing.T, units []web.Unit) {
	t.Helper()
	shown := 0
	for i := 1; i < len(units) && shown < 3; i++ {
		_, cut := web.Reduce([]web.Unit{units[0], units[i]})
		if cut.Units == 0 {
			continue
		}
		t.Logf("dropped unit %d, next to it: %q\ngone: %q", i, units[i-1].Text, units[i].Text)
		shown++
	}
}
