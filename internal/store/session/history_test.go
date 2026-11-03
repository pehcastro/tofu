package session

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"tofu/internal/konst"
	"tofu/internal/sys"
)

func freshHistory(t *testing.T) (*PromptHistory, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "not", "made", "yet")
	return OpenPromptHistory(dir), dir
}

func TestPromptHistoryAMissingFileIsEmptyAndTheFirstAddMakesIt(t *testing.T) {
	history, _ := freshHistory(t)
	if got := history.Prompts(); len(got) != 0 {
		t.Fatalf("a missing file read as %q", got)
	}
	if err := history.Add("first"); err != nil {
		t.Fatalf("the first add into a missing directory: %v", err)
	}
	if got := history.Prompts(); !slices.Equal(got, []string{"first"}) {
		t.Fatalf("after one add the history is %q", got)
	}
}

func TestPromptHistoryIsNewestFirstAndKeepsNewlines(t *testing.T) {
	history, _ := freshHistory(t)
	for _, prompt := range []string{"one", "two\nlines", "three"} {
		if err := history.Add(prompt); err != nil {
			t.Fatal(err)
		}
	}
	if got := history.Prompts(); !slices.Equal(got, []string{"three", "two\nlines", "one"}) {
		t.Fatalf("the history reads %q", got)
	}
}

func TestPromptHistoryKeepsARepeatOfTheOneBeforeOnce(t *testing.T) {
	history, dir := freshHistory(t)
	for _, prompt := range []string{"a", "b", "b", "a"} {
		if err := history.Add(prompt); err != nil {
			t.Fatal(err)
		}
	}
	if got := history.Prompts(); !slices.Equal(got, []string{"a", "b", "a"}) {
		t.Fatalf("the history reads %q", got)
	}
	again := OpenPromptHistory(dir)
	if err := again.Add("a"); err != nil {
		t.Fatal(err)
	}
	if got := again.Prompts(); !slices.Equal(got, []string{"a", "b", "a"}) {
		t.Fatalf("a second instance repeating the newest prompt reads %q", got)
	}
}

func TestPromptHistorySkipsACorruptLineAndATornTail(t *testing.T) {
	history, dir := freshHistory(t)
	if err := history.Add("kept before"); err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(filepath.Join(dir, promptHistoryName), os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = file.WriteString("\nnot json at all\n{\"prompt\":\"\"}\n{\"prompt\":42}\n")
	_ = file.Close()
	if err := history.Add("kept after"); err != nil {
		t.Fatal(err)
	}
	file, _ = os.OpenFile(filepath.Join(dir, promptHistoryName), os.O_APPEND|os.O_WRONLY, 0o600)
	_, _ = file.WriteString("\n{\"prompt\":\"torn")
	_ = file.Close()
	if got := history.Prompts(); !slices.Equal(got, []string{"kept after", "kept before"}) {
		t.Fatalf("a corrupt file reads %q", got)
	}
	if err := history.Add("after the tear"); err != nil {
		t.Fatal(err)
	}
	if got := history.Prompts(); len(got) == 0 || got[0] != "after the tear" {
		t.Fatalf("a line added after a torn tail reads %q", got)
	}
}

func TestPromptHistoryRedactsAStoredKeyBeforeWriting(t *testing.T) {
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)
	const key = "sk-or-v1-made-up-history-7Rk2Qa"
	if err := sys.SaveKey(sys.OpenRouterKeyName, key); err != nil {
		t.Fatal(err)
	}
	history, dir := freshHistory(t)
	if err := history.Add("use " + key + " and " + sys.TypeSafeKeyName + "=ts-made-up-loose-9Xw3"); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, promptHistoryName))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), key) || strings.Contains(string(raw), "ts-made-up-loose") {
		t.Fatalf("the history file holds a key in clear: %s", raw)
	}
	if got := history.Prompts(); len(got) != 1 || strings.Count(got[0], sys.KeyRedactedMark) != 2 {
		t.Fatalf("the redacted prompt reads %q", got)
	}
}

func TestPromptHistoryDropsAPromptOverTheEntryCapRatherThanCuttingIt(t *testing.T) {
	history, _ := freshHistory(t)
	exact := strings.Repeat("x", konst.PromptHistoryEntryBytes)
	if err := history.Add(exact); err != nil {
		t.Fatal(err)
	}
	if err := history.Add(exact + "y"); err != nil {
		t.Fatal(err)
	}
	if got := history.Prompts(); len(got) != 1 || got[0] != exact {
		t.Fatalf("after one prompt at the cap and one over it the history holds %d entries", len(got))
	}
}

func TestPromptHistoryTrimsToTheNewestEntriesPastTheFileCap(t *testing.T) {
	history, dir := freshHistory(t)
	body := strings.Repeat("z", konst.PromptHistoryEntryBytes/2)
	total := konst.PromptHistoryFileBytes/len(body) + 2
	for at := range total {
		if err := history.Add(strconv.Itoa(at) + body); err != nil {
			t.Fatal(err)
		}
	}
	info, err := os.Stat(filepath.Join(dir, promptHistoryName))
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() > konst.PromptHistoryFileBytes {
		t.Fatalf("the file is %d bytes, over the cap of %d", info.Size(), konst.PromptHistoryFileBytes)
	}
	got := history.Prompts()
	if len(got) == 0 || len(got) > konst.PromptHistoryEntries || got[0] != strconv.Itoa(total-1)+body {
		t.Fatalf("after trimming the history holds %d entries and the newest is not the last added", len(got))
	}
}

func TestPromptHistoryTwoInstancesWritingAtOnceTearNoLine(t *testing.T) {
	history, dir := freshHistory(t)
	if err := history.Add("seed"); err != nil {
		t.Fatal(err)
	}
	const each = 50
	var wait sync.WaitGroup
	for _, name := range []string{"left", "right"} {
		instance := OpenPromptHistory(dir)
		wait.Go(func() {
			for at := range each {
				if err := instance.Add(name + strconv.Itoa(at) + strings.Repeat(".", 3000)); err != nil {
					t.Error(err)
				}
			}
		})
	}
	wait.Wait()
	if got := history.Prompts(); len(got) != 2*each+1 {
		t.Fatalf("two instances writing %d prompts each left %d readable entries", each, len(got)-1)
	}
}
