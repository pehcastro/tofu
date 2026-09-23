package main

import (
	"bytes"
	"strings"
	"testing"
)

var loadersThatDoNotExist = []string{
	"skills: no skill loader exists in the tree yet",
	"hooks: no hook loader exists in the tree yet",
}

func TestReloadNamesNoLoaderThatDoesNotExist(t *testing.T) {
	isolatedHomeAndProject(t)
	var out, errOut bytes.Buffer
	if code := reloadVerb(&out, &errOut); code != exitOK {
		t.Fatalf("reload exited %d: %s", code, errOut.String())
	}
	if !strings.HasPrefix(out.String(), "reloaded ") {
		t.Fatalf("reload must say what it re-read, got %q", out.String())
	}
	for _, absent := range loadersThatDoNotExist {
		if strings.Contains(out.String(), absent) {
			t.Fatalf("reload names a loader tofu does not have: %q\n%s", absent, out.String())
		}
	}
}

func TestAppReloadNamesNoLoaderThatDoesNotExist(t *testing.T) {
	isolatedHomeAndProject(t)
	summary := appReload()
	for _, absent := range loadersThatDoNotExist {
		if strings.Contains(summary, absent) {
			t.Fatalf("the app reload summary names a loader tofu does not have: %q\n%s", absent, summary)
		}
	}
}
