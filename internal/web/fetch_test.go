package web_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"tofu/internal/web"
)

func saved(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("reading the saved page: %v", err)
	}
	return body
}

func servePage(t *testing.T, name string) *httptest.Server {
	t.Helper()
	body := saved(t, name)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = writer.Write(body)
	}))
	t.Cleanup(server.Close)
	return server
}

func limits() web.Config { return web.Config{MaxPageBytes: 1 << 20, TimeoutMS: 5000} }

func fetched(t *testing.T, address string) web.Page {
	t.Helper()
	page, err := web.NewClient(limits()).Get(context.Background(), address)
	if err != nil {
		t.Fatalf("fetching %s: %v", address, err)
	}
	return page
}

func TestASavedPageOfEveryShapeComesBackAsUnitsRatherThanMarkup(t *testing.T) {
	for _, page := range []struct {
		file  string
		wants []string
		gone  []string
	}{
		{
			file: "docs.html",
			wants: []string{
				"# Worker pools",
				"## Creating a pool",
				"`runtime.NewPool`",
				"```go",
				"- Start at the number of cores and measure.",
				"Workers | 4 | jobs running at the same time",
				"> Draining is not cancellation",
				"/docs/queues)",
			},
			gone: []string{"Pricing", "Copyright 2026 Acme", "window.acme", "Docs / Worker pools", "Metrics"},
		},
		{
			file: "readme.html",
			wants: []string{
				"# ferry",
				"## Install",
				"go install github.com/ridge/ferry/cmd/ferry@latest",
				"- cron has no record of what a run did",
				"`.ferry/runs.jsonl`",
				"docs/design.md (http://127.0.0.1",
			},
			gone: []string{"Sign in", "Stars 412", "Search or jump to", "Terms"},
		},
		{
			file: "article.html",
			wants: []string{
				"# The queue is not the problem",
				"By Dana Okafor, 14 March 2026",
				"> A number from one run is not a measurement.",
				"1. Look at where time is spent before adding anything.",
				"3. Keep the run that disproved the theory.",
				"claim.sql (https://example.org/repo/blob/main/claim.sql)",
			},
			gone: []string{"Subscribe", "skill issue", "Archive", "written in one sitting", "a latency graph"},
		},
	} {
		t.Run(page.file, func(t *testing.T) {
			server := servePage(t, page.file)
			got := fetched(t, server.URL)
			text := web.Render(got.Units)
			t.Logf("%s: %d bytes of %s became %d units and %d bytes of text\n%s",
				page.file, got.RawBytes, got.ContentType, len(got.Units), len(text), text)

			for _, markup := range []string{"<div", "<p>", "</", "<script", "<a ", "&nbsp;"} {
				if strings.Contains(text, markup) {
					t.Fatalf("the page still carries markup %q", markup)
				}
			}
			for _, want := range page.wants {
				if !strings.Contains(text, want) {
					t.Fatalf("the units do not carry %q", want)
				}
			}
			for _, chrome := range page.gone {
				if strings.Contains(text, chrome) {
					t.Fatalf("the chrome %q survived the conversion", chrome)
				}
			}
			if len(text) >= got.RawBytes {
				t.Fatalf("conversion did not shrink the page: %d bytes in, %d out", got.RawBytes, len(text))
			}
		})
	}
}

func TestACodeBlockSurvivesTheConversionCharacterForCharacter(t *testing.T) {
	server := servePage(t, "docs.html")
	got := fetched(t, server.URL)

	wanted := "```go\npool := runtime.NewPool(runtime.PoolConfig{\n\tWorkers: 8,\n\tQueue:   jobs,\n})\n" +
		"if err := pool.Start(ctx); err != nil {\n\treturn fmt.Errorf(\"starting the pool: %w\", err)\n}\ndefer pool.Drain()\n```"
	var blocks []string
	for _, unit := range got.Units {
		if unit.Kind == web.UnitCode {
			blocks = append(blocks, unit.Text)
		}
	}
	t.Logf("code units: %q", blocks)
	if len(blocks) != 1 {
		t.Fatalf("expected one code unit, got %d", len(blocks))
	}
	if blocks[0] != wanted {
		t.Fatalf("the code block changed:\n got %q\nwant %q", blocks[0], wanted)
	}
}

