package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"tofu/internal/session"
)

const hologram = session.EventKind("hologram")

func sessionWith(t *testing.T, dir, id string, kinds ...session.EventKind) {
	t.Helper()
	store := session.NewStore(dir)
	var events []session.Event
	for i, kind := range kinds {
		body := json.RawMessage(`{}`)
		switch kind {
		case session.EventStep:
			body = json.RawMessage(`{"index":1,"assistant_text":"looking"}`)
		case session.EventMessage:
			body = json.RawMessage(`{"role":"user","content":"go on"}`)
		case session.EventOutcome:
			body = json.RawMessage(`{"id":"` + id + `","outcome":"stopped"}`)
		}
		events = append(events, session.Event{ID: id + "-" + string(kind), Attempt: i + 1, Kind: kind, Body: body})
	}
	header := session.Header{ID: id, Root: id, At: time.Date(2026, 9, 24, 9, 0, 0, 0, time.UTC)}
	if err := store.Write(header, events); err != nil {
		t.Fatalf("writing the fixture session %s: %v", id, err)
	}
}

func TestCorpusNamesTheKindNothingReadsAndTheSessionCarryingIt(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sessions")
	sessionWith(t, dir, "turn-clean", session.EventStep, session.EventMessage, session.EventOutcome)
	sessionWith(t, dir, "turn-thinned", session.EventStep, hologram, session.EventOutcome)

	out, errOut := &bytes.Buffer{}, &bytes.Buffer{}
	if code := run([]string{"corpus", "--dir", dir}, out, errOut); code != exitOK {
		t.Fatalf("exit code = %d, want %d, stderr %q", code, exitOK, errOut.String())
	}
	printed := out.String()
	for _, want := range []string{"sessions 2", string(hologram), "turn-thinned", "thinner than what was recorded"} {
		if !strings.Contains(printed, want) {
			t.Fatalf("the corpus report does not carry %q:\n%s", want, printed)
		}
	}
	if !strings.Contains(printed, "turn-clean  2026-09-24T09:00:00Z  events  steps 1  messages 1  reads 0  every kind read") {
		t.Fatalf("the clean session is not reported as fully read:\n%s", printed)
	}
	t.Logf("bench corpus over a fixture:\n%s", printed)
}

func TestCorpusSaysNothingWasSkippedWhenEveryKindIsRead(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sessions")
	sessionWith(t, dir, "turn-clean", session.EventStep, session.EventMessage, session.EventOutcome)

	out, errOut := &bytes.Buffer{}, &bytes.Buffer{}
	if code := run([]string{"corpus", "--dir", dir}, out, errOut); code != exitOK {
		t.Fatalf("exit code = %d, want %d, stderr %q", code, exitOK, errOut.String())
	}
	if !strings.Contains(out.String(), "nothing was skipped") {
		t.Fatalf("a corpus with no unknown kind does not say so:\n%s", out.String())
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestCorpusOpensNoWireAndNeedsNoKey(t *testing.T) {
	t.Chdir(t.TempDir())
	previous := http.DefaultTransport
	http.DefaultTransport = roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		t.Fatalf("bench corpus opened a wire to %s, and it must open none", request.URL.Host)
		return nil, nil
	})
	t.Cleanup(func() { http.DefaultTransport = previous })

	dir := filepath.Join(t.TempDir(), "sessions")
	sessionWith(t, dir, "turn-clean", session.EventStep, session.EventOutcome)

	out, errOut := &bytes.Buffer{}, &bytes.Buffer{}
	if code := run([]string{"corpus", "--dir", dir}, out, errOut); code != exitOK {
		t.Fatalf("exit code = %d, want %d, stderr %q", code, exitOK, errOut.String())
	}
	if !strings.Contains(out.String(), "no model is called and no wire is opened") {
		t.Fatalf("the report does not state that it called nothing:\n%s", out.String())
	}
	t.Log("no round trip was attempted, and the working directory held no .env for a key to be read from")
}

func TestSkippedEventGapsNamesTheKindASpendingRunWouldMiss(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sessions")
	sessionWith(t, dir, "turn-thinned", session.EventStep, hologram, session.EventOutcome)

	gaps := skippedEventGaps(dir, "turn-thinned")
	if len(gaps) != 1 {
		t.Fatalf("gaps = %v, want exactly one naming the skipped kind", gaps)
	}
	for _, want := range []string{string(hologram), "turn-thinned", "thinner"} {
		if !strings.Contains(gaps[0], want) {
			t.Fatalf("the gap does not carry %q: %q", want, gaps[0])
		}
	}
	t.Logf("a spending run would print this gap: %s", gaps[0])
}

func TestSkippedEventGapsIsSilentWhenEveryKindWasRead(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sessions")
	sessionWith(t, dir, "turn-clean", session.EventStep, session.EventOutcome)

	if gaps := skippedEventGaps(dir, "turn-clean"); gaps != nil {
		t.Fatalf("gaps = %v, want none, every kind in that session is read", gaps)
	}
}

func TestCorpusRefusesAnUnknownArgument(t *testing.T) {
	out, errOut := &bytes.Buffer{}, &bytes.Buffer{}
	if code := run([]string{"corpus", "--depth", "2"}, out, errOut); code != exitUsage {
		t.Fatalf("exit code = %d, want %d", code, exitUsage)
	}
	if !strings.Contains(errOut.String(), `"--depth"`) {
		t.Fatalf("stderr does not name the argument: %q", errOut.String())
	}
}
