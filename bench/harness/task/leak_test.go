package task_test

import (
	"fmt"
	"strings"
	"testing"
)

type errorfer interface {
	Helper()
	Errorf(format string, args ...any)
}

func leakedTokens(prompt string, secrets []string) []string {
	var found []string
	for _, secret := range secrets {
		if strings.Contains(prompt, secret) {
			found = append(found, secret)
		}
	}
	return found
}

func assertPromptCarriesNoSecret(t errorfer, task, prompt string, secrets []string) {
	t.Helper()
	for _, leaked := range leakedTokens(prompt, secrets) {
		t.Errorf("%s prompt contains %q, which is part of its own answer, so it cannot separate an arm that found the answer from one that read the prompt", task, leaked)
	}
}

type recordingT struct{ errors []string }

func (r *recordingT) Helper() {}

func (r *recordingT) Errorf(format string, args ...any) {
	r.errors = append(r.errors, fmt.Sprintf(format, args...))
}

func TestTheLeakDetectorFailsOnAConstructedLeakingItem(t *testing.T) {
	rec := &recordingT{}
	assertPromptCarriesNoSecret(rec, "v-constructed", "Return the exact code withheldByPlan when the plan denies the export.", []string{"withheldByPlan"})
	if len(rec.errors) == 0 {
		t.Fatal("a prompt that quotes withheldByPlan verbatim passed the detector, so it would not have caught a real leak either")
	}
	t.Logf("detector correctly failed the constructed leak: %s", rec.errors[0])
}
