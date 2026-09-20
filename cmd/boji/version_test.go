package main

import (
	"bytes"
	"strings"
	"testing"

	"boji/internal/konst"
)

func TestVersionPrintsTheNumberTheTreeHoldsAndNotDev(t *testing.T) {
	var out bytes.Buffer
	if code := version(&out); code != exitOK {
		t.Fatalf("version exited %d", code)
	}
	line, _, _ := strings.Cut(out.String(), "\n")
	if !strings.HasPrefix(line, "version: "+konst.Version) {
		t.Fatalf("version reads %q, want it to lead with %s", line, konst.Version)
	}
}
