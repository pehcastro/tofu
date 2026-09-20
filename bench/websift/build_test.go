package websift

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"tofu/bench/corpus"
)

type source struct {
	id    string
	group string
	url   string
	task  string
}

func corpusSources() []source {
	focused := []source{
		{"mdn-array-map", GroupFocused, "https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Array/map", "does Array.prototype.map mutate the original array"},
		{"redis-set", GroupFocused, "https://redis.io/docs/latest/commands/set/", "does the redis SET command support an expiry option"},
		{"go-blog-errors", GroupFocused, "https://go.dev/blog/error-handling-and-go", "what is the idiomatic way to handle an error in Go"},
		{"vue-intro", GroupFocused, "https://vuejs.org/guide/introduction.html", "what is Vue.js used for"},
		{"rust-mutability", GroupFocused, "https://doc.rust-lang.org/book/ch03-01-variables-and-mutability.html", "how do you make a variable mutable in Rust"},
		{"mdn-http-404", GroupFocused, "https://developer.mozilla.org/en-US/docs/Web/HTTP/Status/404", "what does HTTP status 404 mean"},
		{"java-arrays", GroupFocused, "https://docs.oracle.com/javase/tutorial/java/nutsandbolts/arrays.html", "how do you declare an array in Java"},
		{"go-tutorial-start", GroupFocused, "https://go.dev/doc/tutorial/getting-started", "which command initializes a new Go module"},
		{"prettier-docs", GroupFocused, "https://prettier.io/docs/en/index.html", "which file formats does prettier support"},
		{"man-ls", GroupFocused, "https://man7.org/linux/man-pages/man1/ls.1.html", "which ls flag shows hidden files"},
		{"man-grep", GroupFocused, "https://man7.org/linux/man-pages/man1/grep.1.html", "which grep flag makes the search case-insensitive"},
		{"python-faq", GroupFocused, "https://docs.python.org/3/faq/general.html", "who created python"},
		{"jsonlines", GroupFocused, "https://jsonlines.org", "what file extension does JSON Lines use"},
		{"go-doc-install", GroupFocused, "https://go.dev/doc/install", "how do I verify a go installation was successful"},
		{"pip-getting-started", GroupFocused, "https://pip.pypa.io/en/stable/getting-started/", "which command upgrades pip itself"},
		{"conventional-commits", GroupFocused, "https://www.conventionalcommits.org/en/v1.0.0/", "what type of change does a commit prefixed feat represent"},
		{"keep-a-changelog", GroupFocused, "https://keepachangelog.com/en/1.1.0/", "what does the Unreleased section hold"},
		{"twelve-factor-config", GroupFocused, "https://12factor.net/config", "should configuration be stored in the code according to twelve-factor"},
		{"nodejs-about", GroupFocused, "https://nodejs.org/en/about", "when was Node.js created"},
		{"python-venv", GroupFocused, "https://docs.python.org/3/tutorial/venv.html", "which command creates a virtual environment in python"},
		{"opensource-mit", GroupFocused, "https://opensource.org/licenses/MIT", "does the MIT license require you to include the original copyright notice"},
	}
	reference := []source{
		{"nodejs-fs", GroupReference, "https://nodejs.org/api/fs.html", "how do I read a file synchronously in Node.js"},
		{"pkggodev-strings", GroupReference, "https://pkg.go.dev/strings", "which Go strings function trims whitespace from both ends"},
		{"sqlite-select", GroupReference, "https://www.sqlite.org/lang_select.html", "how do I do a LEFT JOIN in SQLite"},
	}
	return append(focused, reference...)
}

func TestBuildTheCorpusByFetchingEveryPageOnce(t *testing.T) {
	if os.Getenv("TOFU_BUILD_CORPUS") != "1" {
		t.Skip("set TOFU_BUILD_CORPUS=1 to fetch the corpus pages fresh and rewrite testdata/page-corpus.jsonl")
	}
	client := &http.Client{Timeout: 20 * time.Second}
	var out strings.Builder
	for _, src := range corpusSources() {
		req, err := http.NewRequest(http.MethodGet, src.url, nil)
		if err != nil {
			t.Fatalf("%s: %v", src.id, err)
		}
		req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; tofu-page-sift-corpus/1.0)")
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("%s: fetch: %v", src.id, err)
		}
		body, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			t.Fatalf("%s: read: %v", src.id, err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s: %s returned %d", src.id, src.url, resp.StatusCode)
		}
		contentType := resp.Header.Get("Content-Type")
		if contentType == "" {
			contentType = "text/html"
		}

		row := Row{
			Source:      corpus.Scrub(src.id),
			Group:       src.group,
			URL:         corpus.Scrub(src.url),
			Task:        corpus.Scrub(src.task),
			ContentType: corpus.Scrub(contentType),
			Body:        corpus.Scrub(string(body)),
		}
		encoded, err := json.Marshal(row)
		if err != nil {
			t.Fatalf("%s: encode: %v", src.id, err)
		}
		if leaks := corpus.LeaksIn(string(encoded)); len(leaks) > 0 {
			t.Fatalf("%s: the fetched page still carries %q after scrub", src.id, leaks)
		}
		out.Write(encoded)
		out.WriteString("\n")
	}
	if err := os.WriteFile(corpusFile, []byte(out.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Logf("wrote %s, %d sources, %d bytes", corpusFile, len(corpusSources()), out.Len())
}
