package sift_test

import (
	"bufio"
	"encoding/json"
	"net/url"
	"os"
	"sort"
	"testing"

	"tofu/internal/sift"
	"tofu/internal/web"
)

const recordedPages = "../../bench/websift/testdata/page-corpus.jsonl"

type recordedPage struct {
	Source      string `json:"source"`
	URL         string `json:"url"`
	ContentType string `json:"content_type"`
	Body        string `json:"body"`
}

func TestThePageSiftRemovesNothingWebReduceHasNotAlreadyRemoved(t *testing.T) {
	file, err := os.Open(recordedPages)
	if err != nil {
		t.Skipf("%s is not on disk, so there is nothing to measure: %v", recordedPages, err)
	}
	defer func() { _ = file.Close() }()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)

	var perPage []float64
	extractedTotal, reducedTotal, elidedTotal := 0, 0, 0
	for scanner.Scan() {
		if len(scanner.Bytes()) == 0 {
			continue
		}
		var page recordedPage
		if err := json.Unmarshal(scanner.Bytes(), &page); err != nil {
			t.Fatalf("%s: %v", recordedPages, err)
		}
		base, err := url.Parse(page.URL)
		if err != nil {
			t.Fatalf("%s: %q is not a URL: %v", page.Source, page.URL, err)
		}
		units := web.Units(page.ContentType, []byte(page.Body), base)
		reduced, _ := web.Reduce(units)
		extracted, left := len(web.Render(units)), len(web.Render(reduced))

		elidedUnits, elidedBytes := 0, 0
		for _, unit := range sift.SplitPage(reduced) {
			if mark := sift.PageCheap(unit); !mark.Keep {
				elidedUnits++
				elidedBytes += len(unit.Text)
			}
		}
		if elidedBytes != 0 {
			t.Errorf("%s: the page sift still elides %d units and %d bytes after web.Reduce, so web.Reduce is no longer applying web.LinkOnly to every unit it keeps",
				page.Source, elidedUnits, elidedBytes)
		}

		perPage = append(perPage, percentSaved(extracted, left))
		extractedTotal += extracted
		reducedTotal += left
		elidedTotal += elidedBytes
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("%s: %v", recordedPages, err)
	}
	if len(perPage) == 0 {
		t.Fatalf("%s holds no page, so this test proves nothing", recordedPages)
	}

	sort.Float64s(perPage)
	t.Logf("%d recorded pages: web.Reduce takes %d extracted bytes to %d, %.2f%% of the corpus, per page min %.2f%% median %.2f%% max %.2f%%",
		len(perPage), extractedTotal, reducedTotal, percentSaved(extractedTotal, reducedTotal),
		perPage[0], perPage[len(perPage)/2], perPage[len(perPage)-1])
	t.Logf("the page sift elides %d further bytes, %.2f%% of what web.Reduce leaves", elidedTotal, percentSaved(reducedTotal, reducedTotal-elidedTotal))
}

func percentSaved(before, after int) float64 {
	if before == 0 {
		return 0
	}
	return float64(before-after) / float64(before) * 100
}
