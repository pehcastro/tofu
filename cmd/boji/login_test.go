package main

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"boji/internal/llm/cred"
)

const (
	testAccess = "sk-ant-oat-not-a-real-token"
	testEmail  = "person@example.test"
)

func runLogin(t *testing.T, args ...string) (string, string, int) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := loginVerb(args, strings.NewReader(""), &out, &errOut)
	return out.String(), errOut.String(), code
}

func TestLoginVerbRefusesWhatItCannotRun(t *testing.T) {
	cases := [][]string{{}, {"gemini"}, {"anthropic", "--browser"}}
	for _, args := range cases {
		_, errOut, code := runLogin(t, args...)
		if code != exitUsage {
			t.Errorf("loginVerb(%v) = %d, want %d", args, code, exitUsage)
		}
		if !strings.HasPrefix(errOut, "boji login:") {
			t.Errorf("loginVerb(%v) said %q", args, errOut)
		}
	}
}

func TestLoginStatusReportsNoneAndThenTheStoredCredential(t *testing.T) {
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)

	out, _, code := runLogin(t, "--status")
	if code != exitOK || out != "credentials: none\n" {
		t.Fatalf("status without a credential = %q, code %d", out, code)
	}

	path, err := cred.Path()
	if err != nil {
		t.Fatalf("path: %v", err)
	}
	store, err := cred.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	authorized := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	err = store.Save(cred.Credential{
		Provider:   cred.Anthropic,
		Kind:       cred.KindOAuth,
		Access:     testAccess,
		Refresh:    "refresh-not-a-real-token",
		Expires:    authorized.Add(8 * time.Hour),
		Identity:   cred.Identity{AccountID: "account-uuid-1", Email: testEmail},
		Authorized: authorized,
	}, authorized)
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	out, _, code = runLogin(t, "--status")
	if code != exitOK {
		t.Fatalf("status = %d", code)
	}
	for _, want := range []string{"anthropic", "oauth", "expires 2026-09-18T20:00:00Z", "re-login by 2026-10-18"} {
		if !strings.Contains(out, want) {
			t.Errorf("status %q is missing %q", out, want)
		}
	}
	for _, secret := range []string{testAccess, "refresh-not-a-real-token", testEmail, "account-uuid-1"} {
		if strings.Contains(out, secret) {
			t.Fatalf("status leaked %q", secret)
		}
	}
}
