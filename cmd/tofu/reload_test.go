package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestReloadVerbSaysWhatItCannotReload(t *testing.T) {
	isolatedHomeAndProject(t)
	var out, errOut bytes.Buffer
	if code := reloadVerb(&out, &errOut); code != exitOK {
		t.Fatalf("reload exited %d: %s", code, errOut.String())
	}
	if out.String() == "" {
		t.Fatal("reload printed nothing")
	}
	for _, want := range []string{"skills", "hooks"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("reload output must name %q as not reloaded\n%s", want, out.String())
		}
	}
}
