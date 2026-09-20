package quota

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type stubCredential struct {
	token string
	err   error
}

func (s stubCredential) Access(context.Context) (string, error) {
	return s.token, s.err
}

func stubCodexPoller(t *testing.T, handler http.HandlerFunc) *Poller {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	poller, err := NewPoller(server.Client(), func() time.Time { return recordedNow },
		map[Provider]string{Codex: server.URL})
	if err != nil {
		t.Fatalf("building the poller: %v", err)
	}
	return poller
}

func TestTheUsageEndpointIsPolledOnceAndNeverRetriedInsideOneCall(t *testing.T) {
	var hits atomic.Int64
	poller := stubCodexPoller(t, func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.Header.Get("Authorization") != "Bearer token" {
			t.Errorf("the poll carried %q", r.Header.Get("Authorization"))
		}
		http.Error(w, "rate_limit", http.StatusTooManyRequests)
	})
	account := Account{Provider: Codex, Credential: stubCredential{token: "token"}}
	if _, err := poller.Poll(context.Background(), account); err == nil {
		t.Fatal("a throttled usage endpoint reported success")
	}
	if got := hits.Load(); got != 1 {
		t.Fatalf("the usage endpoint was called %d times, want one, retrying only deepens the throttle", got)
	}
}

func TestTheCacheAnswersASecondPollInsideTheInterval(t *testing.T) {
	var hits atomic.Int64
	poller := stubCodexPoller(t, func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		_, _ = w.Write([]byte(`{"plan_type":"Plus","rate_limit":{"limit_reached":false,` +
			`"primary_window":{"used_percent":12,"limit_window_seconds":18000}}}`))
	})
	account := Account{Provider: Codex, Credential: stubCredential{token: "token"}}
	first, err := poller.Poll(context.Background(), account)
	if err != nil {
		t.Fatalf("the first poll failed: %v", err)
	}
	second, err := poller.Poll(context.Background(), account)
	if err != nil {
		t.Fatalf("the second poll failed: %v", err)
	}
	if hits.Load() != 1 {
		t.Fatalf("the usage endpoint was called %d times inside the interval, want one", hits.Load())
	}
	if len(second.Windows) != len(first.Windows) {
		t.Fatalf("the cached report lost windows: %+v", second)
	}
}

func TestDoctorSeparatesASpentWindowFromABrokenCredential(t *testing.T) {
	spentPoller := stubCodexPoller(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(recordedBody(t, "codex-usage.json"))
	})
	spent, err := spentPoller.Poll(context.Background(), Account{Provider: Codex, Credential: stubCredential{token: "token"}})
	if err != nil {
		t.Fatalf("polling the spent account: %v", err)
	}
	if got := Diagnose(spent, nil); got != ConditionWindowSpent {
		t.Fatalf("a spent window diagnosed as %d, want a spent window", got)
	}
	if until, waits := spent.WaitUntil(recordedNow); !waits || !until.After(recordedNow) {
		t.Fatalf("a spent window reports no time to wait until: %v %v", until, waits)
	}

	brokenPoller := stubCodexPoller(t, func(http.ResponseWriter, *http.Request) {
		t.Error("a broken credential still reached the usage endpoint")
	})
	brokenAccount := Account{Provider: Codex, Credential: stubCredential{err: errors.New("cred: the codex credential is disabled")}}
	broken, err := brokenPoller.Poll(context.Background(), brokenAccount)
	if err == nil {
		t.Fatal("a broken credential polled successfully")
	}
	if got := Diagnose(broken, err); got != ConditionCredentialBroken {
		t.Fatalf("a broken credential diagnosed as %d, want a broken credential", got)
	}
	if _, waits := broken.WaitUntil(recordedNow); waits {
		t.Fatal("a broken credential is reported as a window worth waiting out")
	}
}

func TestDoctorSaysTheSpendLimitBelongsToTheProvider(t *testing.T) {
	line := SpendLimitLine()
	if !strings.Contains(line, "tofu sets none") || !strings.Contains(line, "the account that issued the key") {
		t.Fatalf("the spend limit line read %q", line)
	}
}

func TestAPollReportsNoAccountIdentifierAndNoToken(t *testing.T) {
	poller := stubCodexPoller(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(recordedBody(t, "codex-usage.json"))
	})
	account := Account{Provider: Codex, AccountID: "acct-secret-uuid", Credential: stubCredential{token: "token-secret"}}
	report, err := poller.Poll(context.Background(), account)
	if err != nil {
		t.Fatalf("polling: %v", err)
	}
	whole := fmt.Sprintf("%+v", report)
	if strings.Contains(whole, account.AccountID) || strings.Contains(whole, "token-secret") {
		t.Fatalf("the report carried an identifier or a token:\n%s", whole)
	}
	if report.Plan != "Plus" || len(report.Windows) != 2 {
		t.Fatalf("the recorded body read as %+v", report)
	}
	if report.Windows[0].ID != "5h" || report.Windows[0].Used.Fraction != 1 {
		t.Fatalf("the spent window read as %+v", report.Windows[0])
	}
	if report.Windows[1].ID != "7d" || report.Windows[1].Used.Fraction != 0.425 {
		t.Fatalf("the second window read as %+v", report.Windows[1])
	}
}
