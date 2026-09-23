package search_test

import (
	"strings"
	"testing"

	"tofu/internal/search"
)

const mixedCase = `package service

func Authenticate(user string) error {
	return nil
}
`

func TestACaseInsensitiveFormAnswersWhereTheLiteralFoundNothing(t *testing.T) {
	result := find(t, "authenticate", 0, map[string]string{"auth/service.go": mixedCase})

	if len(result.Units) != 1 {
		t.Fatalf("the literal missed on case and no candidate form answered: %d units, %+v", len(result.Units), result.Tried)
	}
	if result.Answered != search.CaseInsensitive {
		t.Fatalf("the result does not say which form answered: %q", result.Answered)
	}
	if result.Units[0].Symbol != "Authenticate" {
		t.Fatalf("the candidate form found the wrong declaration: %+v", result.Units[0])
	}
	if !strings.Contains(result.Text, "the pattern was wrong, not the answer absent") {
		t.Fatalf("the result does not tell the model the pattern missed rather than the code being absent:\n%s", result.Text)
	}
}

func TestEveryCandidateEmptyIsReportedAsAnAbsenceRatherThanAWrongPattern(t *testing.T) {
	result := find(t, "Cryptolalia", 0, map[string]string{"auth/service.go": mixedCase})

	if result.Answered != search.NoCandidate {
		t.Fatalf("nothing matched and the result still names a form: %q", result.Answered)
	}
	want := []string{
		"no code unit holds that pattern, and no candidate form finds one either",
		"this is an absence, not a failure",
		"literal Cryptolalia: 0 lines in 0 files",
		"case insensitive (?i)Cryptolalia: 0 lines in 0 files",
		"word boundary: not run, the literal matched no line, and this form matches a subset of the literal",
		"go symbol: not run, the literal matched no line",
	}
	for _, phrase := range want {
		if !strings.Contains(result.Text, phrase) {
			t.Fatalf("the absence does not state %q:\n%s", phrase, result.Text)
		}
	}
}

func TestAWrongPatternAndAnAbsenceReadDifferentlyToAModel(t *testing.T) {
	files := map[string]string{"auth/service.go": mixedCase}
	wrong := find(t, "authenticate", 0, files)
	absent := find(t, "Cryptolalia", 0, files)

	t.Logf("a wrong pattern reads:\n%s\nan absence reads:\n%s", wrong.Text, absent.Text)
	if wrong.Text == absent.Text {
		t.Fatal("a wrong pattern and an absence render the same text, which is the ambiguity this is for")
	}
	if strings.Contains(absent.Text, "the pattern was wrong") {
		t.Fatalf("an absence claims the pattern was wrong:\n%s", absent.Text)
	}
	if strings.Contains(wrong.Text, "this is an absence") {
		t.Fatalf("a wrong pattern claims the code is absent:\n%s", wrong.Text)
	}
}

func attemptFor(t *testing.T, result search.Result, candidate search.Candidate) search.Attempt {
	t.Helper()
	for _, attempt := range result.Tried {
		if attempt.Candidate == candidate {
			return attempt
		}
	}
	t.Fatalf("the result does not report the %s form at all: %+v", candidate, result.Tried)
	return search.Attempt{}
}

func TestANonGoTreeGetsTheTextFormsAndIsToldWhyItGetsNoSymbolForm(t *testing.T) {
	result := find(t, "port", 0, map[string]string{"web/app.ts": "const port = 3000\n"})

	if result.Answered != search.Literal {
		t.Fatalf("the literal matched a typescript file and the result says otherwise: %q", result.Answered)
	}
	if boundary := attemptFor(t, result, search.WordBoundary); boundary.Lines != 1 {
		t.Fatalf("the word boundary form did not narrow a typescript match: %+v", boundary)
	}
	if notRun := attemptFor(t, result, search.GoSymbol).NotRun; !strings.Contains(notRun, "go is the only language parsed here") {
		t.Fatalf("the result does not say why a non go tree gets no symbol form: %q", notRun)
	}
}

func TestTheWordBoundaryFormIsNotRunOnAPatternThatIsNotAPlainWord(t *testing.T) {
	result := find(t, `func Auth.*\(`, 0, map[string]string{"auth/service.go": mixedCase})

	if result.Answered != search.Literal {
		t.Fatalf("the regular expression did not match what it was written for: %q", result.Answered)
	}
	if notRun := attemptFor(t, result, search.WordBoundary).NotRun; !strings.Contains(notRun, "not a plain word") {
		t.Fatalf("a regular expression was wrapped in word boundaries anyway: %q", notRun)
	}
}

func TestANarrowingFormRunsOnlyOverWhatTheLiteralAlreadyMatched(t *testing.T) {
	result := find(t, "port", 0, map[string]string{
		"web/app.ts":     "const port = 3000\n",
		"web/export.ts":  "export const other = 1\n",
		"web/absent.txt": "nothing at all\n",
	})

	literal := attemptFor(t, result, search.Literal)
	boundary := attemptFor(t, result, search.WordBoundary)
	if literal.Lines != 2 || literal.Files != 2 {
		t.Fatalf("the literal must match export as well as port: %+v", literal)
	}
	if boundary.Lines != 1 || boundary.Files != 1 {
		t.Fatalf("the word boundary form must drop the hit inside export: %+v", boundary)
	}
}
