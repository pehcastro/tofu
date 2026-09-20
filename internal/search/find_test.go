package search_test

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"boji/internal/konst"
	"boji/internal/search"
)

func find(t *testing.T, pattern string, budget int, files map[string]string) search.Result {
	t.Helper()
	root := t.TempDir()
	names := make([]string, 0, len(files))
	for rel, body := range files {
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
			t.Fatalf("seeding %s: %v", rel, err)
		}
		if err := os.WriteFile(full, []byte(body), 0o600); err != nil {
			t.Fatalf("seeding %s: %v", rel, err)
		}
		names = append(names, rel)
	}
	slices.Sort(names)
	result, err := search.Find(search.Request{
		Root:      root,
		Files:     names,
		Pattern:   regexp.MustCompile(pattern),
		MaxTokens: budget,
	})
	if err != nil {
		t.Fatalf("search.Find: %v", err)
	}
	return result
}

const service = `package service

import "errors"

func Authenticate(user string) error {
	if user == "" {
		return errors.New("empty user")
	}
	return nil
}

func Unrelated() {}
`

func TestAMatchInsideAFunctionReturnsTheWholeFunction(t *testing.T) {
	result := find(t, "empty user", 0, map[string]string{"auth/service.go": service})

	if len(result.Units) != 1 {
		t.Fatalf("expected one unit, got %d: %+v", len(result.Units), result.Units)
	}
	unit := result.Units[0]
	body := strings.Split(unit.Body, "\n")
	if body[0] != "func Authenticate(user string) error {" {
		t.Fatalf("the unit does not start at the function signature, it starts at %q", body[0])
	}
	if body[len(body)-1] != "}" {
		t.Fatalf("the unit does not end at the closing brace, it ends at %q", body[len(body)-1])
	}
	if unit.Kind != search.KindFunc || unit.Symbol != "Authenticate" {
		t.Fatalf("the unit does not name what it is: %s %q", unit.Kind, unit.Symbol)
	}
	if unit.FirstLine != 5 || unit.LastLine != 10 {
		t.Fatalf("the unit does not carry the lines it came from: %d-%d", unit.FirstLine, unit.LastLine)
	}
	if strings.Contains(unit.Body, "func Unrelated") {
		t.Fatalf("the unit spilled into the next declaration:\n%s", unit.Body)
	}
}

const login = `package service

func Login(user string) string {
	` + "//" + ` authenticate happens before this
	return "authenticate now"
}
`

func TestAMatchInACommentAndAMatchInAStringAreNamedAsWhatTheyAre(t *testing.T) {
	result := find(t, "authenticate", 0, map[string]string{"auth/login.go": login})

	if len(result.Units) != 1 {
		t.Fatalf("both matches are in one function and must merge into one unit, got %d", len(result.Units))
	}
	matches := result.Units[0].Matches
	if len(matches) != 2 {
		t.Fatalf("expected the comment line and the string line, got %+v", matches)
	}
	if matches[0].Line != 4 || matches[0].Placement != search.InComment {
		t.Fatalf("the comment match is not reported as a comment: %+v", matches[0])
	}
	if matches[1].Line != 5 || matches[1].Placement != search.InString {
		t.Fatalf("the string match is not reported as a string literal: %+v", matches[1])
	}
	if !strings.Contains(result.Text, "in a comment") || !strings.Contains(result.Text, "in a string literal") {
		t.Fatalf("the result does not tell the model what the matches are:\n%s", result.Text)
	}
}

func numberedLines(count int, needle string, at int) string {
	var body strings.Builder
	for i := 1; i <= count; i++ {
		if i == at {
			body.WriteString(needle + "\n")
			continue
		}
		body.WriteString("filler line\n")
	}
	return body.String()
}

func TestANonGoFileReturnsTheLineWithItsFrameAndCountsAsAFallback(t *testing.T) {
	result := find(t, "needle", 0, map[string]string{"notes.md": numberedLines(20, "the needle is here", 10)})

	if len(result.Units) != 1 {
		t.Fatalf("expected one framed unit, got %d", len(result.Units))
	}
	unit := result.Units[0]
	if !unit.Fallback || unit.Kind != search.KindLines {
		t.Fatalf("a file this tool cannot parse must be a counted fallback: %+v", unit)
	}
	if unit.FirstLine != 10-konst.SearchFrameLines || unit.LastLine != 10+konst.SearchFrameLines {
		t.Fatalf("the frame is not the lines around the match: %d-%d", unit.FirstLine, unit.LastLine)
	}
	if !strings.Contains(unit.Body, "the needle is here") {
		t.Fatalf("the frame lost the matching line:\n%s", unit.Body)
	}
	if result.Stats.Fallbacks != 1 {
		t.Fatalf("the result does not count the fallback: %+v", result.Stats)
	}
	if !strings.Contains(result.Text, "1 fallback") {
		t.Fatalf("the result does not say it fell back:\n%s", result.Text)
	}
}

