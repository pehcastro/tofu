package search_test

import (
	"strings"
	"testing"

	"tofu/internal/search"
)

func TestASearchCutShortByItsBudgetSaysSoByNameRatherThanLookingComplete(t *testing.T) {
	result := find(t, "needle", 60, map[string]string{
		"a.go": "package a\n\nfunc One() { _ = \"needle\" }\n\nfunc Two() { _ = \"needle\" }\n",
		"b.go": "package b\n\nfunc Three() { _ = \"needle\" }\n",
		"c.go": "package c\n\nfunc Four() { _ = \"needle\" }\n",
	})

	if result.Stats.Returned >= result.Stats.Units {
		t.Fatalf("the budget cut nothing, so this proves nothing: %+v", result.Stats)
	}
	for _, want := range []string{"degraded truncated", "a cap or a budget cut the answer short", "raise max_tokens", "not a complete answer"} {
		if !strings.Contains(result.Text, want) {
			t.Fatalf("a cut result never says %q:\n%s", want, result.Text)
		}
	}
}

func TestAFileThatDidNotParseAndOneThatWasNotReadAreBothNamed(t *testing.T) {
	result := find(t, "needle", 0, map[string]string{
		"broken.go": "package broken\n\nfunc Half( { needle }\n",
		"binary.go": "package b\nneedle\x00tail\n",
	})

	for _, want := range []string{"degraded fallback", "degraded binary_skipped", "null byte"} {
		if !strings.Contains(result.Text, want) {
			t.Fatalf("the result never says %q:\n%s", want, result.Text)
		}
	}
}

func TestADegradedStateOutsideTheVocabularyFailsRatherThanRendering(t *testing.T) {
	defer func() {
		recovered := recover()
		if recovered == nil {
			t.Fatal("an unknown degraded state rendered as if it were one of ours")
		}
		if !strings.Contains(recovered.(string), "not a degraded state") {
			t.Fatalf("the panic does not name the problem: %v", recovered)
		}
	}()
	search.Note(search.Degraded("invented"), "")
}
