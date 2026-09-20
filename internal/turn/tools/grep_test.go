package tools_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"boji/internal/konst"
	"boji/internal/turn/tools"
)

func TestGrepUnderASubdirectoryIgnoreFileReportsOnlyTheFilesItSearched(t *testing.T) {
	root := t.TempDir()
	seed(t, root, ".gitignore", "vendored/\n")
	seed(t, root, "src/app.go", "const needleHere = 1\n")
	seed(t, root, "vendored/copy.go", "const needleHere = 2\n")
	grepTool, err := tools.NewGrep(root)
	if err != nil {
		t.Fatalf("building the tool: %v", err)
	}

	found, err := grepTool.Run(context.Background(), json.RawMessage(`{"pattern":"needleHere"}`))
	if err != nil {
		t.Fatalf("grep: %v", err)
	}
	if strings.Contains(found.Content, "vendored/copy.go") {
		t.Fatalf("grep read a file the .gitignore excludes:\n%s", found.Content)
	}
	if !strings.Contains(found.Content, "1 of the 2 text files") {
		t.Fatalf("grep searched something other than app.go and the .gitignore itself:\n%s", found.Content)
	}

	unfiltered, err := grepTool.Run(context.Background(), json.RawMessage(`{"pattern":"needleHere","include_ignored":true}`))
	if err != nil {
		t.Fatalf("grep: %v", err)
	}
	if !strings.Contains(unfiltered.Content, "vendored/copy.go") || !strings.Contains(unfiltered.Content, "degraded unfiltered") {
		t.Fatalf("the override did not reach the ignored file or did not say so:\n%s", unfiltered.Content)
	}
}

func TestAnAgentCanGrepThisRepositoryForItsOwnInstructions(t *testing.T) {
	root := repositoryRoot(t)
	if _, err := os.Stat(filepath.Join(root, "CLAUDE.md")); err != nil {
		t.Skip("CLAUDE.md is itself gitignored here, so a clone does not carry one to find")
	}
	grepTool, err := tools.NewGrep(root)
	if err != nil {
		t.Fatalf("building the tool: %v", err)
	}
	found, err := grepTool.Run(context.Background(), json.RawMessage(`{"pattern":"orchestrator","glob":"CLAUDE.md"}`))
	if err != nil {
		t.Fatalf("grep: %v", err)
	}
	if !strings.Contains(found.Content, "CLAUDE.md:") {
		t.Fatalf("line 11 of the root .gitignore hid the project's own rules from grep:\n%s", found.Content)
	}
}

func TestABinaryFileIsNamedAsSkippedByGrepAndRefusedByEdit(t *testing.T) {
	root := t.TempDir()
	seed(t, root, "notes.txt", "needleHere\n")
	seed(t, root, "blob.bin", "needleHere\x00tail\n")

	grepTool, err := tools.NewGrep(root)
	if err != nil {
		t.Fatalf("building grep: %v", err)
	}
	grepped, err := grepTool.Run(context.Background(), json.RawMessage(`{"pattern":"needleHere"}`))
	if err != nil {
		t.Fatalf("running grep: %v", err)
	}
	for _, want := range []string{"degraded binary_skipped", "null byte", "not a complete answer"} {
		if !strings.Contains(grepped.Content, want) {
			t.Fatalf("grep never says %q about the file it did not read:\n%s", want, grepped.Content)
		}
	}

	editTool, err := tools.NewEdit(root)
	if err != nil {
		t.Fatalf("building edit: %v", err)
	}
	_, err = editTool.Run(context.Background(), json.RawMessage(`{"path":"blob.bin","old_string":"needleHere","new_string":"other"}`))
	if err == nil || !strings.Contains(err.Error(), "degraded binary_skipped") {
		t.Fatalf("edit rewrote a binary file or did not name why it refused: %v", err)
	}
}

func walkCost(t *testing.T, root, args string) (files int, total int64) {
	t.Helper()
	globTool, err := tools.NewGlob(root)
	if err != nil {
		t.Fatalf("building the tool: %v", err)
	}
	listed, err := globTool.Run(context.Background(), json.RawMessage(args))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	for _, line := range strings.Split(listed.Content, "\n") {
		if line == "" || strings.Contains(line, " ") {
			continue
		}
		info, statErr := os.Stat(filepath.Join(root, filepath.FromSlash(line)))
		if statErr != nil {
			continue
		}
		files++
		total += info.Size()
	}
	return files, total
}

func TestTheIgnoredWalkIsMeasuredOnTheRecordedQuestion(t *testing.T) {
	root := repositoryRoot(t)
	grepTool, err := tools.NewGrep(root)
	if err != nil {
		t.Fatalf("building the tool: %v", err)
	}

	started := time.Now()
	filtered, err := grepTool.Run(context.Background(), json.RawMessage(`{"pattern":"TODO|FIXME|XXX|HACK","path":"."}`))
	if err != nil {
		t.Fatalf("grep: %v", err)
	}
	files, bytes := walkCost(t, root, `{"pattern":"**"}`)
	t.Logf("filtered: %d files walked, %d bytes, %d tokens in the result, %s",
		files, bytes, len(filtered.Content)/konst.SearchBytesPerToken, time.Since(started).Round(time.Millisecond))

	if files == 0 {
		t.Fatal("the filtered walk found no file at all")
	}
	if strings.Contains(filtered.Content, ".local/") {
		t.Fatalf("grep read the ignored .local tree, which is what this ticket is about")
	}
	if os.Getenv("BOJI_MEASURE_UNFILTERED") == "" {
		t.Skip("the unfiltered arm reads every ignored byte in the tree, so it runs only under BOJI_MEASURE_UNFILTERED")
	}

	started = time.Now()
	unfiltered, err := grepTool.Run(context.Background(), json.RawMessage(`{"pattern":"TODO|FIXME|XXX|HACK","path":".","include_ignored":true}`))
	if err != nil {
		t.Fatalf("grep unfiltered: %v", err)
	}
	wideFiles, wideBytes := walkCost(t, root, `{"pattern":"**","include_ignored":true}`)
	t.Logf("unfiltered: %d files walked, %d bytes, %d tokens in the result, %s",
		wideFiles, wideBytes, len(unfiltered.Content)/konst.SearchBytesPerToken, time.Since(started).Round(time.Millisecond))
	if wideBytes <= bytes {
		t.Fatalf("the unfiltered walk read %d bytes and the filtered one %d, so nothing was being skipped", wideBytes, bytes)
	}
}
