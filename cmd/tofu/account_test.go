package main

import (
	"strings"
	"testing"
	"time"

	"tofu/internal/llm/cred"
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
			Provider:   cred.Anthropic,
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
	for _, want := range []string{"#1 ", "#2 ", "····uuid"} {
		if !strings.Contains(out, want) {
			t.Errorf("status %q is missing %q", out, want)
		}
	}
	if lines := strings.Count(out, "anthropic oauth, expires"); lines != 2 {
		t.Errorf("status describes %d anthropic credentials, want both: %q", lines, out)
	}
	for _, secret := range []string{firstTestAccount, secondTestAccount, testAccess} {
		if strings.Contains(out, secret) {
			t.Fatal("the listing names something identifying")
		}
	}
}

func TestDisablingOneCredentialLeavesTheOtherChosen(t *testing.T) {
	emptyHome(t)
	storeTwoAnthropicAccounts(t)

	if err := chosenAccount(t); err == nil {
		t.Fatal("two usable credentials chose one by ordering")
	}

	out, errOut, code := runLogin(t, "--disable", "1")
	if code != exitOK {
		t.Fatalf("disable = %d, %q", code, errOut)
	}
	if !strings.Contains(out, "#1 ") || !strings.Contains(out, "set aside by hand") {
		t.Fatalf("disable said %q", out)
	}

	if err := chosenAccount(t); err != nil {
		t.Fatalf("after setting one aside: %v", err)
	}

	out, errOut, code = runLogin(t, "--enable", "1")
	if code != exitOK {
		t.Fatalf("enable = %d, %q", code, errOut)
	}
	if strings.Contains(out, "set aside by hand") {
		t.Fatalf("enable said %q", out)
	}
	if err := chosenAccount(t); err == nil {
		t.Fatal("two usable credentials chose one by ordering again")
	}
}

func chosenAccount(t *testing.T) error {
	t.Helper()
	_, accountID, store, err := subscriptionCredential(cred.Anthropic)
	if store != nil {
		defer func() { _ = store.Close() }()
	}
	if err == nil && accountID != secondTestAccount {
		t.Fatal("the credential left usable was not the one chosen")
	}
	return err
}

func TestBothViewsShowTheAccountTheSameWay(t *testing.T) {
	emptyHome(t)
	stored := storeCredential(t, cred.Anthropic)

	listing, errOut, code := runLogin(t, "--status")
	if code != exitOK {
		t.Fatalf("status = %d, %q", code, errOut)
	}
	pane := settingsScreen(t, appProviders())

	shown := maskedAccount(cred.Identity{Email: stored})
	if !strings.Contains(listing, shown) {
		t.Errorf("tofu login --status does not show %q:\n%s", shown, listing)
	}
	if !strings.Contains(pane, shown) {
		t.Errorf("the settings pane does not show %q:\n%s", shown, pane)
	}
	if strings.Contains(listing, stored) {
		t.Error("tofu login --status names the account in full")
	}
	if strings.Contains(pane, stored) {
		t.Errorf("the settings pane names the account in full:\n%s", pane)
	}
}

func TestTwoAccountsThatMaskAlikeAreStillToldApart(t *testing.T) {
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

	listing, errOut, code := runLogin(t, "--status")
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
