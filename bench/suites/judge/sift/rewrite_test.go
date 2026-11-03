package sift

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	benchcorpus "tofu/bench/corpus"
)

func TestRewriteTheCorpusThroughTheScrub(t *testing.T) {
	if os.Getenv("TOFU_REWRITE_CORPUS") != "1" {
		t.Skip("set TOFU_REWRITE_CORPUS=1 to rewrite the fixture through bench/corpus")
	}
	raw, err := os.ReadFile(corpusFile)
	if err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var row map[string]string
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			t.Fatal(err)
		}
		for key, value := range row {
			row[key] = benchcorpus.Scrub(value)
		}
		encoded, err := json.Marshal(row)
		if err != nil {
			t.Fatal(err)
		}
		out.Write(encoded)
		out.WriteString("\n")
	}
	if leaks := benchcorpus.LeaksIn(out.String()); len(leaks) > 0 {
		t.Fatalf("the scrubbed corpus still carries %q", leaks)
	}
	if err := os.WriteFile(corpusFile, []byte(out.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Logf("rewrote %s, %d bytes", corpusFile, out.Len())
}
