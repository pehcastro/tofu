package main

import (
	"bytes"
	"strconv"
	"strings"
	"testing"
	"time"

	"tofu/internal/judge/ledger"
	"tofu/internal/konst"
	"tofu/internal/sys"
)

func whyText(t *testing.T, id string) string {
	t.Helper()
	var out, errOut bytes.Buffer
	if code := whyVerb([]string{id}, &out, &errOut, time.Now); code != exitOK {
		t.Fatalf("exit = %d, stderr %s", code, errOut.String())
	}
	return out.String()
}

func TestWhyReadsAnElidedStateBackAndItReadsLikeAnInlineOne(t *testing.T) {
	t.Chdir(t.TempDir())
	dir, err := sys.LogDir()
	if err != nil {
		t.Fatalf("LogDir: %v", err)
	}
	small := `{"agent":"tofu","input":{"command":"git status"},"tool":"bash"}`
	inline, _ := writeFixtureLedger(t, dir, func(r *ledger.Row) { r.State = []byte(small) })
	if inline.StateElision != nil {
		t.Fatalf("a %d byte state was elided", len(small))
	}
	large := `{"agent":"tofu","input":{"command":"git log ` + strings.Repeat("z", 8*1024) + `"},"tool":"bash"}`
	elided, _ := writeFixtureLedger(t, dir, func(r *ledger.Row) { r.State = []byte(large) })
	if elided.StateElision == nil {
		t.Fatalf("a %d byte state was not elided", len(large))
	}

	inlineText, elidedText := whyText(t, inline.ID), whyText(t, elided.ID)
	if !strings.Contains(inlineText, "  state      "+strconv.Itoa(len(small))+" bytes\n    "+small) {
		t.Fatalf("an inline state is not printed whole, got:\n%s", inlineText)
	}
	if !strings.Contains(elidedText, large[:konst.WhyStateBytes]) {
		t.Fatalf("the elided state was not read back from its file, got:\n%s", elidedText)
	}
	if strings.Contains(elidedText, large) {
		t.Fatalf("the whole %d byte state was printed with no bound", len(large))
	}
	for _, want := range []string{"first " + strconv.Itoa(konst.WhyStateBytes) + " shown", "rest     tofu why " + elided.ID + " --state"} {
		if !strings.Contains(elidedText, want) {
			t.Fatalf("the bounded state line is missing %q, got:\n%s", want, elidedText)
		}
	}
	t.Logf("inline:\n%s", inlineText)
	t.Logf("elided:\n%.900s", elidedText)
}

func TestWhySaysAStateFileIsMissingRatherThanPrintingAnEmptyState(t *testing.T) {
	t.Chdir(t.TempDir())
	dir, err := sys.LogDir()
	if err != nil {
		t.Fatalf("LogDir: %v", err)
	}
	row, _ := writeFixtureLedger(t, dir, func(r *ledger.Row) {
		r.State = nil
		r.StateElision = &ledger.StateElision{Bytes: 9001, Head: `{"tool":"bash"`, Tail: `"}`, File: "states/never-written.json"}
	})

	text := whyText(t, row.ID)
	for _, want := range []string{"9001 bytes recorded", "is not on disk", "never-written.json", `{"tool":"bash"`} {
		if !strings.Contains(text, want) {
			t.Fatalf("a row whose state file is gone must say so, missing %q, got:\n%s", want, text)
		}
	}
	if strings.Contains(text, "refused") {
		t.Fatalf("a missing file must not read as a refusal, got:\n%s", text)
	}
	t.Logf("%s", text)
}

func TestWhyRefusesAStateFileOutsideTheLedgerAndSaysWhy(t *testing.T) {
	t.Chdir(t.TempDir())
	dir, err := sys.LogDir()
	if err != nil {
		t.Fatalf("LogDir: %v", err)
	}
	row, _ := writeFixtureLedger(t, dir, func(r *ledger.Row) {
		r.State = nil
		r.StateElision = &ledger.StateElision{Bytes: 9001, Head: `{"tool":"bash"`, Tail: `"}`, File: "../../pretend-secret.txt"}
	})

	text := whyText(t, row.ID)
	for _, want := range []string{"refused", "outside the ledger", "../../pretend-secret.txt", dir} {
		if !strings.Contains(text, want) {
			t.Fatalf("a refused state must say why, missing %q, got:\n%s", want, text)
		}
	}
	if strings.Contains(text, "is not on disk") {
		t.Fatalf("a refusal must not read as a missing file, got:\n%s", text)
	}
	t.Logf("%s", text)
}
