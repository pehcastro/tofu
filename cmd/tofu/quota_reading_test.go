package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"tofu/bench/picker"
	"tofu/internal/llm/quota"
)

var polledAt = time.Date(2026, 9, 21, 15, 4, 5, 0, time.UTC)

func readingsAfterPolling(t *testing.T) (picker.Corpus, []byte) {
	t.Helper()
	t.Chdir(t.TempDir())
	store := storeWithTwoAnthropicAccounts(t, time.Now())
	rows, err := store.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	url, _ := usageStub(t)
	results, err := pollRows(context.Background(), store, rows,
		func() time.Time { return polledAt }, map[quota.Provider]string{quota.ClaudeSub: url})
	if err != nil {
		t.Fatalf("pollRows: %v", err)
	}
	for _, result := range results {
		if result.recordErr != nil {
			t.Fatalf("recording a reading failed: %v", result.recordErr)
		}
		if result.err != nil || result.unusable != "" {
			t.Fatalf("an account was not polled: %v %s", result.err, result.unusable)
		}
	}
	dir, err := quotaReadingDir()
	if err != nil {
		t.Fatalf("locating the reading directory: %v", err)
	}
	corpus, err := picker.Gather(dir)
	if err != nil {
		t.Fatalf("gathering the written readings: %v", err)
	}
	var written []byte
	entries, _ := os.ReadDir(dir)
	for _, entry := range entries {
		body, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			t.Fatalf("reading %s: %v", entry.Name(), err)
		}
		written = append(written, body...)
	}
	return corpus, written
}

func TestEachPolledAccountWritesOneReadingABenchCanRead(t *testing.T) {
	corpus, written := readingsAfterPolling(t)

	if len(corpus.Readings) != 2 {
		t.Fatalf("two polled accounts left %d readings in %v, so the picker's arm still has nothing to score:\n%s",
			len(corpus.Readings), corpus.Roots, written)
	}
	if len(corpus.Skips) != 0 {
		t.Fatalf("the bench reader skipped a written row: %+v", corpus.Skips)
	}
	if corpus.Accounts != 2 {
		t.Fatalf("the two rows name %d accounts, so one run's accounts are not distinguishable", corpus.Accounts)
	}
	first := corpus.Readings[0]
	if first.Row != 1 || corpus.Readings[1].Row != 2 {
		t.Fatalf("the rows name accounts %d and %d, want the credential row numbers 1 and 2",
			first.Row, corpus.Readings[1].Row)
	}
	if first.Report.Provider != quota.ClaudeSub || !first.Report.FetchedAt.Equal(polledAt) {
		t.Fatalf("the row reads as provider %q fetched at %v", first.Report.Provider, first.Report.FetchedAt)
	}
	if len(first.Report.Windows) != 1 {
		t.Fatalf("the row carries %d windows, want the one the vendor reported", len(first.Report.Windows))
	}
	window := first.Report.Windows[0]
	resets := time.Date(2026, 9, 21, 18, 0, 0, 0, time.UTC)
	if window.ID != "5h" || window.Used.Fraction != 0.25 || !window.ResetsAt.Equal(resets) {
		t.Fatalf("the window reads as %+v", window)
	}
	t.Logf("\n%s", written)
}

func TestAnUnrecordedReadingIsSaidOnTheLineTheAccountAlreadyHas(t *testing.T) {
	serving := quota.Report{
		Provider:  quota.ClaudeSub,
		FetchedAt: polledAt,
		Windows:   []quota.Window{{ID: "5h", Used: quota.Used{Fraction: 0.25, Reported: true}}},
	}
	refused := errors.New("the reading directory is read only")

	state := credentialState(pollResult{report: serving, recordErr: refused}, polledAt)
	if !strings.HasPrefix(state, usageServingState) || !strings.Contains(state, refused.Error()) {
		t.Fatalf("an account whose reading was not written reads %q, which hides it", state)
	}
	if plain := credentialState(pollResult{report: serving}, polledAt); plain != usageServingState {
		t.Fatalf("an account whose reading was written reads %q", plain)
	}
}

func TestAWrittenReadingCarriesNoTokenNoAccountIdAndNoEmail(t *testing.T) {
	_, written := readingsAfterPolling(t)
	if len(written) == 0 {
		t.Fatal("polling two accounts wrote no bytes, so there is nothing to grep for a secret")
	}

	for _, secret := range []string{
		stubAccess(firstStubAccount),
		firstStubAccount,
		stubEmail(firstStubAccount),
		"not-a-real-refresh-" + firstStubAccount,
	} {
		if strings.Contains(string(written), secret) {
			t.Fatalf("a written reading carries %q:\n%s", secret, written)
		}
	}
}
