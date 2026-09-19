package quota

import (
	"bufio"
	"bytes"
	"net/http"
	"net/textproto"
	"os"
	"path/filepath"
	"testing"
	"time"
)

var recordedNow = time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC)

func recordedHeaders(t *testing.T, name string) http.Header {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("reading the recorded headers: %v", err)
	}
	fields, err := textproto.NewReader(bufio.NewReader(bytes.NewReader(append(raw, '\n')))).ReadMIMEHeader()
	if err != nil {
		t.Fatalf("parsing the recorded headers: %v", err)
	}
	return http.Header(fields)
}

func recordedBody(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("reading the recorded body: %v", err)
	}
	return raw
}

func windowByID(t *testing.T, report Report, id string) Window {
	t.Helper()
	for _, window := range report.Windows {
		if window.ID == id {
			return window
		}
	}
	t.Fatalf("no %q window in %v", id, report.Windows)
	return Window{}
}

func TestRecordedHeadersCarryTheWindowsAndAMissingOneStaysUnknown(t *testing.T) {
	anthropic := FromHeaders(Anthropic, recordedHeaders(t, "anthropic-headers.txt"), recordedNow)
	if got := windowByID(t, anthropic, "5h").Used; !got.Reported || got.Fraction != 0.02 {
		t.Fatalf("the anthropic 5h utilization read %+v, want 0.02 reported", got)
	}
	if got := windowByID(t, anthropic, "5h").ResetsAt; !got.Equal(time.Unix(1780405800, 0).UTC()) {
		t.Fatalf("the anthropic 5h reset read %v", got)
	}
	if got := windowByID(t, anthropic, "7d").Used.Fraction; got != 0.3 {
		t.Fatalf("the anthropic 7d utilization read %v, want 0.3", got)
	}
	openWeights := windowByID(t, anthropic, "7d_oi")
	if openWeights.Used.Reported || openWeights.State() != StateUnknown {
		t.Fatalf("a header-less utilization read %+v, want unknown rather than zero", openWeights)
	}

	codex := FromHeaders(Codex, recordedHeaders(t, "codex-headers.txt"), recordedNow)
	primary := windowByID(t, codex, "1h")
	if !primary.Used.Reported || primary.Used.Fraction != 0.99 {
		t.Fatalf("the codex primary window read %+v, want 0.99 reported", primary.Used)
	}
	if primary.Duration != time.Hour {
		t.Fatalf("the codex primary duration read %v, want 1h", primary.Duration)
	}
	secondary := windowByID(t, codex, "7d")
	if secondary.Used.Reported || secondary.State() != StateUnknown {
		t.Fatalf("a header-less used percent read %+v, want unknown rather than zero", secondary)
	}
}

func TestOnlyAnExhaustedWindowWithAFutureResetAuthorizesWaiting(t *testing.T) {
	spent := Window{ID: "7d", Used: Used{Fraction: 1, Reported: true}}
	timed := spent
	timed.ResetsAt = recordedNow.Add(2 * time.Hour)

	withReset := Report{Provider: Anthropic, Windows: []Window{timed}}
	until, ok := withReset.WaitUntil(recordedNow)
	if !ok || !until.Equal(timed.ResetsAt) {
		t.Fatalf("an exhausted window with a future reset gave %v %v, want the reset", until, ok)
	}

	withoutReset := Report{Provider: Anthropic, Windows: []Window{spent}}
	if _, ok := withoutReset.WaitUntil(recordedNow); ok {
		t.Fatal("an exhausted window with no reset authorized a wait")
	}
	if !withoutReset.Exhausted() {
		t.Fatal("an exhausted window with no reset stopped reading as exhausted")
	}

	permanentBesideTimed := Report{Provider: Anthropic, Windows: []Window{timed, spent}}
	if _, ok := permanentBesideTimed.WaitUntil(recordedNow); ok {
		t.Fatal("a permanent cap beside a timed window authorized a wait")
	}
}

