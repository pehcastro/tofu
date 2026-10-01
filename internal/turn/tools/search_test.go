package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"tofu/internal/search"
	"tofu/internal/turn"
)

const searchProfileEnvar = "TOFU_SEARCH_PROFILE"

func TestSearchProfileOverATree(t *testing.T) {
	dir := os.Getenv(searchProfileEnvar)
	if dir == "" {
		t.Skip("skipped: " + searchProfileEnvar + " names no tree to search, and this test reads no path of its own")
	}
	tool, err := NewSearch(dir)
	if err != nil {
		t.Fatal(err)
	}
	root := tool.root
	under := os.Getenv(searchProfileEnvar + "_UNDER")
	patterns := []string{"isNodeError", "ISNODEERROR", "^export function", "import"}
	if under != "" {
		patterns = patterns[:1]
	}
	for index, pattern := range patterns {
		started := time.Now()
		files, err := profiledFiles(root, under, os.Getenv(searchProfileEnvar+"_HALF"))
		if err != nil {
			t.Fatal(err)
		}
		listing := time.Since(started)
		started = time.Now()
		result, err := search.Find(search.Request{Root: string(root), Files: files, Pattern: regexp.MustCompile(pattern), MaxTokens: 3000})
		if err != nil {
			t.Fatal(err)
		}
		found := time.Since(started)
		matching := result.Tried[0].Spent + result.Tried[2].Spent
		t.Logf("%-16s wall: list %4d ms over %d files, find %5d ms. summed over files: read %6d ms, match %5d ms. %d candidates, %d units, %d scanned, %d skipped",
			pattern, listing.Milliseconds(), len(files), found.Milliseconds(), result.Stats.Read.Milliseconds(), matching.Milliseconds(),
			result.Stats.Candidates, result.Stats.Units, result.Stats.Scanned, result.Stats.Skipped)
		if under != "" {
			continue
		}
		args, err := json.Marshal(searchArgs{Pattern: pattern, MaxTokens: 3000})
		if err != nil {
			t.Fatal(err)
		}
		ran, err := tool.Run(context.Background(), args)
		if err != nil {
			t.Fatal(err)
		}
		if out := os.Getenv(searchProfileEnvar + "_OUT"); out != "" {
			if err := os.WriteFile(fmt.Sprintf("%s.%d.txt", out, index), []byte(ran.Content), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func profiledFiles(root turn.Root, under, half string) ([]string, error) {
	if under == "" {
		listed, err := filesUnder(root, ".", false)
		return listed.files, err
	}
	var files []string
	walked := 0
	err := filepath.WalkDir(filepath.Join(string(root), under), func(full string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		walked++
		if half == "" || (walked%2 == 0) == (half == "even") {
			rel, relErr := filepath.Rel(string(root), full)
			files = append(files, filepath.ToSlash(rel))
			return relErr
		}
		return nil
	})
	return files, err
}
