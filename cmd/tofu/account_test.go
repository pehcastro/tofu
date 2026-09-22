package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"tofu/internal/llm/cred"
	"tofu/internal/llm/wire/codex"
	"tofu/internal/widget"
)

const (
	firstTestAccount  = "first-account-uuid"
	secondTestAccount = "second-account-uuid"
)

func storeTwoAnthropicAccounts(t *testing.T) {
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
	now := time.Now()
	for _, account := range []string{firstTestAccount, secondTestAccount} {
		credential := cred.Credential{
			Provider:   cred.ClaudeSub,
			Kind:       cred.KindOAuth,
			Access:     testAccess,
			Expires:    now.Add(time.Hour),
			Identity:   cred.Identity{AccountID: account},
			Authorized: now,
		}
		if err := store.Save(credential, now); err != nil {
			t.Fatal(err)
		}
	}
}

func TestLoginStatusListsEveryStoredCredential(t *testing.T) {
	emptyHome(t)
	storeTwoAnthropicAccounts(t)

	out, _, code := runLogin(t, "--status")
	if code != exitOK {
		t.Fatalf("status = %d", code)
	}
	for _, want := range []string{"#1 ", "#2 ", firstTestAccount, secondTestAccount} {
		if !strings.Contains(out, want) {
			t.Errorf("status %q is missing %q", out, want)
		}
	}
	if lines := strings.Count(out, "oauth, expires"); lines != 2 {
		t.Errorf("status describes %d anthropic credentials, want both: %q", lines, out)
	}
	if strings.Contains(out, testAccess) {
		t.Fatal("the listing names a token")
	}
}

func TestTwoUsableCredentialsArePickedBetweenAndSettingOneAsideMovesTheChoice(t *testing.T) {
	emptyHome(t)
	storeTwoAnthropicAccounts(t)

	chosen, err := chosenAccount(t)
	if err != nil {
		t.Fatalf("two usable credentials refused to choose: %v", err)
	}
	if chosen != 1 {
		t.Fatalf("the picker chose #%d, want the first while neither reports a window", chosen)
	}

	out, errOut, code := runLogin(t, "--disable", "1")
	if code != exitOK {
		t.Fatalf("disable = %d, %q", code, errOut)
	}
	if !strings.Contains(out, "#1 ") || !strings.Contains(out, "set aside by hand") {
		t.Fatalf("disable said %q", out)
	}

	aside, err := chosenAccount(t)
	if err != nil {
		t.Fatalf("after setting one aside: %v", err)
	}
	if aside != 2 {
		t.Fatalf("the picker chose #%d while #1 is set aside", aside)
	}

	out, errOut, code = runLogin(t, "--enable", "1")
	if code != exitOK {
		t.Fatalf("enable = %d, %q", code, errOut)
	}
	if strings.Contains(out, "set aside by hand") {
		t.Fatalf("enable said %q", out)
	}
	back, err := chosenAccount(t)
	if err != nil || back != 1 {
		t.Fatalf("the picker chose #%d (%v) once both are usable again", back, err)
	}
}

func chosenAccount(t *testing.T) (int64, error) {
	t.Helper()
	held, _, err := openAccounts(runOpts{wire: wireSubscription}, "anthropic/claude-opus-5")
	if held != nil {
		defer held.close()
	}
	if err != nil {
		return 0, err
	}
	account, err := held.pick(context.Background())
	return account.ID, err
}

func TestTheListingNamesTheAccountAndThePaneStillMasksIt(t *testing.T) {
	emptyHome(t)
	stored := storeCredential(t, cred.ClaudeSub)

	listing, errOut, code := runLogin(t, "--status")
	if code != exitOK {
		t.Fatalf("status = %d, %q", code, errOut)
	}
	redacted, errOut, code := runLogin(t, "--status", redactFlag)
	if code != exitOK {
		t.Fatalf("status %s = %d, %q", redactFlag, code, errOut)
	}
	pane := settingsScreen(t, appProviders())

	shown := maskedAccount(cred.Identity{Email: stored})
	if !strings.Contains(listing, stored) {
		t.Errorf("tofu login --status does not name the account:\n%s", listing)
	}
	if !strings.Contains(redacted, shown) || strings.Contains(redacted, stored) {
		t.Errorf("tofu login --status %s does not fall back to the mask:\n%s", redactFlag, redacted)
	}
	if !strings.Contains(pane, shown) || strings.Contains(pane, stored) {
		t.Errorf("the settings pane no longer masks the account:\n%s", pane)
	}
}

func storeCodexLogin(t *testing.T, account string) {
	t.Helper()
	claims, err := json.Marshal(map[string]any{
		codex.JWTAuthClaim: map[string]any{codex.AccountClaim: account},
	})
	if err != nil {
		t.Fatal(err)
	}
	segment := base64.RawURLEncoding.EncodeToString
	path, err := cred.Path()
	if err != nil {
		t.Fatal(err)
	}
	store, err := cred.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	now := time.Now()
	credential := cred.Credential{
		Provider:   cred.CodexSub,
		Kind:       cred.KindOAuth,
		Access:     segment([]byte(`{"alg":"none"}`)) + "." + segment(claims) + ".not-a-signature",
		Expires:    now.Add(time.Hour),
		Authorized: now,
	}
	if err := store.Save(credential, now); err != nil {
		t.Fatal(err)
	}
}

func TestACodexAccountIsNamedByItsIDInTheListingAndMaskedInThePane(t *testing.T) {
	emptyHome(t)
	const account = "codex-account-uuid"
	storeCodexLogin(t, account)

	listing, errOut, code := runLogin(t, "--status")
	if code != exitOK {
		t.Fatalf("status = %d, %q", code, errOut)
	}
	pane := settingsScreen(t, appProviders())

	shown := maskedAccount(cred.Identity{AccountID: account})
	if !strings.Contains(shown, "#") {
		t.Fatal("a codex account renders without a fingerprint")
	}
	if !strings.Contains(listing, account) {
		t.Errorf("tofu login --status does not name the codex account:\n%s", listing)
	}
	if !strings.Contains(pane, shown) || strings.Contains(pane, account) {
		t.Errorf("the settings pane no longer masks the codex account:\n%s", pane)
	}
}

func TestTwoAccountsThatMaskAlikeAreStillToldApartWhenRedacted(t *testing.T) {
	emptyHome(t)
	storeTwoAnthropicAccounts(t)

	if widget.Mask(firstTestAccount) != widget.Mask(secondTestAccount) {
		t.Fatal("the two accounts do not share a tail, so nothing here is proved")
	}
	first := maskedAccount(cred.Identity{AccountID: firstTestAccount})
	second := maskedAccount(cred.Identity{AccountID: secondTestAccount})
	if first == second {
		t.Fatalf("both accounts render as %q", first)
	}

	listing, errOut, code := runLogin(t, "--status", redactFlag)
	if code != exitOK {
		t.Fatalf("status = %d, %q", code, errOut)
	}
	for _, want := range []string{first, second} {
		if !strings.Contains(listing, want) {
			t.Errorf("the listing is missing %q:\n%s", want, listing)
		}
	}
}

func TestSettingAsideRefusesANumberThatIsNotStored(t *testing.T) {
	emptyHome(t)
	storeTwoAnthropicAccounts(t)

	for _, number := range []string{"9", "not-a-number"} {
		_, errOut, code := runLogin(t, "--disable", number)
		if code != exitVerdict {
			t.Errorf("disable %s = %d, want a refusal", number, code)
		}
		if !strings.Contains(errOut, "tofu login") {
			t.Errorf("the refusal for %s does not say what to run: %q", number, errOut)
		}
	}
}