func TestAQuotaRejectionNamesWhenToReturnAndAnUnreadableOneDoesNot(t *testing.T) {
	named := FromRejection(Codex, http.StatusTooManyRequests, recordedHeaders(t, "codex-headers.txt"),
		recordedBody(t, "codex-rejection.json"), recordedNow)
	if named.Plan != "Plus" {
		t.Fatalf("the rejection plan read %q, want Plus", named.Plan)
	}
	until, ok := named.WaitUntil(recordedNow)
	if !ok || !until.Equal(time.Unix(1780405800, 0).UTC()) {
		t.Fatalf("the rejection named %v %v, want the body reset", until, ok)
	}

	opaque := FromRejection(Codex, http.StatusTooManyRequests, http.Header{}, []byte("upstream connect error"), recordedNow)
	if !opaque.Exhausted() {
		t.Fatal("a 429 with an unreadable body stopped reading as exhausted")
	}
	if _, ok := opaque.WaitUntil(recordedNow); ok {
		t.Fatal("a 429 with nothing parseable guessed a return time")
	}
	if len(opaque.Windows) != 0 {
		t.Fatalf("a 429 with nothing parseable invented %v", opaque.Windows)
	}

	retryAdvised := http.Header{}
	retryAdvised.Set("Retry-After", "120")
	advised := FromRejection(Anthropic, http.StatusTooManyRequests, retryAdvised, []byte("{}"), recordedNow)
	until, ok = advised.WaitUntil(recordedNow)
	if !ok || !until.Equal(recordedNow.Add(2*time.Minute)) {
		t.Fatalf("a retry-after rejection named %v %v, want two minutes on", until, ok)
	}

	notQuota := FromRejection(Anthropic, http.StatusInternalServerError, http.Header{}, []byte("<html>gateway</html>"), recordedNow)
	if notQuota.Exhausted() || len(notQuota.Windows) != 0 {
		t.Fatalf("a non-quota rejection read as a spent window: %+v", notQuota)
	}
}

func TestAReportedContextWindowOverridesTheCatalogOnlyWhenLarger(t *testing.T) {
	if got := ContextWindowTokens(200000, 1000000); got != 1000000 {
		t.Fatalf("a larger reported window gave %d, want 1000000", got)
	}
	if got := ContextWindowTokens(200000, 128000); got != 200000 {
		t.Fatalf("a smaller reported window gave %d, want the catalog's 200000", got)
	}
	if got := ContextWindowTokens(200000, 0); got != 200000 {
		t.Fatalf("an unreported window gave %d, want the catalog's 200000", got)
	}
}

func TestTheUsageEndpointPayloadsCarryEveryWindow(t *testing.T) {
	anthropic, err := FromAnthropicUsage(recordedBody(t, "anthropic-usage.json"), recordedNow)
	if err != nil {
		t.Fatalf("parsing the anthropic usage payload: %v", err)
	}
	if got := windowByID(t, anthropic, "5h").Used.Fraction; got != 0.17 {
		t.Fatalf("the anthropic 5h utilization read %v, want 0.17", got)
	}
	scoped := windowByID(t, anthropic, "7d:fable")
	if scoped.State() != StateExhausted {
		t.Fatalf("an inactive scoped limit at 100 percent read %+v, want exhausted", scoped)
	}

	codex, err := FromCodexUsage(recordedBody(t, "codex-usage.json"), recordedNow)
	if err != nil {
		t.Fatalf("parsing the codex usage payload: %v", err)
	}
	if !codex.Exhausted() {
		t.Fatal("a spent codex plan window read as serving")
	}
	if got := windowByID(t, codex, "5h").ResetsAt; !got.Equal(recordedNow.Add(time.Hour)) {
		t.Fatalf("a reset_after_seconds window read %v, want an hour on", got)
	}
	if got := windowByID(t, codex, "7d").Used.Fraction; got != 0.425 {
		t.Fatalf("the codex secondary window read %v, want 0.425", got)
	}

	funded, err := FromCodexUsage(recordedBody(t, "codex-usage-credits.json"), recordedNow)
	if err != nil {
		t.Fatalf("parsing the credit-funded codex payload: %v", err)
	}
	if funded.Exhausted() {
		t.Fatal("a credit-funded account read as exhausted, which parks a working account until the weekly reset")
	}
}