func TestAPageThatIsNotTextSaysSoAndReturnsNothing(t *testing.T) {
	for _, shape := range []struct {
		name        string
		contentType string
		disposition string
		body        []byte
		why         string
	}{
		{name: "an image", contentType: "image/png", body: []byte("\x89PNG\r\n\x1a\n" + strings.Repeat("\x00\xff", 64)), why: "not a text type"},
		{name: "a binary", contentType: "application/octet-stream", body: []byte("\x7fELF\x02\x01\x01\x00" + strings.Repeat("\x00", 64)), why: "not a text type"},
		{name: "a download", contentType: "text/csv", disposition: `attachment; filename="rows.csv"`, body: []byte("a,b\n1,2\n"), why: "as a download"},
	} {
		t.Run(shape.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.Header().Set("Content-Type", shape.contentType)
				if shape.disposition != "" {
					writer.Header().Set("Content-Disposition", shape.disposition)
				}
				_, _ = writer.Write(shape.body)
			}))
			t.Cleanup(server.Close)

			page, err := web.NewClient(limits()).Get(context.Background(), server.URL)
			t.Logf("%s: %v", shape.name, err)
			if err == nil {
				t.Fatalf("%s came back as a page: %+v", shape.name, page)
			}
			var refusal web.NotTextError
			if !errors.As(err, &refusal) {
				t.Fatalf("%s did not refuse as a not-text page: %T", shape.name, err)
			}
			if len(page.Units) != 0 {
				t.Fatalf("%s returned units anyway: %+v", shape.name, page.Units)
			}
			if !strings.Contains(refusal.Error(), shape.why) {
				t.Fatalf("the refusal does not say why: %q", refusal.Error())
			}
		})
	}
}

func TestTheSameAddressTwiceInOneTurnReachesTheNetworkOnce(t *testing.T) {
	requests := 0
	body := saved(t, "docs.html")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		requests++
		writer.Header().Set("Content-Type", "text/html")
		_, _ = writer.Write(body)
	}))
	t.Cleanup(server.Close)

	client := web.NewClient(limits())
	for _, address := range []string{server.URL + "/docs/pools", server.URL + "/docs/pools#choosing"} {
		if _, err := client.Get(context.Background(), address); err != nil {
			t.Fatalf("fetching %s: %v", address, err)
		}
	}
	t.Logf("the stub server counted %d requests for two fetches", requests)
	if requests != 1 {
		t.Fatalf("the page was fetched %d times", requests)
	}
}

func TestAPageOverTheCeilingIsRefusedRatherThanCut(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/plain")
		_, _ = writer.Write([]byte(strings.Repeat("x", 4096)))
	}))
	t.Cleanup(server.Close)

	_, err := web.NewClient(web.Config{MaxPageBytes: 1024, TimeoutMS: 5000}).Get(context.Background(), server.URL)
	t.Logf("over the ceiling: %v", err)
	var over web.TooLargeError
	if !errors.As(err, &over) || over.Cap != 1024 {
		t.Fatalf("a page over the ceiling was not refused: %v", err)
	}
}

func TestAnAddressThatIsNotHTTPAndAFailingStatusBothSayWhat(t *testing.T) {
	_, err := web.NewClient(limits()).Get(context.Background(), "file:///etc/passwd")
	t.Logf("a file url: %v", err)
	if err == nil || !strings.Contains(err.Error(), "http or https") {
		t.Fatalf("a file url was not refused: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		http.Error(writer, "gone", http.StatusNotFound)
	}))
	t.Cleanup(server.Close)
	_, err = web.NewClient(limits()).Get(context.Background(), server.URL)
	t.Logf("a 404: %v", err)
	var status web.StatusError
	if !errors.As(err, &status) || status.Status != http.StatusNotFound {
		t.Fatalf("a 404 was not reported as a status: %v", err)
	}
}
