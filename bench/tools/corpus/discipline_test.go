package corpus

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"tofu/bench/tools"
)

var identifierSplit = regexp.MustCompile(`[^A-Za-z0-9]+`)

func splitIdentifier(s string) []string {
	var out []string
	for _, part := range identifierSplit.Split(s, -1) {
		out = append(out, splitCamel(part)...)
	}
	return out
}

func splitCamel(s string) []string {
	var words []string
	var cur []rune
	for i, r := range s {
		if i > 0 && r >= 'A' && r <= 'Z' && cur[len(cur)-1] >= 'a' {
			words = append(words, string(cur))
			cur = nil
		}
		cur = append(cur, r)
	}
	if len(cur) > 0 {
		words = append(words, string(cur))
	}
	return words
}

func forbiddenWords(a Answer, identifiers []string) map[string]bool {
	forbidden := map[string]bool{}
	add := func(s string) {
		for _, w := range splitIdentifier(s) {
			if len(w) >= 3 {
				forbidden[strings.ToLower(w)] = true
			}
		}
	}
	for _, part := range strings.Split(a.File, "/") {
		add(part)
		add(strings.TrimSuffix(part, filepath.Ext(part)))
	}
	for _, id := range identifiers {
		add(id)
	}
	return forbidden
}

func TestDescribedAndIntentQuestionsCarryNoAnswerVocabulary(t *testing.T) {
	qs, err := ReadQuestions(corpusPath)
	if err != nil {
		t.Fatalf("ReadQuestions: %v", err)
	}
	if len(qs) == 0 {
		t.Fatal("the corpus is empty")
	}
	for _, q := range qs {
		if q.Band == BandNamed {
			continue
		}
		words := tools.ContentWords(q.Text)
		for _, p := range q.Pins() {
			forbidden := forbiddenWords(p.Answer, q.Identifiers)
			for _, w := range words {
				if forbidden[strings.ToLower(w)] {
					t.Errorf("%s (%s): %q names %q, a path component, stem or identifier of its own answer %s:%d",
						q.ID, q.Band, w, w, p.File, p.Line)
				}
			}
		}
	}
}

func reaches(t, want reflect.Type) bool {
	if t == want {
		return true
	}
	if kind := t.Kind(); kind == reflect.Slice || kind == reflect.Array || kind == reflect.Pointer {
		return reaches(t.Elem(), want)
	}
	if t.Kind() != reflect.Struct {
		return false
	}
	for i := range t.NumField() {
		if reaches(t.Field(i).Type, want) {
			return true
		}
	}
	return false
}

func TestWhatAnArmIsHandedCarriesNoFingerprint(t *testing.T) {
	asked, fingerprint := reflect.TypeOf(Asked{}), reflect.TypeOf(LineFingerprint(""))
	for i := range asked.NumField() {
		if field := asked.Field(i); reaches(field.Type, fingerprint) {
			t.Errorf("Asked.%s reaches a %s: an arm that can read the fingerprint of its own answer line is the strongest leak this corpus could carry", field.Name, fingerprint)
		}
	}
	for i := range asked.NumMethod() {
		method := asked.Method(i)
		for out := range method.Type.NumOut() {
			if reaches(method.Type.Out(out), fingerprint) {
				t.Errorf("Asked.%s returns a %s and an arm calls it", method.Name, fingerprint)
			}
		}
	}

	qs, err := ReadQuestions(corpusPath)
	if err != nil {
		t.Fatalf("ReadQuestions: %v", err)
	}
	pinned := 0
	for _, q := range qs {
		handed, err := json.Marshal(q.Asked())
		if err != nil {
			t.Fatalf("%s: %v", q.ID, err)
		}
		for _, p := range q.Pins() {
			if p.Fingerprint == "" {
				continue
			}
			pinned++
			if strings.Contains(string(handed), string(p.Fingerprint)) {
				t.Errorf("%s: what the arms are handed carries the fingerprint of %s:%d", q.ID, p.File, p.Line)
			}
		}
	}
	t.Logf("%d fingerprints recorded, none of them in what an arm is handed", pinned)
}
