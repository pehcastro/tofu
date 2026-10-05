package search

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"tofu/internal/konst"
)

var needle = regexp.MustCompile("needle")

func findNeedle(t *testing.T, name, body string) Result {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := Find(Request{Root: root, Files: []string{name}, Pattern: needle})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func onlyBody(t *testing.T, result Result) string {
	t.Helper()
	if len(result.Units) != 1 {
		t.Fatalf("%d units returned, want the one match shown:\n%s", len(result.Units), result.Text)
	}
	return result.Units[0].Body
}

var cutMarkers = regexp.MustCompile(`^\[(\d+) characters cut\] (.*) \[(\d+) characters cut\]$`)

func checkCut(t *testing.T, line, body string) {
	t.Helper()
	parts := cutMarkers.FindStringSubmatch(body)
	if parts == nil {
		t.Fatalf("no marker on each side: %.120q", body)
	}
	if !utf8.ValidString(body) {
		t.Fatalf("a cut split a rune: %.120q", body)
	}
	at := strings.Index(line, parts[2])
	if at < 0 {
		t.Fatalf("the kept text is not in the line: %.120q", parts[2])
	}
	left := strconv.Itoa(utf8.RuneCountInString(line[:at]))
	right := strconv.Itoa(utf8.RuneCountInString(line[at+len(parts[2]):]))
	if parts[1] != left || parts[3] != right {
		t.Fatalf("the markers say %s and %s characters cut, the line dropped %s and %s", parts[1], parts[3], left, right)
	}
}

func TestLongLineMatchIsShownCutAroundIt(t *testing.T) {
	line := strings.Repeat("var a=1;", 131072) + "needle()" + strings.Repeat("var b=2;", 131072)
	body := onlyBody(t, findNeedle(t, "app.min.js", line+"\n"))
	if !strings.Contains(body, "needle()") || len(body) > konst.SearchLineWidth+64 {
		t.Fatalf("body is %d bytes and must hold the match within about %d: %.200q", len(body), konst.SearchLineWidth, body)
	}
	checkCut(t, line, body)
}

func TestLongLineCutCountsCharactersAndKeepsRunesWhole(t *testing.T) {
	line := strings.Repeat("é", 4000) + "needle" + strings.Repeat("ü", 4000)
	body := onlyBody(t, findNeedle(t, "text.md", line))
	if !strings.Contains(body, "needle") {
		t.Fatalf("the cut lost the match: %.120q", body)
	}
	checkCut(t, line, body)
}

func TestLongLineNearAnEdgeIsCutOnOneSideOnly(t *testing.T) {
	tail := strings.Repeat("x", 5000)
	dropped := strconv.Itoa(5006 - konst.SearchLineWidth)
	early := onlyBody(t, findNeedle(t, "a.txt", "needle"+tail))
	if !strings.HasPrefix(early, "needle") || !strings.HasSuffix(early, " ["+dropped+" characters cut]") {
		t.Fatalf("a match at column 0 is cut wrongly: %q", early[len(early)-40:])
	}
	late := onlyBody(t, findNeedle(t, "b.txt", tail+"needle\r\n"))
	if !strings.HasSuffix(late, "needle") || !strings.HasPrefix(late, "["+dropped+" characters cut] ") {
		t.Fatalf("a match at the end of a line ending in a carriage return is cut wrongly: %q", late[:40])
	}
}

func TestLineAtTheWidthIsNotCut(t *testing.T) {
	line := "needle" + strings.Repeat("y", konst.SearchLineWidth-6)
	if body := onlyBody(t, findNeedle(t, "c.txt", line)); body != line {
		t.Fatalf("a line exactly the width long was changed: %q", body)
	}
}

func TestLongNeighbourLinesAreCutToo(t *testing.T) {
	long := strings.Repeat("z", 1<<20)
	file := long + "\n" + long + "\nshort needle\n" + long + "\n" + long + "\n" + long + "needle\n"
	body := onlyBody(t, findNeedle(t, "d.js", file))
	if len(body) > 7*(konst.SearchLineWidth+64) || strings.Count(body, "needle") != 2 {
		t.Fatalf("the frame holds %d bytes and %d of the 2 matches", len(body), strings.Count(body, "needle"))
	}
}

func TestGoFileLongLineIsNotCut(t *testing.T) {
	literal := strings.Repeat("q", 3000)
	result := findNeedle(t, "p.go", "package p\n\nfunc needle() string {\n\treturn \""+literal+"\"\n}\n")
	if len(result.Units) != 1 || !strings.Contains(result.Units[0].Body, literal) {
		t.Fatalf("a parsed go file changed: %s", result.Text)
	}
}

func findBesideHugeFile(t *testing.T, small string) Result {
	t.Helper()
	root := t.TempDir()
	huge, err := os.Create(filepath.Join(root, "build.log"))
	if err != nil {
		t.Fatal(err)
	}
	if err := huge.Truncate(konst.SearchFileByteCap + 1); err != nil {
		t.Fatal(err)
	}
	if err := huge.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte(small), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := Find(Request{Root: root, Files: []string{"build.log", "a.txt"}, Pattern: needle})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestAbsenceBesideAnUnreadFileDoesNotClaimEveryFileWasRead(t *testing.T) {
	result := findBesideHugeFile(t, "nothing here\n")
	if strings.Contains(result.Text, "the files were read") || !strings.Contains(result.Text, "1 file over") {
		t.Fatalf("an absence beside a file over the cap claims every file was read:\n%s", result.Text)
	}
}

func TestFileOverTheCapIsSkippedAndCountedOnce(t *testing.T) {
	result := findBesideHugeFile(t, "NEEDLE\n")
	if result.Stats.TooLarge != 1 || result.Stats.Skipped != 0 || len(result.Units) != 1 {
		t.Fatalf("too large %d, binary %d, units %d: want 1, 0, 1 after the case insensitive rescan:\n%s",
			result.Stats.TooLarge, result.Stats.Skipped, len(result.Units), result.Text)
	}
	if !strings.Contains(result.Text, "1 file over") {
		t.Fatalf("the result does not say a file was skipped for its size:\n%s", result.Text)
	}
}
