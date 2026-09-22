package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"tofu/interface/tui/theme"
	"tofu/internal/llm/cred"
	"tofu/internal/llm/quota"
	"tofu/internal/sys"
	"tofu/internal/widget"
)

const (
	firstEmail  = "luiz@example.test"
	secondEmail = "work@example.test"
)

func flat(text string) string { return strings.Join(strings.Fields(text), " ") }

func vendorStubs(t *testing.T, now time.Time) map[quota.Provider]string {
	t.Helper()
	urls := make(map[quota.Provider]string, 2)
	for provider, body := range map[quota.Provider]string{
		quota.Anthropic: `{"five_hour":{"utilization":25,"resets_at":"` +
			now.Add(3*time.Hour).UTC().Format(time.RFC3339) + `"}}`,
		quota.Codex: `{"plan_type":"pro","rate_limit":{"primary_window":` +
			`{"used_percent":13,"limit_window_seconds":604800,"reset_after_seconds":7200}}}`,
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(body))
		}))
		t.Cleanup(server.Close)
		urls[provider] = server.URL
	}
	return urls
}

func storeLogins(t *testing.T, logins ...cred.Credential) {
	t.Helper()
	path, err := cred.Path()
	if err != nil {
		t.Fatal(err)
	}
	store, err := cred.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	for _, login := range logins {
		if err := store.Save(login, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
}

func anthropicLogin(account, email string) cred.Credential {
	now := time.Now()
	return cred.Credential{
		Provider:   cred.Anthropic,
		Kind:       cred.KindOAuth,
		Access:     testAccess,
		Expires:    now.Add(time.Hour),
		Identity:   cred.Identity{AccountID: account, Email: email},
		Authorized: now,
	}
}

func codexLogin(account string) cred.Credential {
	now := time.Now()
	return cred.Credential{
		Provider:   cred.Codex,
		Kind:       cred.KindOAuth,
		Access:     testAccess,
		Expires:    now.Add(time.Hour),
		Identity:   cred.Identity{AccountID: account},
		Authorized: now,
	}
}

func statusOf(t *testing.T, args ...string) string {
	t.Helper()
	now := time.Now()
	var out, errOut bytes.Buffer
	if code := statusVerb(args, &out, &errOut, plain, now, vendorStubs(t, now)); code != exitOK {
		t.Fatalf("status %v = %d, %q", args, code, errOut.String())
	}
	return out.String()
}

func statusLine(t *testing.T, listing, contains string) string {
	t.Helper()
	for _, line := range strings.Split(listing, "\n") {
		if strings.Contains(line, contains) {
			return line
		}
	}
	t.Fatalf("no line carries %q:\n%s", contains, listing)
	return ""
}

func TestTheCredentialStoreATestSeesIsNeverTheOwners(t *testing.T) {
	owner := sys.OwnerHomeStateDir()
	if owner == "" {
		t.Skip("this machine's home is already inside the temp directory, so there is no owner store to be kept off")
	}
	redirected, err := cred.Path()
	if err != nil {
		t.Fatal(err)
	}
	emptyHome(t)
	isolated, err := cred.Path()
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{redirected, isolated} {
		if strings.HasPrefix(filepath.Clean(path), filepath.Clean(owner)) {
			t.Fatalf("a test would read %s, which is under the owner's store at %s", path, owner)
		}
		if !strings.HasPrefix(filepath.Clean(path), filepath.Clean(os.TempDir())) {
			t.Fatalf("a test would read %s, which is outside %s", path, os.TempDir())
		}
	}
}

func TestTwoAccountsOnOneSubscriptionAreToldApartByTheirEmail(t *testing.T) {
	emptyHome(t)
	storeLogins(t,
		anthropicLogin("account-uuid-a", firstEmail),
		anthropicLogin("account-uuid-b", secondEmail))

	if widget.Mask(firstEmail) != widget.Mask(secondEmail) {
		t.Fatal("the two emails do not mask alike, so nothing here is proved")
	}
	listing := statusOf(t)
	first, second := statusLine(t, listing, "#1"), statusLine(t, listing, "#2")
	if !strings.Contains(first, firstEmail) || !strings.Contains(second, secondEmail) {
		t.Fatalf("the two accounts are not named by their email:\n%s", listing)
	}
	if first == second {
		t.Fatalf("the two account rows read the same:\n%s", listing)
	}
}

func TestTheListingCarriesTheSubscriptionThePlanAndTheWindows(t *testing.T) {
	emptyHome(t)
	storeLogins(t, anthropicLogin("account-uuid-a", firstEmail), codexLogin("codex-account-uuid"))

	listing := statusOf(t)
	for _, want := range []string{
		"claude-sub",
		"codex-sub",
		"not reported by anthropic",
		"pro",
		"25%",
		"13%",
		"resets in",
	} {
		if !strings.Contains(listing, want) {
			t.Errorf("the listing is missing %q:\n%s", want, listing)
		}
	}
	t.Log("\n" + listing)
}

func TestAStateThatNeedsAttentionReadsDifferentlyFromOneThatDoesNot(t *testing.T) {
	emptyHome(t)
	storeLogins(t,
		anthropicLogin("account-uuid-a", firstEmail),
		anthropicLogin("account-uuid-b", secondEmail))
	if _, errOut, code := runLogin(t, "--disable", "1"); code != exitOK {
		t.Fatalf("disable = %d, %q", code, errOut)
	}

	now := time.Now()
	report, err := credentialStatus(now, false, vendorStubs(t, now))
	if err != nil {
		t.Fatal(err)
	}
	accounts := report.Sources[0].Accounts
	if len(accounts) != 2 {
		t.Fatalf("one subscription holds %d accounts, want 2", len(accounts))
	}
	aside, serving := accounts[0], accounts[1]
	if !aside.Attention || serving.Attention {
		t.Fatalf("attention reads %v and %v, want the set aside account alone", aside.Attention, serving.Attention)
	}
	if aside.State == serving.State {
		t.Fatalf("both accounts read %q", aside.State)
	}

	painted := statusText(report, coloured, now)
	asideCell := statusLine(t, painted, "#1")
	servingCell := statusLine(t, painted, "#2")
	if !strings.Contains(asideCell, theme.Warn().Render(aside.State)) {
		t.Fatalf("the account that needs attention is not drawn as a warning: %q", asideCell)
	}
	if strings.Contains(servingCell, theme.Warn().Render(serving.State)) {
		t.Fatalf("the serving account is drawn as a warning: %q", servingCell)
	}
}

func TestTwoUsableAccountsSayWhyEveryTurnRefusesAndWhatFixesIt(t *testing.T) {
	emptyHome(t)
	storeLogins(t,
		anthropicLogin("account-uuid-a", firstEmail),
		anthropicLogin("account-uuid-b", secondEmail))

	listing := statusOf(t)
	if !strings.Contains(flat(listing), "so every turn refuses") {
		t.Fatalf("the listing does not say a turn will refuse:\n%s", listing)
	}
	if !strings.Contains(listing, "run tofu login --disable 1") {
		t.Fatalf("the listing names no command that fixes it:\n%s", listing)
	}
	t.Log("\n" + listing)
	first, _, _ := strings.Cut(listing, "\n")
	if !strings.Contains(first, "claude-sub refuses every turn") {
		t.Fatalf("the headline hides the refusal: %q", first)
	}

	if _, errOut, code := runLogin(t, "--disable", "1"); code != exitOK {
		t.Fatalf("disable = %d, %q", code, errOut)
	}
	if after := statusOf(t); strings.Contains(flat(after), "so every turn refuses") {
		t.Fatalf("one account is set aside and the listing still refuses:\n%s", after)
	}
}

func TestRedactionIsBehindAFlagAndIsNotTheDefault(t *testing.T) {
	emptyHome(t)
	storeLogins(t, anthropicLogin("account-uuid-a", firstEmail))

	shown := statusOf(t)
	if !strings.Contains(shown, firstEmail) {
		t.Fatalf("the default listing hides the email:\n%s", shown)
	}
	hidden := statusOf(t, redactFlag)
	if strings.Contains(hidden, firstEmail) {
		t.Fatalf("%s left the email in:\n%s", redactFlag, hidden)
	}
	if !strings.Contains(hidden, maskedAccount(cred.Identity{Email: firstEmail})) {
		t.Fatalf("%s did not render the mask:\n%s", redactFlag, hidden)
	}
	for _, secret := range []string{testAccess, "account-uuid-a"} {
		if strings.Contains(shown, secret) || strings.Contains(hidden, secret) {
			t.Fatalf("the listing printed %q", secret)
		}
	}
}

func TestAnAccountWithNoIdentityDoesNotRenderAsAnEmptyColumn(t *testing.T) {
	emptyHome(t)
	storeLogins(t, codexLogin(""))

	for _, args := range [][]string{nil, {redactFlag}} {
		line := statusLine(t, statusOf(t, args...), "#1")
		if !strings.Contains(line, noAccountYet) {
			t.Fatalf("an account with no identity reads %q", line)
		}
	}
}

func TestStatusJSONCarriesEveryFieldTheTextDoes(t *testing.T) {
	emptyHome(t)
	storeLogins(t,
		anthropicLogin("account-uuid-a", firstEmail),
		anthropicLogin("account-uuid-b", secondEmail))

	var report statusReport
	if err := json.Unmarshal([]byte(statusOf(t, jsonFlag)), &report); err != nil {
		t.Fatalf("tofu login --status --json does not parse: %v", err)
	}
	text := statusOf(t)
	carries := func(want string) {
		t.Helper()
		if want != "" && !strings.Contains(flat(text), flat(want)) {
			t.Errorf("the text drops %q:\n%s", want, text)
		}
	}
	if report.ReportedAt.IsZero() {
		t.Fatal("the json carries no moment")
	}
	carries(report.Headline)
	carries(report.Gate)
	for _, source := range report.Sources {
		carries(source.Subscription)
		carries(source.Refusal)
		carries(source.Fix)
		for _, account := range source.Accounts {
			carries("#" + strconv.FormatInt(account.ID, 10))
			carries(account.Account)
			carries(account.Login)
			carries(account.Plan)
			carries(account.State)
			for _, window := range account.Windows {
				if window.Reported {
					carries(window.ID)
					carries(window.percent())
				}
			}
		}
	}
}

func TestStatusRefusesAnUnknownFlag(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := statusVerb([]string{"--all"}, &out, &errOut, plain, time.Now(), nil); code != exitUsage {
		t.Fatalf("an unknown flag gave exit %d, want %d", code, exitUsage)
	}
	if !strings.Contains(errOut.String(), statusFlags) {
		t.Fatalf("the refusal read %q", errOut.String())
	}
}

func TestAStoredCredentialIsNeverPolledAgainstAVendorInATest(t *testing.T) {
	emptyHome(t)
	storeLogins(t, anthropicLogin("account-uuid-a", firstEmail))

	report, err := credentialStatus(time.Now(), false, nil)
	if err != nil {
		t.Fatal(err)
	}
	state := report.Sources[0].Accounts[0].State
	if !strings.Contains(state, noVendorInATest) {
		t.Fatalf("a test with no stub url read %q, so something reached a vendor", state)
	}
}
