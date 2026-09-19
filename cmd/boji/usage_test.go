package main

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func isolateHome(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
}

func TestUsageRefusesAnArgument(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := usageVerb([]string{"anthropic"}, &out, &errOut); code != exitUsage {
		t.Fatalf("an argument gave exit %d, want %d", code, exitUsage)
	}
	if !strings.Contains(errOut.String(), "usage: boji usage") {
		t.Fatalf("the error read %q", errOut.String())
	}
}

func TestUsageWithoutACredentialNamesWhoOwnsTheSpendLimit(t *testing.T) {
	isolateHome(t)
	var out, errOut bytes.Buffer
	if code := usageVerb(nil, &out, &errOut); code != exitOK {
		t.Fatalf("exit %d, stderr %q", code, errOut.String())
	}
	printed := out.String()
	if !strings.Contains(printed, "no subscription credential") {
		t.Fatalf("the output read %q", printed)
	}
	if !strings.Contains(printed, "an api key's spending limit is the provider's") {
		t.Fatalf("the spend limit line is missing from %q", printed)
	}
}

func TestDoctorLinesCarryTheQuotaStateAndTheSpendLimit(t *testing.T) {
	isolateHome(t)
	lines := quotaDoctorLines(time.Now())
	if len(lines) == 0 {
		t.Fatal("no doctor line")
	}
	last := lines[len(lines)-1]
	if !strings.Contains(last, "spend limit: boji sets none") {
		t.Fatalf("the last doctor line read %q", last)
	}
}
