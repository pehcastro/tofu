package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestSiftIsRoutedAndListed(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"sift", "--arm", "brevity"}, strings.NewReader("## A heading\n\nthe answer is four\n"), &out, &errOut); code != exitOK {
		t.Fatalf("tofu sift exited %d: %s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "--- sift ---") {
		t.Fatalf("tofu sift printed no sift output:\n%s", out.String())
	}

	out.Reset()
	if code := run([]string{"help"}, strings.NewReader(""), &out, &errOut); code != exitOK {
		t.Fatalf("tofu help exited %d", code)
	}
	if !strings.Contains(out.String(), "\n  sift ") {
		t.Fatal("the usage list does not name sift")
	}
	if !strings.Contains(out.String(), "\n  changelog ") {
		t.Fatal("the usage list does not name changelog")
	}
}