const broken = `package service

func Authenticate(user string) error {
	if user == "" {
		return errors.New("empty user"
}
`

func TestAGoFileThatDoesNotParseFallsBackRatherThanFailing(t *testing.T) {
	result := find(t, "empty user", 0, map[string]string{"auth/broken.go": broken})

	if len(result.Units) != 1 {
		t.Fatalf("expected the unparsable file to still answer, got %d units", len(result.Units))
	}
	if !result.Units[0].Fallback || result.Stats.Fallbacks != 1 {
		t.Fatalf("a go file that does not parse must be a counted fallback: %+v %+v", result.Units[0], result.Stats)
	}
	if result.Units[0].Matches[0].Placement != search.NotParsed {
		t.Fatalf("the tool must not claim to know what an unparsed match is: %+v", result.Units[0].Matches[0])
	}
	if !strings.Contains(result.Units[0].Body, `errors.New("empty user"`) {
		t.Fatalf("the fallback lost the matching line:\n%s", result.Units[0].Body)
	}
}

func longFunctions(count int) map[string]string {
	files := make(map[string]string, count)
	for i := range count {
		var body strings.Builder
		body.WriteString("package service\n\nfunc Handle" + string(rune('A'+i)) + "() int {\n\ttotal := 0\n")
		for line := range 40 {
			body.WriteString("\ttotal += needle" + string(rune('0'+line%10)) + "\n")
		}
		body.WriteString("\treturn total\n}\n")
		files["pkg"+string(rune('a'+i))+"/handle.go"] = body.String()
	}
	return files
}

func TestOverTheBudgetTheToolReturnsFewerWholeUnitsRatherThanATruncatedOne(t *testing.T) {
	budget := 800
	result := find(t, "needle", budget, longFunctions(6))

	if result.Stats.Units != 6 {
		t.Fatalf("expected one unit per file, got %d", result.Stats.Units)
	}
	if result.Stats.Returned >= result.Stats.Units {
		t.Fatalf("the budget selected everything: %d of %d returned", result.Stats.Returned, result.Stats.Units)
	}
	if result.Stats.Returned == 0 {
		t.Fatalf("the budget returned nothing at all: %+v", result.Stats)
	}
	if result.Stats.Tokens > budget {
		t.Fatalf("the result claims %d tokens against a budget of %d", result.Stats.Tokens, budget)
	}
	if spent := len(result.Text) / konst.SearchBytesPerToken; spent > budget {
		t.Fatalf("the rendered result is about %d tokens against a budget of %d", spent, budget)
	}
	for _, unit := range result.Units {
		lines := strings.Split(unit.Body, "\n")
		if !strings.HasPrefix(lines[0], "func Handle") || lines[len(lines)-1] != "}" {
			t.Fatalf("a returned unit was cut rather than dropped:\n%s", unit.Body)
		}
	}
}

func TestTheResultStatesWhatItDidWithEveryCount(t *testing.T) {
	files := longFunctions(2)
	files["notes.md"] = numberedLines(5, "a needle in prose", 2)
	result := find(t, "needle", 0, files)

	if result.Stats.Candidates != 81 {
		t.Fatalf("expected 40 matching lines in each of two files plus one in prose, got %d", result.Stats.Candidates)
	}
	if result.Stats.Units != 3 || result.Stats.Returned != 3 || result.Stats.Fallbacks != 1 {
		t.Fatalf("the counts do not describe the search: %+v", result.Stats)
	}
	for _, phrase := range []string{"81 text candidates", "3 units", "3 returned", "1 fallback", "tokens"} {
		if !strings.Contains(result.Text, phrase) {
			t.Fatalf("the result does not state %q:\n%s", phrase, result.Text)
		}
	}
	if result.Stats.Tokens == 0 {
		t.Fatalf("the result does not price itself: %+v", result.Stats)
	}
}

func TestZeroMatchesIsAnAnswerRatherThanAFailure(t *testing.T) {
	result := find(t, "nothing here matches this", 0, map[string]string{"auth/service.go": service})

	if len(result.Units) != 0 || result.Stats.Candidates != 0 {
		t.Fatalf("expected no unit and no candidate: %+v", result.Stats)
	}
	if !strings.Contains(result.Text, "not a failure") {
		t.Fatalf("the empty result does not say it is an answer:\n%s", result.Text)
	}
}
